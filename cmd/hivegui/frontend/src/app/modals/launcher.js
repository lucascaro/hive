// ---------- agent launcher ----------
//
// Moved verbatim from main.js. Focus-pipeline callbacks are injected
// via initLauncher(deps) — the launcher must never import the focus
// pipeline directly (main.js owns that wiring).

import {
  CreateSession, DuplicateSession, RestartSession, ListAgents, IsGitRepo,
} from '../../bridge.js';
import { state } from '../state.js';
import { flashStatus, reportFailure } from '../dom.js';
import { activeProjectId, resolveSessionCwd } from '../selectors.js';
import { registerModal } from './registry.js';

let deps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
};

export const launcherEl = document.getElementById('launcher');
export const launcherState = {
  items: [],
  selected: 0,
  projectId: null,
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

function loadAgentUsage() {
  try { return JSON.parse(localStorage.getItem('hive.agentUsage') || '{}') || {}; }
  catch { return {}; }
}
export function bumpAgentUsage(id) {
  if (!id) return;
  const u = loadAgentUsage();
  u[id] = (u[id] || 0) + 1;
  try { localStorage.setItem('hive.agentUsage', JSON.stringify(u)); } catch {}
}

function highlightLauncherSelection() {
  launcherState.items.forEach((it, i) => {
    it.el.classList.toggle('selected', i === launcherState.selected);
    if (i === launcherState.selected) it.el.scrollIntoView({ block: 'nearest' });
  });
}

export function moveLauncherSelection(delta) {
  const n = launcherState.items.length;
  if (n === 0) return;
  launcherState.selected = (launcherState.selected + delta + n) % n;
  highlightLauncherSelection();
}

export function activateLauncherSelection() {
  const it = launcherState.items[launcherState.selected];
  if (!it) return;
  bumpAgentUsage(it.agent.id);
  flashStatus('creating session…');
  if (launcherState.duplicateFrom) {
    DuplicateSession(
      it.agent.id,
      launcherState.projectId || '',
      launcherState.duplicateCwd,
    ).catch(reportFailure('duplicate session'));
  } else {
    CreateSession(
      it.agent.id,
      launcherState.projectId || activeProjectId(),
      '', '',
      0, 0,
      !!launcherState.useWorktree,
    ).catch(reportFailure('new session'));
  }
  closeLauncher();
}

export function openLauncher(projectId, opts) {
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
  launcherState.duplicateFrom = (opts && opts.duplicateFrom) || null;
  launcherState.duplicateCwd = (opts && opts.duplicateCwd) || '';
  if (launcherState.duplicateFrom) {
    launcherState.useWorktree = false;
  }
  // Open the shell synchronously with a loading row so the popup
  // appears the instant the user asks for it; the agent list fills in
  // when ListAgents resolves. Kills the old open-blank-then-populate
  // flash (and the launcher not appearing at all if the list is slow).
  launcherEl.innerHTML = '';
  launcherState.items = [];
  // Anchor next to the resolved project's + button so the user
  // can see which project the new session lands in. Falls back
  // to the global new-project button if the project's row isn't
  // currently in the DOM (e.g. its header is offscreen), and to a
  // fixed spot over the sidebar if neither anchor exists — a
  // missing anchor must not throw and leave the launcher unopened
  // (the throw used to vanish into this chain's empty catch).
  const anchorEl =
    document.querySelector(`.project[data-pid="${launcherState.projectId}"] .project-actions button`) ??
    document.getElementById('new-project-btn');
  if (anchorEl) {
    const r = anchorEl.getBoundingClientRect();
    launcherEl.style.left = `${r.left}px`;
    launcherEl.style.top = `${r.bottom + 4}px`;
  } else {
    launcherEl.style.left = '16px';
    launcherEl.style.top = '64px';
  }
  const loading = document.createElement('div');
  loading.className = 'launcher-loading';
  loading.textContent = 'Loading agents…';
  launcherEl.appendChild(loading);
  launcherEl.classList.remove('hidden');
  // Drop the active tile's visual focus — modal owns the keyboard.
  deps.setFocusedTile(null);

  ListAgents()
    .then((agents) => {
      // The user may have dismissed the launcher while the list was in
      // flight — don't resurrect it.
      if (launcherEl.classList.contains('hidden')) return;
      launcherEl.innerHTML = '';
      launcherState.items = [];

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
      if (!launcherState.duplicateFrom) {
        const wtRow = document.createElement('label');
        wtRow.className = 'launcher-worktree';
        const wtBox = document.createElement('input');
        wtBox.type = 'checkbox';
        wtBox.checked = !!launcherState.useWorktree;
        const wtLabel = document.createElement('span');
        wtLabel.textContent = 'Create in git worktree';
        wtRow.append(wtBox, wtLabel);
        wtBox.addEventListener('change', (e) => {
          launcherState.useWorktree = e.target.checked;
          localStorage.setItem('hive.worktree', e.target.checked ? '1' : '0');
        });
        launcherEl.appendChild(wtRow);
        if (projCwd) {
          IsGitRepo(projCwd).then((ok) => {
            if (!ok) {
              wtRow.classList.add('disabled');
              wtBox.disabled = true;
              wtBox.checked = false;
              launcherState.useWorktree = false;
              wtLabel.textContent = 'Worktree (project is not a git repo)';
            }
          }).catch(() => {
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
      const ordered = agents
        .map((a, i) => ({ a, i }))
        .sort((x, y) => {
          const ux = usage[x.a.id] || 0, uy = usage[y.a.id] || 0;
          if (ux !== uy) return uy - ux;
          return x.i - y.i;
        })
        .map((e) => e.a);
      if (ordered.length === 0) {
        const none = document.createElement('div');
        none.className = 'launcher-empty';
        none.textContent = 'No agents found';
        launcherEl.appendChild(none);
      }
      ordered.forEach((a, idx) => {
        const item = document.createElement('div');
        item.className = 'launcher-item' + (a.available ? '' : ' uninstalled');
        item.style.setProperty('--agent-color', a.color);
        const num = document.createElement('span');
        num.className = 'agent-num';
        // Number keys 1–9 select that row directly; 10+ rows show no
        // number (no digit shortcut).
        num.textContent = idx < 9 ? String(idx + 1) : '';
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
        item.addEventListener('click', () => {
          bumpAgentUsage(a.id);
          flashStatus('creating session…');
          if (launcherState.duplicateFrom) {
            DuplicateSession(
              a.id,
              launcherState.projectId || '',
              launcherState.duplicateCwd,
            ).catch(reportFailure('duplicate session'));
          } else {
            CreateSession(
              a.id,
              launcherState.projectId,
              '', '',
              0, 0,
              !!launcherState.useWorktree,
            ).catch(reportFailure('new session'));
          }
          closeLauncher();
        });
        item.addEventListener('mouseenter', () => {
          launcherState.selected = idx;
          highlightLauncherSelection();
        });
        launcherEl.appendChild(item);
        launcherState.items.push({ agent: a, el: item });
      });
      launcherState.selected = 0;
      highlightLauncherSelection();
    })
    // Anything thrown in the chain above (not just a ListAgents
    // rejection) used to land here silently — the user pressed ⌘T and
    // nothing happened, with no trace. Close the loading shell too:
    // an empty popup with stale "Loading agents…" would be worse.
    .catch((err) => {
      reportFailure('launcher')(err);
      closeLauncher();
    });
}

export function closeLauncher() {
  launcherEl.classList.add('hidden');
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
  DuplicateSession(s.agent || '', pid, cwd).catch(reportFailure('duplicate session'));
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

export function initLauncher(injected) {
  deps = injected;
  registerModal(launcherEl);
  document.addEventListener('click', (e) => {
    const inAction = e.target.closest('.project-actions');
    if (!launcherEl.contains(e.target) && !inAction) closeLauncher();
  });
}
