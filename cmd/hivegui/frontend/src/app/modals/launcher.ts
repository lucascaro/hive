// ---------- agent launcher ----------
//
// Moved verbatim from main.js. Focus-pipeline callbacks are injected
// via initLauncher(deps) — the launcher must never import the focus
// pipeline directly (main.ts owns that wiring).

import {
  CreateSession,
  DuplicateSession,
  RestartSession,
  ListAgents,
  IsGitRepo,
} from '../../bridge.js';
import { state } from '../state.js';
import { flashStatus, reportFailure } from '../dom.js';
import { activeProjectId, resolveSessionCwd } from '../selectors.js';
import { registerModal } from './registry.js';
import { releaseFocus } from './focus-trap.js';
import { pageEl } from '../el.js';
import { cmdOrCtrl } from '../../lib/platform.js';
import type { SessionInfo } from '../state.js';
// Type-only, so the generated module is erased before Vite resolves it.
import type { main } from '../../../wailsjs/go/models';

// Narrow on purpose: this modal needs exactly two callbacks off the
// focus pipeline, so it names those two rather than the whole module.
export interface LauncherDeps {
  setFocusedTile: (id: string | null) => void;
  refocusActiveTerm: () => void;
}

// One rendered agent row, paired with the element that draws it so
// highlightLauncherSelection can toggle .selected without re-querying.
interface LauncherItem {
  agent: main.AgentInfo;
  el: HTMLElement;
}

export interface LauncherOpts {
  forceWorktree?: boolean;
  duplicateFrom?: SessionInfo | null;
  duplicateCwd?: string;
  // worktreePath switches the launcher into "resume this worktree"
  // mode: the session runs in a worktree that already exists, so no
  // worktree row and no branch input are shown.
  worktreePath?: string;
  // continueConversation asks the agent to resume its most recent
  // conversation in that worktree instead of starting a new one.
  continueConversation?: boolean;
}

let deps: LauncherDeps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
};

export const launcherEl = pageEl('launcher');

// The three children openLauncher builds inside #launcher, in order:
// the filter box, the worktree row, and the list of agent rows. Only
// the list is rebuilt as the user types — recreating the input would
// drop focus and the caret on every keystroke.
//
// All three are recreated per open (openLauncher clears #launcher), so
// a query never leaks from one opening to the next. They are null
// before the first open and after a close, which is reachable: the
// ListAgents rejection path closes a launcher whose children were
// never filled in.
let searchEl: HTMLInputElement | null = null;
let listEl: HTMLElement | null = null;
// The branch-name box, when the worktree toggle is on. Module-level for
// the same reason searchEl is: the mousedown and keydown handlers below
// are installed once at init and have to recognise it.
let branchEl: HTMLInputElement | null = null;
// Bumped on every open. A ListAgents promise captures the value it was
// issued under and bails if it no longer matches, so a slow response
// can't land in a launcher that has since been reopened.
//
// This used to be implicit: the old .then began by wiping #launcher, so
// a late resolve just rebuilt everything. The wipe had to go (it would
// destroy the focused filter box), which turned "harmlessly redundant"
// into "inserts a second worktree row" — the `hidden` check alone
// doesn't catch a reopen, because the launcher is visible again by then.
let openGeneration = 0;
// The usage-ordered agent list ListAgents returned, kept so filtering
// re-renders from memory instead of refetching. Reset per open, not
// just per close: a ⌘T over an already-open launcher would otherwise
// filter the previous opening's list until the new response lands.
let allAgents: main.AgentInfo[] = [];
// True from the moment a request is issued until it settles. Without
// it, an empty allAgents is indistinguishable from "the query excluded
// everything", and the first character typed during the round trip
// would replace the loading row with "No agents match".
let agentsLoading = false;
export const launcherState: {
  items: LauncherItem[];
  selected: number;
  projectId: string | null;
  useWorktree: boolean;
  branch: string;
  worktreePath: string;
  continueConversation: boolean;
  duplicateFrom: SessionInfo | null;
  duplicateCwd: string;
} = {
  items: [],
  selected: 0,
  projectId: null,
  // branch names the worktree to create. Empty lets the daemon
  // generate an adjective-noun, which is the long-standing default.
  branch: '',
  // worktreePath, when set, runs the session in an EXISTING worktree
  // instead of creating one.
  worktreePath: '',
  // Only ever true for one opening, set by the worktree browser's
  // "Continue previous" action.
  continueConversation: false,
  // useWorktree is sticky across launcher opens, persisted in
  // localStorage. ⌃⌘N opens the launcher with this forced to true
  // for the duration of that opening.
  useWorktree: localStorage.getItem('hive.worktree') === '1',
  // duplicateFrom, when set, switches the launcher into "duplicate
  // session" mode: cwd is fixed to duplicateCwd, the worktree toggle is
  // hidden, and selecting an agent calls DuplicateSession instead of
  // CreateSession.
  duplicateFrom: null,
  duplicateCwd: '',
};

// Agent id → launch count, persisted in localStorage. Read back as
// unknown JSON, so every lookup is `|| 0`.
type AgentUsage = Record<string, number>;

function loadAgentUsage(): AgentUsage {
  try {
    return JSON.parse(localStorage.getItem('hive.agentUsage') || '{}') || {};
  } catch {
    return {};
  }
}
export function bumpAgentUsage(id: string | undefined) {
  if (!id) return;
  const u = loadAgentUsage();
  u[id] = (u[id] || 0) + 1;
  try {
    localStorage.setItem('hive.agentUsage', JSON.stringify(u));
  } catch {}
}

function highlightLauncherSelection() {
  launcherState.items.forEach((it, i) => {
    it.el.classList.toggle('selected', i === launcherState.selected);
    if (i === launcherState.selected)
      it.el.scrollIntoView({ block: 'nearest' });
  });
}

function moveLauncherSelection(delta: number) {
  const n = launcherState.items.length;
  if (n === 0) return;
  launcherState.selected = (launcherState.selected + delta + n) % n;
  highlightLauncherSelection();
}

// launchSelected runs the create/duplicate-session sequence shared by
// the keyboard-select path (activateLauncherSelection) and the
// per-item click handler in openLauncher: bump usage, flash status,
// call the daemon, close the launcher.
function launchSelected(agentId: string) {
  bumpAgentUsage(agentId);
  flashStatus('creating session…');
  // Anchor the new session under the one it came from (duplicate) or
  // under the active one, so it lands next to its context instead of at
  // the bottom of the sidebar.
  const anchor = launcherState.duplicateFrom?.id || state.activeId || '';
  if (launcherState.duplicateFrom) {
    DuplicateSession(
      agentId,
      launcherState.projectId || '',
      launcherState.duplicateCwd,
      anchor,
    ).catch(reportFailure('duplicate session'));
  } else {
    CreateSession(
      agentId,
      launcherState.projectId || activeProjectId(),
      '',
      '',
      0,
      0,
      !!launcherState.useWorktree,
      anchor,
      launcherState.branch,
      launcherState.worktreePath,
      launcherState.continueConversation,
    ).catch(reportFailure('new session'));
  }
  closeLauncher();
}

function activateLauncherSelection() {
  const it = launcherState.items[launcherState.selected];
  if (!it) return;
  launchSelected(it.agent.id);
}

// Rebuild the agent rows from allAgents, narrowed to those whose name
// contains the query. Same substring match the command palette uses
// (modals/command-palette.ts) — deliberately not fuzzy.
//
// The query is read off searchEl every call rather than cached:
// ListAgents can resolve after the user has already started typing,
// and a cached query would be stale by the time this runs for the
// first filled-in render.
function renderLauncherList() {
  if (!listEl) return;
  // Two readings of the same box, on purpose. `raw` decides whether the
  // user is typing — it must agree with the digit handler, which also
  // tests the raw value, or a lone space would show [n] hints that no
  // longer fire. `q` decides what matches, where surrounding whitespace
  // is just noise.
  const raw = searchEl?.value ?? '';
  const q = raw.trim().toLowerCase();
  listEl.innerHTML = '';
  launcherState.items = [];
  const matches = q
    ? allAgents.filter((a) => a.name.toLowerCase().includes(q))
    : allAgents;
  if (matches.length === 0) {
    const none = document.createElement('div');
    // Three different facts, and conflating any two of them misreads as
    // a broken agent list: the request is still in flight, the daemon
    // returned nothing, or the query excluded everything.
    if (agentsLoading) {
      none.className = 'launcher-loading';
      none.textContent = 'Loading agents…';
    } else {
      none.className = 'launcher-empty';
      none.textContent = q ? 'No agents match' : 'No agents found';
    }
    listEl.appendChild(none);
  }
  matches.forEach((a, idx) => {
    const item = document.createElement('div');
    item.className = `launcher-item${a.available ? '' : ' uninstalled'}`;
    item.style.setProperty('--agent-color', a.color);
    const num = document.createElement('span');
    num.className = 'agent-num';
    // Number keys 1–9 select that row directly; 10+ rows show no
    // number (no digit shortcut). While a query is active the digits
    // type into it instead of selecting, so the hints come off — a
    // visible [n] that does nothing is worse than none (AGENTS.md,
    // Key Discoverability).
    num.textContent = !raw && idx < 9 ? String(idx + 1) : '';
    const dot = document.createElement('span');
    dot.className = 'agent-dot';
    const name = document.createElement('span');
    name.className = 'agent-name';
    name.textContent = a.name;
    item.append(num, dot, name);
    if (!a.available && a.installCmd && a.installCmd.length) {
      const tag = document.createElement('span');
      tag.className = 'install-tag';
      tag.title = a.installCmd.join(' ');
      tag.textContent = 'install?';
      item.appendChild(tag);
    }
    item.addEventListener('click', () => launchSelected(a.id));
    item.addEventListener('mouseenter', () => {
      launcherState.selected = idx;
      highlightLauncherSelection();
    });
    listEl?.appendChild(item);
    launcherState.items.push({ agent: a, el: item });
  });
  // Narrowing the list invalidates the old index — the row that was
  // selected may not even be rendered any more. Always land on the
  // top match so Enter means "the obvious one".
  launcherState.selected = 0;
  highlightLauncherSelection();
}

// projectId is optional, not just nullable: main.ts, keyboard.ts and
// view.ts all call openLauncher() bare and let the `|| activeProjectId()`
// fallback below pick the project. Wave 5a typed it as required because
// every one of those callers was still unchecked JS.
export function openLauncher(projectId?: string | null, opts?: LauncherOpts) {
  launcherState.projectId = projectId || activeProjectId();
  // Re-read the sticky pref each open so a one-shot forceWorktree from a
  // previous opening doesn't leak into the next regular open. forceWorktree
  // overrides for this opening only and is intentionally not persisted.
  launcherState.useWorktree =
    opts && typeof opts.forceWorktree === 'boolean'
      ? opts.forceWorktree
      : localStorage.getItem('hive.worktree') === '1';
  // duplicateFrom: when present, the launcher is forking an existing
  // session into the same cwd — never a new worktree.
  launcherState.duplicateFrom = opts?.duplicateFrom || null;
  launcherState.duplicateCwd = opts?.duplicateCwd || '';
  // Reset per open: a branch typed for one session must never leak
  // into the next one, which would silently reuse (or collide with)
  // the previous name.
  launcherState.branch = '';
  launcherState.worktreePath = opts?.worktreePath || '';
  launcherState.continueConversation = !!opts?.continueConversation;
  if (launcherState.duplicateFrom || launcherState.worktreePath) {
    launcherState.useWorktree = false;
  }
  // Open the shell synchronously with a loading row so the popup
  // appears the instant the user asks for it; the agent list fills in
  // when ListAgents resolves. Kills the old open-blank-then-populate
  // flash (and the launcher not appearing at all if the list is slow).
  launcherEl.innerHTML = '';
  branchEl = null;
  launcherState.items = [];
  // Anchor under the resolved project's card header so the user
  // can see which project the new session lands in. The header, not
  // its + button: the card's actions are `display: none` until the
  // header is hovered (ui/project-card.ts), and a display:none anchor
  // measures as a zero rect at the origin. Falls back to the global
  // new-project button if the project's card isn't currently in the
  // DOM (e.g. the project is minimized), and to a fixed spot over the
  // sidebar if neither anchor exists — a missing anchor must not throw
  // and leave the launcher unopened (the throw used to vanish into
  // this chain's empty catch).
  const anchorEl =
    document.querySelector(
      `.hv-project-card[data-pid="${launcherState.projectId}"] .hv-project-card__header`,
    ) ?? document.getElementById('new-project-btn');
  if (anchorEl) {
    const r = anchorEl.getBoundingClientRect();
    launcherEl.style.left = `${r.left}px`;
    launcherEl.style.top = `${r.bottom + 4}px`;
  } else {
    launcherEl.style.left = '16px';
    launcherEl.style.top = '64px';
  }
  // Filter box first, then the list the rows render into. Both are
  // built fresh here (the innerHTML wipe above dropped the previous
  // pair), which is what guarantees every opening starts with an empty
  // query — including ⌘T on an already-open launcher and the ⇧⌘P
  // duplicate flow. Don't "optimize" this into reusing the old input.
  searchEl = document.createElement('input');
  searchEl.type = 'text';
  searchEl.className = 'launcher-search';
  searchEl.placeholder = 'Filter agents…';
  searchEl.setAttribute('aria-label', 'Filter agents');
  searchEl.autocomplete = 'off';
  searchEl.addEventListener('input', renderLauncherList);
  listEl = document.createElement('div');
  listEl.className = 'launcher-list';
  launcherEl.append(searchEl, listEl);
  // Reset before the first render so neither the previous opening's
  // agents nor its loading state leak into this one.
  allAgents = [];
  agentsLoading = true;
  // Draws the loading row through the same path every keystroke uses,
  // so typing during the round trip can't render something the initial
  // paint wouldn't have.
  renderLauncherList();
  launcherEl.classList.remove('hidden');
  // Drop the active tile's visual focus — modal owns the keyboard.
  deps.setFocusedTile(null);
  // The launcher's keys are handled by its own listener (initLauncher),
  // which only sees them while focus is inside #launcher.
  searchEl.focus();

  const gen = ++openGeneration;
  ListAgents()
    .then((agents) => {
      // The user may have dismissed the launcher while the list was in
      // flight — don't resurrect it.
      if (launcherEl.classList.contains('hidden')) return;
      // ...or reopened it, in which case this response belongs to a
      // launcher that no longer exists and a newer request owns the DOM.
      if (gen !== openGeneration) return;
      agentsLoading = false;

      // Worktree toggle row at the top of the menu. Disabled (and
      // visually muted) when the active project's cwd isn't a git
      // repo. The IsGitRepo probe is async; we render the row
      // immediately as enabled and disable it once the probe
      // completes — almost always before the user reaches for the
      // checkbox.
      const proj = state.projects.find((p) => p.id === launcherState.projectId);
      const projCwd = proj?.cwd ?? '';
      // In duplicate mode the cwd is fixed to the source session, so
      // the worktree toggle is meaningless — skip the row entirely.
      // In resume mode the worktree already exists, so neither the
      // toggle nor the branch input has anything to decide.
      if (!launcherState.duplicateFrom && !launcherState.worktreePath) {
        const wtRow = document.createElement('label');
        wtRow.className = 'launcher-worktree';
        const wtBox = document.createElement('input');
        wtBox.type = 'checkbox';
        wtBox.checked = !!launcherState.useWorktree;
        const wtLabel = document.createElement('span');
        wtLabel.textContent = 'Create in git worktree';
        wtRow.append(wtBox, wtLabel);
        // Between the filter box and the list, not appended at the
        // end — #launcher already holds both by the time this runs.
        launcherEl.insertBefore(wtRow, listEl);

        // Branch name, revealed only while the toggle is on: it is
        // meaningless otherwise, and an always-visible field would push
        // the agent list down on every open.
        const branchInput = document.createElement('input');
        branchEl = branchInput;
        branchInput.type = 'text';
        branchInput.className = 'launcher-branch';
        branchInput.placeholder = 'branch name (optional)';
        branchInput.setAttribute('aria-label', 'Worktree branch name');
        branchInput.value = launcherState.branch;
        const syncBranchVisibility = () => {
          branchInput.classList.toggle('hidden', !launcherState.useWorktree);
        };
        syncBranchVisibility();
        launcherEl.insertBefore(branchInput, listEl);
        branchInput.addEventListener('input', () => {
          launcherState.branch = branchInput.value.trim();
        });
        wtBox.addEventListener('change', (e) => {
          const box = e.target as HTMLInputElement;
          launcherState.useWorktree = box.checked;
          localStorage.setItem('hive.worktree', box.checked ? '1' : '0');
          syncBranchVisibility();
          if (!box.checked) {
            branchInput.value = '';
            launcherState.branch = '';
          }
        });
        if (projCwd) {
          IsGitRepo(projCwd)
            .then((ok) => {
              // Same staleness rule as ListAgents above: without this, a
              // late "not a git repo" answer for the project this open
              // was anchored to could set useWorktree = false under a
              // launcher since reopened on a git-backed project, leaving
              // a checked box whose state says off.
              if (gen !== openGeneration) return;
              if (!ok) {
                wtRow.classList.add('disabled');
                wtBox.disabled = true;
                wtBox.checked = false;
                launcherState.useWorktree = false;
                launcherState.branch = '';
                branchInput.value = '';
                branchInput.classList.add('hidden');
                wtLabel.textContent = 'Worktree (project is not a git repo)';
              }
            })
            .catch(() => {
              // Intentionally silent: the probe rejects only when the
              // bridge itself is down. Worst case the daemon later
              // refuses worktree creation via control:error, which IS
              // surfaced.
            });
        }
      }
      // Detection (exec.LookPath on the daemon side) is best-effort:
      // it can miss agents installed as shell aliases, functions, or
      // PATH that's only set up by an interactive rc file. So we list
      // every agent as launchable and let the user try; the daemon
      // runs the command via the user's interactive shell, and any
      // real failure surfaces as "command not found" inside the
      // session's terminal. The "install" hint stays visible as
      // advisory text for the truly-not-installed case.
      // Sort agents by recent usage (most-used first), ties preserve
      // the agent package's display order. Usage is persisted in
      // localStorage and incremented on activation.
      const usage = loadAgentUsage();
      allAgents = agents
        .map((a, i) => ({ a, i }))
        .sort((x, y) => {
          const ux = usage[x.a.id] || 0,
            uy = usage[y.a.id] || 0;
          if (ux !== uy) return uy - ux;
          return x.i - y.i;
        })
        .map((e) => e.a);
      // Renders through the same path as every keystroke, so a query
      // typed while this request was in flight is already applied.
      renderLauncherList();
    })
    // Anything thrown in the chain above (not just a ListAgents
    // rejection) used to land here silently — the user pressed ⌘T and
    // nothing happened, with no trace. Close the loading shell too:
    // an empty popup with stale "Loading agents…" would be worse.
    .catch((err) => {
      // Same staleness rule as the success path: a rejection from a
      // superseded request must not close the launcher the user just
      // reopened. Still reported — the failure was real.
      reportFailure('launcher')(err);
      if (gen !== openGeneration) return;
      agentsLoading = false;
      closeLauncher();
    });
}

export function closeLauncher() {
  // Idempotent: an outside click on a focusable element closes twice —
  // once from focusout at mousedown, once from the document click
  // handler — and running refocusActiveTerm() twice is pointless work
  // against the terminal. Also makes the ListAgents rejection path safe
  // when it fires against a launcher that is already closed.
  if (launcherEl.classList.contains('hidden')) return;
  // Blur first: refocusActiveTerm() bails when activeElement is an
  // INPUT (lib/focus.ts), and hiding the launcher via CSS does not
  // synchronously move focus out of it in a real engine.
  //
  // Whatever holds focus, not just searchEl. In practice the mousedown
  // handler below keeps focus on searchEl, so those are the same thing
  // today — this stays general so that adding one focusable control to
  // the popup later can't quietly resurrect the bug it was written for
  // (terminal never refocused, because activeElement was still an
  // <input> the launcher owned).
  releaseFocus(launcherEl);
  launcherEl.classList.add('hidden');
  searchEl = null;
  listEl = null;
  branchEl = null;
  allAgents = [];
  launcherState.items = [];
  launcherState.duplicateFrom = null;
  launcherState.duplicateCwd = '';
  deps.refocusActiveTerm();
}

export function duplicateActiveSession() {
  const s = state.sessions.find((x) => x.id === state.activeId);
  if (!s) return;
  const cwd = resolveSessionCwd(s);
  if (!cwd) {
    flashStatus('cannot duplicate: source session has no cwd', true);
    return;
  }
  const pid = s.projectId ?? s.project_id ?? '';
  if (s.agent) bumpAgentUsage(s.agent);
  DuplicateSession(s.agent || '', pid, cwd, s.id).catch(
    reportFailure('duplicate session'),
  );
}

export function restartActiveSession() {
  const s = state.sessions.find((x) => x.id === state.activeId);
  if (!s) {
    flashStatus('no active session to restart', true);
    return;
  }
  RestartSession(s.id).catch(reportFailure('restart'));
}

export function duplicateActiveSessionChooseTool() {
  const s = state.sessions.find((x) => x.id === state.activeId);
  if (!s) return;
  const cwd = resolveSessionCwd(s);
  if (!cwd) {
    flashStatus('cannot duplicate: source session has no cwd', true);
    return;
  }
  const pid = s.projectId ?? s.project_id ?? '';
  openLauncher(pid, { duplicateFrom: s, duplicateCwd: cwd });
}

export function initLauncher(injected: LauncherDeps) {
  deps = injected;
  registerModal(launcherEl);
  // The launcher owns its keyboard while open, the way the command
  // palette and settings do — it has to, now that the filter box holds
  // focus and lib/focus.ts hands the keyboard to a focused <input>.
  // keyboard.ts bails out for #launcher for the same reason.
  launcherEl.addEventListener('keydown', (e) => {
    const handle = (fn: () => void) => {
      e.preventDefault();
      e.stopPropagation();
      fn();
    };
    if (e.key === 'ArrowDown' || (e.key === 'Tab' && !e.shiftKey))
      return handle(() => moveLauncherSelection(+1));
    if (e.key === 'ArrowUp' || (e.key === 'Tab' && e.shiftKey))
      return handle(() => moveLauncherSelection(-1));
    if (e.key === 'Enter') return handle(activateLauncherSelection);
    if (e.key === 'Escape') return handle(closeLauncher);
    if (cmdOrCtrl(e) && (e.key === 'n' || e.key === 'N'))
      return handle(closeLauncher);
    // ⌘T / ⇧⌘T while already open re-opens (and so clears the query).
    // keyboard.ts used to give us this for free — its launcher block
    // fell through on unhandled keys and hit the global ⌘T binding
    // further down. Now that it bails out for #launcher entirely, the
    // binding has to be repeated here or it would silently stop working.
    if (cmdOrCtrl(e) && (e.key === 't' || e.key === 'T'))
      return handle(() => {
        // Same two calls as the global binding in keyboard.ts — passing
        // undefined re-resolves the active project rather than pinning
        // the one this opening was anchored to.
        if (e.shiftKey) openLauncher(undefined, { forceWorktree: true });
        else openLauncher();
      });
    // Digit shortcut: 1–9 picks the corresponding row, but only while
    // the filter box is empty — past that the user is typing a query
    // and a digit is just a character. Raw .value, not trimmed: a
    // typed space is already a query. Skipped when a modifier is held
    // so ⌘1 and friends aren't swallowed.
    if (
      !e.metaKey &&
      !e.ctrlKey &&
      !e.altKey &&
      /^[1-9]$/.test(e.key) &&
      searchEl?.value === '' &&
      // A digit typed into the branch box is part of the branch name
      // (`fix-2`), not a row shortcut. Without this the box silently
      // drops every number the user types.
      e.target !== branchEl
    ) {
      const i = parseInt(e.key, 10) - 1;
      if (i < launcherState.items.length) {
        return handle(() => {
          launcherState.selected = i;
          activateLauncherSelection();
        });
      }
    }
  });
  // Nothing but the filter box may take focus. Clicking anything else
  // would blur it, and the keydown listener above only fires while
  // focus is inside #launcher — so the search would silently stop
  // responding to typing.
  //
  // preventDefault on mousedown suppresses only the focus shift and
  // text selection: click still fires, so agent rows still launch and
  // the worktree checkbox still toggles (a checkbox toggles on click
  // activation, not on focus). The checkbox used to be exempt here,
  // which meant clicking it moved focus onto it and every subsequent
  // keystroke went to the checkbox instead of the query — the list just
  // stopped narrowing, with nothing on screen to explain why.
  launcherEl.addEventListener('mousedown', (e) => {
    const target = e.target as Element | null;
    // Both text boxes are exempt: they exist to be typed into, so they
    // must be able to take focus from a click. Everything else stays
    // blocked — see the checkbox rationale above. The launcher's own
    // keydown handler covers the whole popup, so Enter/Escape/arrows
    // still work while focus sits in either box.
    if (target === searchEl || target === branchEl) return;
    e.preventDefault();
  });
  // Focus leaving the launcher closes it. keyboard.ts now bails out for
  // the whole window whenever #launcher is visible, and this module's
  // own keydown listener only fires while focus is inside it — so a
  // launcher that stays visible after focus moves away is a launcher
  // nobody is listening for, Escape included.
  //
  // The .hv-project-card__actions buttons are how you get there: each one
  // calls stopPropagation, so the outside-click handler below never sees
  // them (its .hv-project-card__actions exemption has always been
  // unreachable for that reason), but clicking the edit or delete button
  // still moves focus out.
  //
  // relatedTarget null means focus went nowhere — that's closeLauncher's
  // own blur, so ignore it rather than recursing.
  launcherEl.addEventListener('focusout', (e) => {
    if (launcherEl.classList.contains('hidden')) return;
    const next = (e as FocusEvent).relatedTarget as Node | null;
    if (next && !launcherEl.contains(next)) closeLauncher();
  });
  document.addEventListener('click', (e) => {
    const target = e.target as Element | null;
    // A click that OPENS the launcher must not also close it: this
    // handler runs after the opener's own handler, on the same event.
    // The sidebar's project actions are exempt for that reason, and
    // any other opener opts in with data-opens-launcher (the worktree
    // browser's "Open session" button does).
    const inAction =
      target?.closest('.hv-project-card__actions') ??
      target?.closest('[data-opens-launcher]');
    if (!launcherEl.contains(target) && !inAction) closeLauncher();
  });
}
