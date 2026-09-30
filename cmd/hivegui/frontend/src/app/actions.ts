// ---------- app actions ----------
//
// What the app does, independent of how it was asked: the key bindings
// (app/key-scopes.ts), the native menu and the command palette all reach
// these through the command registry (app/commands.ts). Moved out of
// keyboard.ts by spec 478, which left that file with only the listener.
//
// Font helpers and the focus pipeline are injected via initActions(deps)
// from main.tsx, so this module never imports the focus pipeline (see the
// acyclic-modules note at the wiring block in main.tsx).

import { KillProject, Confirm, SetClipboardText, Notify } from '../bridge.js';
import { readNeedsAttention } from './state.js';
import { appStore } from '../store/store.js';
import {
  addAttentionRestored,
  addAttentionRestoredProject,
  clearAttentionRestored,
  isModalOpen,
  setActivityGrid,
  setActivityPanel,
  setAttentionReturnId,
} from '../store/store.js';
import { blurTerminals } from './focus.js';
import { flashStatus, reportFailure } from './dom.js';
import {
  orderedSessions,
  activeProjectId,
  nextAttentionId,
} from './selectors.js';
import { openWorktrees } from './modals/worktrees.js';
import { openQuickIdea, closeQuickIdea } from './modals/quick-idea.js';
import {
  openIdeaInbox,
  closeIdeaInbox,
  ideaInboxProjectId,
} from './modals/idea-inbox.js';
import {
  switchTo,
  setView,
  gridSpatialMove,
  restoreSession,
  minimizeSession,
  minimizeProject,
  isSessionHidden,
  gridWouldTile,
} from './view.js';
import { clearAttention } from './events.js';
import { goBack, goForward } from '../lib/nav-history.js';
import { readProjectId } from '../lib/wire.js';
import { clusterReorderOps } from '../lib/worktree-groups.js';
import { runReorder } from './reorder-runner.js';
import { mustEl } from './el.js';
import type { ProjectInfo } from './state.js';
import { exclusiveScopeOpen, IDEA_SCOPES } from './key-scopes.js';

// Live read of the store. A function, not a destructured snapshot: this
// module runs inside event handlers and must never cache a slice across
// a store write.
const appData = () => appStore.getState();

export interface ActionDeps {
  bumpFontSize: (delta: number) => void;
  resetFontSize: () => void;
  focusActiveTerm: () => void;
  withoutNavHistory: (fn: () => void) => void;
}

let deps: ActionDeps = {
  bumpFontSize: () => {},
  resetFontSize: () => {},
  focusActiveTerm: () => {},
  // Injected from main.tsx like focusActiveTerm above: this module must
  // not import the focus pipeline directly (see the acyclic-modules
  // note at the wiring block in main.tsx). The default still RUNS fn —
  // an un-wired harness gets working navigation without suppression,
  // not a silently swallowed switch.
  withoutNavHistory: (fn) => fn(),
};

export function initActions(injected: ActionDeps) {
  deps = injected;
}

export function bumpFont(delta: number) {
  deps.bumpFontSize(delta);
}

export function resetFont() {
  deps.resetFontSize();
}

export function toggleSidebar() {
  mustEl('app').classList.toggle('sidebar-hidden');
  // Layout reflow → tile bodies resize → ResizeObserver fits xterm,
  // and fit.fit() can synchronously fire focusout on the helper-
  // textarea as the canvas re-sizes. Without re-asserting focus,
  // keystrokes strand on document.body even though the visual
  // .term-focused stays correctly pinned on the active tile (#208 R3).
  //
  // Sync-fire focusActiveTerm so setFocusedTile's rAF retry loop is
  // armed before the first focusout; staggered delayed re-fires catch
  // any focusout that escapes the standard 8-frame retry budget
  // (later RO callbacks, WebGL canvas swap, DPR settle). All calls
  // are idempotent — re-focusing an already-focused element is a no-op.
  deps.focusActiveTerm();
  setTimeout(() => deps.focusActiveTerm(), 32);
  setTimeout(() => deps.focusActiveTerm(), 100);
  setTimeout(() => deps.focusActiveTerm(), 250);
}

// focusActiveSession is the "zoom into the tile you navigated to" action
// behind both ⌘⏎ and the command palette, so the two can't drift. A no-op
// in single view: there is nothing to zoom into, and the palette lists
// every command in every view. setView already restores terminal focus
// and snaps the tile to the bottom.
export function focusActiveSession() {
  if (appData().view === 'single') return;
  setView('single');
}

export function toggleProjectGrid() {
  setView(appData().view === 'grid-project' ? 'single' : 'grid-project');
}

// ⌘J. In single view it opens or closes the inspector panel; in a grid
// it swaps every tile between its terminal and its activity. Neither
// takes keyboard focus: the panel is read-only, and the activity grid
// has nothing typeable, so it drops focus instead of stranding keys in
// a hidden terminal.
export function toggleActivity() {
  const s = appData();
  if (s.view === 'single') {
    setActivityPanel(!s.activityPanel);
    // The terminal column resizes; refit can blur the textarea, same as
    // toggleSidebar.
    deps.focusActiveTerm();
    setTimeout(() => deps.focusActiveTerm(), 100);
    return;
  }
  setActivityGrid(!s.activityGrid);
  if (appData().activityGrid) blurTerminals();
  else deps.focusActiveTerm();
}

// ⌘⇧J. The activity grid from anywhere. From single view it enters the
// project grid; with fewer than two sessions there is no grid to enter,
// so it opens the panel rather than doing nothing. Already on, it goes
// back to the terminals.
export function showActivityGrid() {
  const s = appData();
  if (s.view !== 'single') {
    toggleActivity();
    return;
  }
  if (!gridWouldTile('grid-project')) {
    if (!s.activityPanel) toggleActivity();
    return;
  }
  // Flag first, so the focus drive setView schedules sees the activity
  // grid and leaves the terminals alone.
  setActivityGrid(true);
  setView('grid-project');
  blurTerminals();
}

export function toggleAllGrid() {
  setView(appData().view === 'grid-all' ? 'single' : 'grid-all');
}

// handleArrow is the single implementation behind both the ⌘-arrow
// keydowns and the Session-menu events, so the two can't drift (they
// had: the menu's next/prev-session mapped ⌘↓ to a horizontal grid
// move).
//
// Returns true when the app consumed the key. Horizontal arrows in
// focused mode return false: ⌘←/⌘→ and ⇧⌘←/⇧⌘→ are start/end-of-line
// (and select-to-start/end) in the terminal, and the app must not take
// them.
export function handleArrow(
  dCol: number,
  dRow: number,
  shift: boolean,
): boolean {
  if (appData().view !== 'single') {
    gridSpatialMove(dCol, dRow);
    return true;
  }
  if (dCol !== 0) return false; // ⌘←/→ belong to the terminal in focused mode
  moveActiveSession(dRow, shift);
  return true;
}

export function navSession(delta: number) {
  handleArrow(0, delta, false);
}

// A menu item labelled "Move Session Forward" must reorder in every
// view — it used to silently become a horizontal grid move.
export function reorderActive(delta: number) {
  moveActiveSession(delta, true);
}

// jumpToAttention (⌘B) goes to the next session with an unread bell,
// recording where you came from in appData().attentionReturnId so ⇧⌘B can
// bring you back. The anchor is written ONLY when the slot is empty, so
// it holds the session you were working in before the FIRST ⌘B — a round
// of bells can bounce you through several flagged sessions and ⇧⌘B still
// returns you to the work you actually interrupted, not to the previous
// interruption. ⇧⌘B releases the anchor, which starts the next round.
//
// switchTo → setActive clears the target's attention flag, so a jump
// both delivers you there and marks it seen, exactly like clicking it.
//
// A minimized session that rings its bell is restored on the way in and
// re-minimized on the way back (see endRound) — the tray is where you
// put sessions you don't want to look at, and glancing at one because it
// asked for you shouldn't be what drags it back into the grid for good.
export function jumpToAttention() {
  const id = nextAttentionId();
  if (!id) {
    // The active session can carry a stale flag: onSessionDeath adds
    // attention unconditionally, even for the session you're looking at.
    // nextAttentionId skips the active session, so without this the row
    // would pulse while ⌘B insists nothing needs attention.
    const staleId = appData().activeId;
    const staleSession = appData().sessions.find((s) => s.id === staleId);
    if (staleSession && readNeedsAttention(staleSession)) {
      clearAttention(staleId as string);
    }
    flashStatus('no sessions need attention');
    return;
  }
  // Landing back on the anchor closes the round: you are already where
  // ⇧⌘B would take you, so release the slot rather than leave a
  // no-op jump-back armed (this happens when the session you were
  // working in rings its own bell mid-round). Sessions restored earlier
  // in the round stay restored — re-minimizing them while the user sits
  // on the anchor would yank tiles out from under them with no keypress
  // to explain it.
  if (id === appData().attentionReturnId) endRound({ reminimize: false });
  else if (!appData().attentionReturnId)
    setAttentionReturnId(appData().activeId);

  if (isSessionHidden(id)) {
    // Record which of the two mechanisms was hiding it, so ⇧⌘B can put
    // back exactly what ⌘B pulled out — the session, its project, or
    // both.
    if (appData().minimized.has(id)) addAttentionRestored(id);
    const pid = readProjectId(appData().sessions.find((s) => s.id === id));
    if (pid && appData().minimizedProjects.has(pid)) {
      addAttentionRestoredProject(pid);
    }
    restoreSession(id); // un-minimize + re-render tray, then switchTo
  } else {
    switchTo(id);
  }
}

// jumpBack (⇧⌘B) returns to the session held before the first ⌘B and
// ends the round, so the next ⌘B starts a fresh one. The anchored
// session can be killed while you're away, hence the still-exists guard.
export function jumpBack() {
  const id = appData().attentionReturnId;
  if (!id || !appData().sessions.some((s) => s.id === id)) {
    endRound({ reminimize: true }); // still tidy up any restored tiles
    flashStatus('nowhere to jump back to');
    return;
  }
  // Move focus home BEFORE re-minimizing: minimizeSession hands focus to
  // another visible tile when it hides the active one, which would fight
  // the jump we just made.
  switchTo(id);
  endRound({ reminimize: true });
}

// endRound releases the return anchor and, when asked, puts back every
// session ⌘B pulled out of the minimized tray during the round. Sessions
// killed while you were away are dropped rather than re-minimized —
// adding a dead id to appData().minimized would strand a chip in the tray.
function endRound({ reminimize }: { reminimize: boolean }) {
  setAttentionReturnId(null);
  if (reminimize) {
    for (const rid of appData().attentionRestored) {
      if (
        rid !== appData().activeId &&
        appData().sessions.some((s) => s.id === rid)
      ) {
        minimizeSession(rid);
      }
    }
    // Projects last: re-minimizing one hides every session in it, so
    // doing it before the session pass would hide the active session
    // the pass above deliberately skips. The same guard applies —
    // never re-minimize the project you are sitting in.
    const activePID = readProjectId(
      appData().sessions.find((s) => s.id === appData().activeId),
    );
    for (const pid of appData().attentionRestoredProjects) {
      if (pid !== activePID && appData().projects.some((p) => p.id === pid)) {
        minimizeProject(pid);
      }
    }
  }
  clearAttentionRestored();
}

// navBack / navForward (Ctrl+- / Ctrl+Shift+-) walk the session history
// recorded by setActive. withoutNavHistory keeps the replay from being
// recorded as new navigation — otherwise back would immediately push the
// session it just left and the two keys would ping-pong.
//
// sessionExists mirrors jumpBack's still-exists guard: a session on the
// stack can be killed while you are elsewhere, and the stack walk skips
// it rather than dead-ending.
const sessionExists = (id: string) =>
  appData().sessions.some((s) => s.id === id);

// navGo performs the switch a history step resolved to. A minimized
// session has to be restored on the way in, exactly as ⌘B does at
// jumpToAttention: gridScopeSessions filters appData().minimized, so a bare
// switchTo would make the session active with no tile in the grid — the
// sidebar selection moves, nothing appears, and keyboard focus lands on
// <body> where keystrokes are silently dropped.
//
// Unlike ⌘B this does NOT record the restore in attentionRestored:
// that set exists so ⇧⌘B can re-minimize sessions a bell round pulled
// out on your behalf. Going back here is a deliberate "put me in that
// session", so it stays restored.
function navGo(id: string) {
  deps.withoutNavHistory(() => {
    if (isSessionHidden(id))
      restoreSession(id); // un-minimize (session and/or project), then switchTo
    else switchTo(id);
  });
}

export function navBack() {
  const id = goBack(appData().nav, appData().activeId, sessionExists);
  if (!id) {
    flashStatus('nothing to go back to');
    return;
  }
  navGo(id);
}

export function navForward() {
  const id = goForward(appData().nav, appData().activeId, sessionExists);
  if (!id) {
    flashStatus('nothing to go forward to');
    return;
  }
  navGo(id);
}

export function switchToNthSession(n: number) {
  const ord = orderedSessions();
  if (n - 1 < ord.length) switchTo(ord[n - 1].id);
}

// Debug: arm/disarm the scroll tracer. trace.ts latches hive.debug at
// module load, so the new state only takes effect after a reload — do it
// here so the user never needs the devtools console to flip the gate.
export function toggleScrollDebug() {
  let on = false;
  try {
    on = localStorage.getItem('hive.debug') === '1';
  } catch {
    /* storage off */
  }
  try {
    localStorage.setItem('hive.debug', on ? '0' : '1');
  } catch {
    /* storage off */
  }
  // The reload is intentional and unavoidable: trace.ts reads hive.debug
  // once at module load, so the new state only takes effect on a fresh load.
  // The menu label says "(Reloads)" so this isn't a surprise.
  location.reload();
}

// Debug: copy the captured scroll trace to the clipboard via the Go side
// (works without devtools and without a clipboard user-gesture). Reuses
// window.__hive_dumpscroll (trace.ts) so the dump shape matches what the
// e2e harness and bug reports expect.
export function copyScrollTrace() {
  const dump =
    typeof window.__hive_dumpscroll === 'function'
      ? window.__hive_dumpscroll()
      : {
          enabled: false,
          ring: window.__hive_scrolltrace || [],
          lastJump: null,
        };
  SetClipboardText(JSON.stringify(dump)).catch(
    reportFailure('copy debug trace'),
  );
  const n = dump.ring?.length ?? 0;
  const body = dump.enabled
    ? `Copied ${n} trace event${n === 1 ? '' : 's'} to the clipboard.`
    : 'Debug Trace is OFF — run "Toggle Debug Trace" first, reload, reproduce, then copy.';
  Notify('Hive', 'Debug Trace', body, 'scroll-trace').catch(() => {});
}

// ---------- delete project ----------

// confirmAndDeleteProject is the single confirm + KillProject path
// shared by the sidebar's delete button and the ⇧⌘⌫ shortcut. Kept as one
// function so the prompt text and killSessions logic can't drift.
export async function confirmAndDeleteProject(
  proj: ProjectInfo | undefined | null,
) {
  if (!proj) return;
  const sessions = appData().sessions.filter(
    (s) => (s.projectId ?? s.project_id) === proj.id,
  );
  const msg = sessions.length
    ? `Delete project "${proj.name}" and kill ${sessions.length} session${sessions.length === 1 ? '' : 's'}?`
    : `Delete project "${proj.name}"?`;
  const ok = await Confirm('Delete project', msg);
  if (!ok) return;
  // deleteIdeas=false: the daemon refuses with project_has_ideas when
  // the project still holds open ideas, and app/events.ts asks that
  // second question — losing captured notes is a separate consent from
  // losing the project.
  KillProject(proj.id, sessions.length > 0, false).catch(
    reportFailure('delete project'),
  );
}

export function deleteActiveProject() {
  const pid = activeProjectId();
  confirmAndDeleteProject(appData().projects.find((p) => p.id === pid));
}

// The worktree browser is per-project, so the keyboard and palette
// paths resolve the project the same way every other project-scoped
// action does.
export function openWorktreesForActiveProject() {
  const pid = activeProjectId();
  openWorktrees(appData().projects.find((p) => p.id === pid) ?? null);
}

// The inbox is per-project too, so it resolves the project the same
// way. With no project at all this opens nothing rather than falling
// back to the first one: ⇧⌘I is a stray-keystroke away from ⌘I, and an
// unrelated project's inbox is a worse answer than none.
export function openIdeaInboxForActiveProject() {
  const pid = activeProjectId();
  openIdeaInbox(appData().projects.find((p) => p.id === pid) ?? null);
}

// ---------- ⌘I / ⇧⌘I ----------
//
// One implementation each, behind the 'quick-idea' / 'idea-inbox'
// commands that the keydown bindings, the native menu and the palette all
// run. On macOS the menu accelerator swallows the chord before the
// webview (see MENU_COMMANDS in commands.ts), so the menu is the only
// path that runs there; on Windows and Linux there is no menu at all and only the
// keydown path runs. Keeping the decision in one place is what stops
// the two platforms from behaving differently — the first version of
// this feature put it in the keydown branch alone and was inert on mac.

// ideaKeysBlocked keeps the menu path in line with the keyboard. The
// window listener never reaches the ⌘I binding while an exclusive scope
// sits above it, so a menu item that punched through those scopes would
// give macOS a behaviour no other platform has — a capture sheet over the
// settings dialog, or over a question about deleting a worktree. Derived
// from KEY_SCOPES, so a new modal blocks ⌘I without anyone remembering to
// add it here. The two idea scopes are excluded: ⌘I and ⇧⌘I are their own
// toggles.
function ideaKeysBlocked(): boolean {
  return exclusiveScopeOpen(IDEA_SCOPES);
}

// captureIdea is ⌘I: open the capture sheet, close it if it is already
// up, and — from the inbox — close the inbox first and carry ITS
// project into the sheet. That last part is two bugs avoided rather
// than a nicety: every .hv-dialog shares z-index 40 and #idea-inbox is
// later in index.html, so a sheet left over it paints BEHIND it; and
// openQuickIdea() with no argument prefills activeProjectId(), the
// focused session's project, so filing from another project's inbox
// would land the note somewhere the user never looked.
export function captureIdea() {
  if (ideaKeysBlocked()) return;
  if (isModalOpen('quick-idea')) {
    closeQuickIdea();
    return;
  }
  if (isModalOpen('idea-inbox')) {
    const projectId = ideaInboxProjectId();
    closeIdeaInbox();
    openQuickIdea(projectId);
    return;
  }
  openQuickIdea();
}

// toggleIdeaInbox is ⇧⌘I. The capture sheet closes first: it is the
// smaller thing and it is what the user just came from, and leaving it
// up would put it behind the panel.
export function toggleIdeaInbox() {
  if (ideaKeysBlocked()) return;
  if (isModalOpen('idea-inbox')) {
    closeIdeaInbox();
    return;
  }
  if (isModalOpen('quick-idea')) closeQuickIdea();
  openIdeaInboxForActiveProject();
}

// moveActiveSession walks the (project_order, session_order) list.
// reorder=true moves the session within its project only.
export function moveActiveSession(delta: number, reorder: boolean) {
  const ord = orderedSessions();
  const n = ord.length;
  if (n === 0) return;
  const idx = ord.findIndex((s) => s.id === appData().activeId);
  if (idx < 0) {
    // No active session (an empty project is selected, or the last one
    // was closed): seed on the first VISIBLE session. ord[0] may be
    // minimized, and switchTo does not un-minimize — only restoreSession
    // does — so seeding blind is the same "moved into the tray" failure
    // the walk below exists to prevent.
    const first = ord.find((s) => !isSessionHidden(s.id));
    if (first) switchTo(first.id);
    return;
  }
  if (reorder) {
    // Moving one row can take several UpdateSession calls: a session in a
    // shared worktree moves with its whole group, and each call is one
    // delete-then-insert in the daemon's flat r.order. The indices are
    // computed up front against a simulated list (lib/worktree-groups.ts),
    // so they must be applied in order — and the sequence stops at the
    // first failure rather than leaving the group scattered.
    // Hidden sessions are not slots (#407): the sidebar does not paint them,
    // so swapping with one would look like a dead press.
    const ops = clusterReorderOps(
      appData().sessions,
      appData().activeId,
      delta,
      { has: isSessionHidden },
    );
    if (ops.length === 0) return;
    void runReorder(ops);
    return;
  }
  // Step OVER minimized sessions (their own tray, or their project's):
  // you put them away, so the arrows must not walk you back into them —
  // in a grid view landing on one has no tile and drops you to single.
  // The walk is over the full ordered list, not a filtered one, because
  // orderedSessions() is shared with the tray, palette and ⌘1-9, which
  // still list everything — and all of which see the same clustered order
  // the sidebar paints. The sidebar itself drops minimized rows (#407), so
  // every row it shows besides the active one is a stop on this walk.
  // Math.sign: the walk visits every slot for any delta, not only ±1.
  const step = Math.sign(delta) || 1;
  for (let i = 1; i < n; i++) {
    const cand = ord[(((idx + step * i) % n) + n) % n];
    if (!isSessionHidden(cand.id)) {
      switchTo(cand.id);
      return;
    }
  }
  // Full circle, nothing visible — stay put rather than teleport into
  // the tray.
}
