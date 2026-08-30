// ---------- bell + attention + daemon events ----------
//
// Moved verbatim from main.js. wireDaemonEvents(deps) registers every
// EventsOn handler; view/focus callbacks and the scroll tracer are
// injected because they live in main.ts until later stages.

import {
  EventsOn,
  Notify,
  KillSession,
  KillSessionAndWorktree,
  ConnectControl,
  LogFrontend,
} from '../bridge.js';
import { state, saveCollapsed, saveMinimizedProjects } from './state.js';
import type { SessionInfo, ProjectInfo } from './state.js';
import { setStatus, flashStatus, reportFailure, setBootState } from './dom.js';
import { orderedSessions } from './selectors.js';
import {
  renderSidebar,
  updateSidebarSelection,
  updateSidebarTitles,
} from './sidebar.js';
import { pruneCollapsed } from '../lib/collapsed.js';
import { handleWorktreesPayload } from './modals/worktrees.js';
import { openChoiceDialog } from './modals/choice-dialog.js';
import type { WorktreesPayload } from '../lib/worktrees.js';
import { phaseOf, isReady, isClosing } from '../lib/phase-steps.js';
import { pruneNav } from '../lib/nav-history.js';
import { handleScrollbackEvent, abandonReplays } from '../lib/scrollback.js';
import { createScrollTrace } from '../lib/scroll-debug.js';
import type { ScrollTrace } from '../lib/scroll-debug.js';

export interface EventsDeps {
  switchTo: (id: string) => void;
  renderMinimizedTray: () => void;
  // renderGrid / enforceViewFloor come through the deps seam rather
  // than a direct view.ts import: view.ts pulls in sidebar and the
  // modals, and events.ts is deliberately kept out of that graph.
  renderGrid: () => void;
  enforceViewFloor: () => void;
  updateAppTitle: () => void;
  focusActiveTerm: () => void;
  refocusActiveTerm: () => void;
  isDaemonRestarting: () => boolean;
  // Unlike view.ts's Pick<…, 'rec' | 'count'>, the pty:data probe also
  // reads `counters` directly.
  scrollTrace: Pick<ScrollTrace, 'rec' | 'count' | 'counters'>;
}

// Pre-wireDaemonEvents stub. A real disabled tracer rather than a
// hand-rolled `{ rec }` literal, same as view.ts: enabled:false
// short-circuits inside rec/count, so the no-op behavior is identical
// and the stub can't drift out of the interface.
let deps: EventsDeps = {
  switchTo: () => {},
  renderMinimizedTray: () => {},
  renderGrid: () => {},
  enforceViewFloor: () => {},
  updateAppTitle: () => {},
  focusActiveTerm: () => {},
  refocusActiveTerm: () => {},
  isDaemonRestarting: () => false,
  scrollTrace: createScrollTrace({ enabled: false }),
};

// Wire payloads the daemon pushes over EventsOn. Hand-written for the
// same reason SessionInfo is (state.ts): these cross as raw JSON, never
// as a bound method's return, so they are absent from wailsjs/go/models.
interface ProjectEvent {
  kind: 'added' | 'removed' | 'updated';
  project: ProjectInfo;
}

interface SessionEvent {
  // 'title' is a session-only kind: the program on the PTY re-titled
  // itself (internal/wire/control.go SessionEventTitle). Kept separate
  // from 'updated' so title churn never triggers a full re-render.
  kind: 'added' | 'removed' | 'updated' | 'title';
  session: SessionInfo;
}

interface ControlError {
  code?: string;
  message?: string;
  session_id?: string;
}

interface PtyError {
  code?: string;
  message?: string;
}

// Control-connection reconnect loop. Guarded so overlapping disconnect
// events don't spawn parallel loops. Backoff climbs 500ms → 5s cap so a
// daemon that's slow to come back doesn't get hammered. Stops if a
// RestartDaemon takes over (that path spawns a fresh GUI) or once
// ConnectControl succeeds — the daemon then re-pushes the session list.
let _reconnecting = false;
export async function reconnectControl(
  maxAttempts = Infinity,
): Promise<boolean> {
  if (_reconnecting) return false;
  _reconnecting = true;
  let delay = 500;
  let attempts = 0;
  try {
    for (;;) {
      if (deps.isDaemonRestarting()) return false; // fresh GUI is taking over
      try {
        await ConnectControl();
        setStatus('connected');
        try {
          LogFrontend('control reconnected');
        } catch {
          /* bridge absent in tests */
        }
        return true;
      } catch (err) {
        try {
          LogFrontend(`control reconnect failed: ${err}`);
        } catch {
          /* ignore */
        }
        attempts += 1;
        // A drop mid-session retries forever: the daemon existed a
        // moment ago. The boot path passes a cap instead — a daemon
        // that never came up is a failure to surface, not one to keep
        // dialing (and every failed dial can spawn a fresh hived).
        if (attempts >= maxAttempts) return false;
        await new Promise((r) => setTimeout(r, delay));
        delay = Math.min(delay * 2, 5000);
      }
    }
  } finally {
    _reconnecting = false;
  }
}

// onSessionBell is fired by SessionTerm whenever its xterm receives
// BEL. Active + window-focused session: ignore. Otherwise: mark
// attention, repaint sidebar, and fire a desktop notification — but
// only on the transition from no-attention → attention, so a session
// emitting bells in a tight loop doesn't spam the OS notification
// center.
export function onSessionBell(info: SessionInfo) {
  const isActive = info.id === state.activeId;
  const windowFocused = document.hasFocus();
  if (isActive && windowFocused) return;
  const alreadyAttention = state.attention.has(info.id);
  if (alreadyAttention) {
    // Refresh to re-trigger CSS animation.
    state.attention.delete(info.id);
    state.terms.get(info.id)?.host.classList.remove('attention');
  }
  state.attention.add(info.id);
  state.terms.get(info.id)?.host.classList.add('attention');
  state.terms.get(info.id)?.refreshStateIcon?.();
  updateSidebarSelection();
  deps.renderMinimizedTray();
  if (!alreadyAttention) fireBellNotification(info);
}

export function clearAttention(sessionId: string) {
  if (state.attention.delete(sessionId)) {
    state.terms.get(sessionId)?.host.classList.remove('attention');
    state.terms.get(sessionId)?.refreshStateIcon?.();
    updateSidebarSelection();
    deps.renderMinimizedTray();
  }
}

// fireBellNotification routes through Go because Wails' WKWebView on
// macOS doesn't implement the HTML5 Notification API. The Go side
// dispatches per-platform (NSUserNotification / notify-send / Windows
// toast). The session id is passed as the tag so the OS can dedupe
// repeated bells from the same session and the click handler knows
// which session to switch to.
function fireBellNotification(info: SessionInfo) {
  const proj = state.projects.find(
    (p) => p.id === (info.projectId ?? info.project_id),
  );
  const projectName = proj?.name ?? '';
  const title = info.name || 'Session';
  const subtitle = projectName;
  const body = 'Waiting for input — click to switch.';
  Notify(title, subtitle, body, info.id).catch(() => {
    // Best-effort; the visual sidebar pulse covers the user even if
    // the OS notification fails (no notify-send installed, etc.).
  });
}

// onSessionDeath fires once when a session transitions Alive→dead.
// Shows the in-tile overlay, marks attention, and posts a desktop
// notification distinct from a normal bell.
function onSessionDeath(info: SessionInfo) {
  state.dismissedDead.delete(info.id);
  const t = state.terms.get(info.id);
  if (t) {
    // Flip attached eagerly so a switch-back before pty:disconnect arrives
    // doesn't try to reuse the dying connection.
    t.attached = false;
    t.setDead(
      true,
      info.last_error || 'The process running in this session has exited.',
    );
  }
  // Reuse the attention pulse path so the sidebar entry highlights.
  state.attention.add(info.id);
  state.terms.get(info.id)?.host.classList.add('attention');
  updateSidebarSelection();
  const proj = state.projects.find(
    (p) => p.id === (info.projectId ?? info.project_id),
  );
  // Best-effort like fireBellNotification: the overlay + sidebar pulse
  // already cover the user if the OS notification fails.
  Notify(
    info.name || 'Session',
    proj?.name ?? '',
    'Session ended.',
    info.id,
  ).catch(() => {});
}

// neighbourOf returns the session to focus when `id` goes away: the
// one before it in display order, or the one after when it's first.
function neighbourOf(id: string): string | null {
  const ord = orderedSessions();
  const idx = ord.findIndex((s) => s.id === id);
  if (idx < 0) return null;
  const nb = idx > 0 ? ord[idx - 1] : ord[idx + 1];
  return nb?.id ?? null;
}

export function wireDaemonEvents(injected: EventsDeps) {
  deps = injected;

  // Whenever the window regains focus, clear the active session's
  // attention state — the user is presumably looking at it. Also
  // restore xterm focus: macOS fullscreen toggles, ⌘-tab returns, and
  // menu actions can leave the window focused but no element inside it,
  // so typing would land on the body and be lost.
  window.addEventListener('focus', () => {
    if (state.activeId) clearAttention(state.activeId);
    deps.refocusActiveTerm();
  });

  EventsOn('project:list', (jsonStr: string) => {
    const { projects } = JSON.parse(jsonStr) as { projects?: ProjectInfo[] };
    state.projects = projects || [];
    if (!state.currentProjectId && state.projects[0]) {
      state.currentProjectId = state.projects[0].id;
    }
    // Drop persisted collapse / minimize entries for projects that no
    // longer exist so the localStorage keys can't grow forever. Both
    // prunes live here, not in renderSidebar: this event is the arrival
    // of authoritative project data, and pruning against a
    // not-yet-populated state.projects would wipe the sets instead.
    const ids = state.projects.map((p) => p.id);
    const pruned = pruneCollapsed(state.collapsed, ids);
    if (pruned.changed) {
      state.collapsed = pruned.set;
      saveCollapsed();
    }
    const prunedMin = pruneCollapsed(state.minimizedProjects, ids);
    if (prunedMin.changed) {
      state.minimizedProjects = prunedMin.set;
      saveMinimizedProjects();
    }
    renderSidebar();
  });

  // The daemon answers LIST_WORKTREES — and every worktree mutation —
  // with a WORKTREES frame, fanned out as this event. The browser
  // re-renders from it; nothing else in the app reads worktree
  // inventory.
  EventsOn('worktree:list', (jsonStr: string) => {
    let payload: WorktreesPayload;
    try {
      payload = JSON.parse(jsonStr) as WorktreesPayload;
    } catch {
      flashStatus('bad worktree payload', true);
      return;
    }
    handleWorktreesPayload(payload);
  });

  EventsOn('project:event', (jsonStr: string) => {
    const ev = JSON.parse(jsonStr) as ProjectEvent;
    const i = state.projects.findIndex((p) => p.id === ev.project.id);
    if (ev.kind === 'added') {
      if (i < 0) state.projects.push(ev.project);
      // First-ever project: make it current.
      if (!state.currentProjectId) state.currentProjectId = ev.project.id;
    } else if (ev.kind === 'removed') {
      if (i >= 0) state.projects.splice(i, 1);
      if (state.collapsed.delete(ev.project.id)) saveCollapsed();
      if (state.minimizedProjects.delete(ev.project.id)) {
        saveMinimizedProjects();
      }
      if (state.currentProjectId === ev.project.id) {
        state.currentProjectId = state.projects[0]?.id ?? null;
      }
    } else if (ev.kind === 'updated') {
      if (i >= 0) state.projects[i] = ev.project;
      // Refresh tile-header project color for every session belonging
      // to this project so grid/single-mode title bars reflect rename
      // and recolor in real time.
      for (const s of state.sessions) {
        const pid = s.projectId ?? s.project_id;
        if (pid !== ev.project.id) continue;
        const st = state.terms.get(s.id);
        if (st) st.setProject(ev.project.name, ev.project.color);
      }
    }
    state.projects.sort((a, b) => (a.order ?? 0) - (b.order ?? 0));
    renderSidebar();
  });

  // processAliveTransition compares incoming Alive against the last
  // known value for this session and fires the death/revive side
  // effects on the boundary. First sight of a session (no prior entry)
  // just records the value without firing anything.
  function processAliveTransition(info: SessionInfo) {
    const prev = state.aliveById.get(info.id);
    const phase = phaseOf(info);
    const prevPhase = state.phaseById.get(info.id);
    const wasPending = prevPhase !== undefined && !isReady(prevPhase);
    state.aliveById.set(info.id, !!info.alive);
    state.phaseById.set(info.id, phase);
    // A session that hasn't finished starting is not dead — it has no
    // PTY *yet*. Death is only meaningful once the daemon says ready.
    if (!isReady(phase)) return;
    if (prev === true && info.alive === false) {
      onSessionDeath(info);
    } else if (prev === undefined && info.alive === false) {
      // Session was born dead (e.g. agent binary not found).
      onSessionDeath(info);
    } else if (prev === false && info.alive === false && wasPending) {
      // Reached ready still dead: the spawn failed. Alive-transition
      // detection can't see this — `added` already recorded
      // alive:false while the session was merely starting, so this is
      // false→false — and without the explicit call a born-dead
      // session would sit under the loading panel forever.
      onSessionDeath(info);
    } else if (prev === false && info.alive === true) {
      state.dismissedDead.delete(info.id);
      const t = state.terms.get(info.id);
      if (t) {
        // Wipe stale frame from the previous (dead) shell so the revived
        // session's prompt lands on a clean screen instead of stacking on
        // the old cursor position.
        try {
          t.term?.reset();
        } catch {}
        abandonReplays(t); // the wipe abandons any in-flight restream
        t.attached = false;
        t.setDead(false);
        // Re-open the dial the revived PTY needs. Without it the
        // cleared tile sits blank until the next switchTo/render/
        // resize. Unconditional like the ready-edge attach in
        // setPhase (the restart path gates on visibility instead);
        // safe because ensureAttached is idempotent and gated on
        // phase, liveness and a non-zero box.
        t.ensureAttached();
      }
    }
  }

  EventsOn('session:list', (jsonStr: string) => {
    // First list = the daemon answered. Anything the pane renders from
    // here on is the truth, so the boot overlay's job is done.
    setBootState(null);
    const { sessions } = JSON.parse(jsonStr) as { sessions?: SessionInfo[] };
    state.sessions = sessions || [];
    for (const s of state.sessions) {
      processAliveTransition(s);
      state.terms.get(s.id)?.setPhase(phaseOf(s));
    }
    // Drop any ids whose sessions no longer exist (e.g. after a daemon
    // restart or list reset) so the tray doesn't leak stale chips and
    // the transition-detection maps don't grow for the life of the
    // process. A snapshot is the only path that can retire a session
    // without a per-session `removed` event.
    const liveIds = new Set(state.sessions.map((s) => s.id));
    for (const id of Array.from(state.minimized)) {
      if (!liveIds.has(id)) state.minimized.delete(id);
    }
    for (const id of Array.from(state.aliveById.keys())) {
      if (!liveIds.has(id)) state.aliveById.delete(id);
    }
    for (const id of Array.from(state.phaseById.keys())) {
      if (!liveIds.has(id)) state.phaseById.delete(id);
    }
    pruneNav(state.nav, (id) => liveIds.has(id));
    renderSidebar();
    deps.renderMinimizedTray();
    if (!state.activeId && state.sessions.length > 0) {
      deps.switchTo(orderedSessions()[0].id);
    }
  });

  EventsOn('session:event', (jsonStr: string) => {
    const ev = JSON.parse(jsonStr) as SessionEvent;
    const i = state.sessions.findIndex((s) => s.id === ev.session.id);
    if (ev.kind === 'added' || ev.kind === 'updated') {
      processAliveTransition(ev.session);
    }
    if (ev.kind === 'added') {
      if (i < 0) state.sessions.push(ev.session);
      renderSidebar();
      deps.switchTo(ev.session.id);
      return;
    }
    // The program on the PTY re-titled itself. Its own kind, not an
    // `updated`, precisely so it can take the cheap path: patch the
    // sidebar's title line in place and touch nothing else. A full
    // renderSidebar() here would wipe and rebuild every row at whatever
    // rate the child process redraws, eating dblclick pairs on the way
    // (see updateSidebarSelection's comment).
    if (ev.kind === 'title') {
      if (i >= 0) state.sessions[i] = ev.session;
      updateSidebarTitles();
      return;
    }
    if (ev.kind === 'updated' && isClosing(phaseOf(ev.session))) {
      // Don't make the user watch a teardown: the moment the daemon
      // starts closing, hand focus to the neighbour. The tile itself
      // stays (dimmed) until `removed` lands, which can be seconds
      // later on a big worktree.
      if (state.activeId === ev.session.id) {
        const next = neighbourOf(ev.session.id);
        if (next) deps.switchTo(next);
      }
    }
    if (ev.kind === 'removed') {
      state.aliveById.delete(ev.session.id);
      state.phaseById.delete(ev.session.id);
      state.dismissedDead.delete(ev.session.id);
      state.minimized.delete(ev.session.id);
      const nextId =
        state.activeId === ev.session.id ? neighbourOf(ev.session.id) : null;
      if (i >= 0) state.sessions.splice(i, 1);
      const t = state.terms.get(ev.session.id);
      if (t) {
        t.destroy();
        state.terms.delete(ev.session.id);
      }
      // Prune AFTER the splice above: until then the removed id is
      // still in state.sessions, so an exists-check would keep it.
      pruneNav(state.nav, (id) => state.sessions.some((s) => s.id === id));
      if (state.activeId === ev.session.id) {
        state.activeId = null;
        if (nextId) deps.switchTo(nextId);
      }
      // Killing the second-to-last tile leaves a one-tile grid, which is
      // the degenerate state setView refuses to enter.
      deps.enforceViewFloor();
      // Repaint the grid: one of its cells just went away. Previously
      // the only repaint on this path was the switchTo above, which
      // fires solely when the *active* tile was the one killed — so a
      // grid that lost any other tile kept a stale layout. Focus now
      // moves at `closing`, so by the time `removed` lands the active
      // id is already the neighbour and switchTo never runs; without
      // this, closing a session never refreshed the grid at all.
      if (state.view !== 'single') deps.renderGrid();
    } else if (ev.kind === 'updated') {
      // A reorder arrives as `updated` events carrying new .order
      // values. renderGrid appends tiles in gridScopeSessions order, so
      // without a repaint the sidebar reorders while the tiles keep
      // their stale DOM order.
      const prevOrder = i >= 0 ? state.sessions[i].order : undefined;
      if (i >= 0) state.sessions[i] = ev.session;
      if (prevOrder !== ev.session.order && state.view !== 'single') {
        deps.renderGrid();
      }
      // Push the new name/color/worktree branch into the cached
      // SessionTerm so the grid tile-header refreshes immediately.
      // Without this, renames look broken in grid mode — the sidebar
      // updates but the tile keeps showing the old name.
      const st = state.terms.get(ev.session.id);
      if (st) {
        st.setInfo(ev.session);
        // Phase drives the loading panel and the attach gate. Set it
        // after setInfo so the panel's labels (branch, agent) come
        // from the fresh info.
        st.setPhase(phaseOf(ev.session));
        const pid = ev.session.projectId ?? ev.session.project_id;
        const proj = state.projects.find((p) => p.id === pid);
        st.setProject(proj?.name ?? '', proj?.color ?? '');
        // Restart Session path: pty:disconnect already flipped attached
        // off and set needsReattach. Now that the daemon has confirmed
        // a fresh alive=true PTY, reattach the visible term so its
        // resumed stream starts flowing without a manual switch.
        // Hidden terms are left dirty; switchTo/showSingle/renderGrid
        // will ensureAttached when they next become visible.
        if (st.needsReattach && ev.session.alive) {
          st.needsReattach = false;
          try {
            st.term?.reset();
          } catch {}
          abandonReplays(st); // the wipe abandons any in-flight restream
          const visible =
            (state.view === 'single' && state.activeId === ev.session.id) ||
            (state.view !== 'single' && st.host.classList.contains('in-grid'));
          if (visible) {
            st.ensureAttached();
            if (state.activeId === ev.session.id) deps.focusActiveTerm();
          }
        }
      }
      if (state.activeId === ev.session.id) deps.updateAppTitle();
    }
    renderSidebar();
    if (ev.kind === 'removed' || ev.kind === 'updated')
      deps.renderMinimizedTray();
  });

  EventsOn('pty:data', (id: string, b64: string) => {
    // Daemon-traffic probe: is the freeze the daemon flooding us? Count
    // every inbound frame + (base64) byte volume, and drop a timestamped
    // checkpoint every 200 frames so a flood shows as a steep bytes/sec
    // slope to line up against the heartbeat-stall timestamps. Cheap and
    // gated; counters ride the dump (rotation-proof) so the total survives
    // even when the ring churns under a flood.
    if (deps.scrollTrace.rec.enabled) {
      deps.scrollTrace.count('ptyFrames');
      deps.scrollTrace.count('ptyB64Bytes', b64 ? b64.length : 0);
      if (deps.scrollTrace.counters.ptyFrames % 200 === 0) {
        deps.scrollTrace.rec('pty-checkpoint', {
          frames: deps.scrollTrace.counters.ptyFrames,
          b64Bytes: deps.scrollTrace.counters.ptyB64Bytes,
        });
      }
    }
    state.terms.get(id)?.writeData(b64);
  });

  EventsOn('pty:event', (id: string, jsonStr: string) => {
    try {
      const ev = JSON.parse(jsonStr) as { kind: string };
      const st = state.terms.get(id);
      if (!st) return;
      // Begin: wipe xterm so replay paints onto a clean slate (otherwise
      // the new bytes would overlay whatever's already rendered — the
      // bug-2 symptom). Done: scroll to bottom so the user lands at the
      // cursor. Wire-order is what guarantees no live bytes land
      // between Begin and Done — see daemon's SubscribeWithAtomicReplay
      // and EmitAtomicReplay.
      if (deps.scrollTrace.rec.enabled) {
        const buf = st.term?.buffer?.active;
        // Count replay events by kind too: a daemon stuck re-streaming the
        // scrollback ring shows up as a runaway scrollback_replay_begin
        // count (the #222/#228/#232 suspects), distinct from a raw data
        // flood (ptyFrames). A storm here = the daemon is the freeze.
        deps.scrollTrace.count(`ptyEvent:${ev.kind}`);
        deps.scrollTrace.rec(ev.kind, {
          id,
          viewportY: buf?.viewportY,
          baseY: buf?.baseY,
          wants: st._replayWantsBottom,
        });
        // Mark replay activity so the scroll-jump detector can label a
        // following up-move as replay-driven (tiny sinceReplayMs) vs an
        // unrelated renderer/resize jump.
        try {
          st._lastReplayTs = performance.now();
        } catch {
          /* no perf clock */
        }
      }
      handleScrollbackEvent(
        st,
        ev.kind,
        deps.scrollTrace.rec.enabled ? deps.scrollTrace.rec : undefined,
      );
      // Replay done = the daemon has finished painting the settled
      // screen, which is the cue to drop the loading panel.
      if (ev.kind === 'scrollback_replay_done') st.revealAfterReplay();
    } catch {
      /* ignore */
    }
  });

  EventsOn('pty:disconnect', (id: string) => {
    const st = state.terms.get(id);
    if (st) {
      st.attached = false;
      if (isClosing(st.phase)) {
        // The daemon killed this PTY on its way to removing the
        // session. Marking it for reattach would send us dialing a
        // session that is being deleted.
        abandonReplays(st);
        return;
      }
      // The connection dropped: any in-flight replay's done will never
      // arrive, so clear the in-flight count (else it pins the viewport
      // to the bottom forever after reattach).
      abandonReplays(st);
      // Mark the term as needing reattach. Restart Session closes the
      // daemon-side PTY (which lands here) and respawns; the subsequent
      // session:event(updated, alive=true) is where we re-OpenSession.
      st.needsReattach = true;
    }
  });

  EventsOn('pty:error', (id: string, jsonStr: string) => {
    const st = state.terms.get(id);
    // A session mid-create or mid-teardown is *expected* to refuse
    // (session_starting / no_such_session); painting that in red into
    // the pane is how a normal close used to look broken.
    if (st && isReady(st.phase)) {
      try {
        const e = JSON.parse(jsonStr) as PtyError;
        st.term?.write?.(
          `\r\n\x1b[31m[hived: ${e.code}: ${e.message}]\x1b[0m\r\n`,
        );
      } catch {}
    }
  });

  EventsOn('control:disconnect', () => {
    // During a user-initiated RestartDaemon we knowingly close the
    // control conn; the banner already says "Restarting hived…". Don't
    // also flash an alarming red status line in that window.
    if (deps.isDaemonRestarting()) return;
    // The control conn is the GUI's only channel for session/project
    // state and commands. Before, a drop (macOS sleep/wake, a daemon
    // replaced by an upgrade) was terminal: ConnectControl ran once at
    // boot and nothing reconnected, so the UI sat frozen until the user
    // restarted. Retry with backoff — ConnectControl is idempotent and
    // the daemon re-pushes a full SESSIONS snapshot on connect, so the
    // UI re-syncs automatically once it's back.
    setStatus('reconnecting…', true);
    reconnectControl();
  });

  // User clicked a notification toast. Route to that session in the
  // current view (single keeps single, grid keeps grid) without toggling
  // modes. switchTo handles the view-aware repaint.
  EventsOn('bell-click', (sessionId: string) => {
    if (!sessionId) return;
    const info = state.sessions.find((s) => s.id === sessionId);
    if (!info) return;
    deps.switchTo(sessionId);
    clearAttention(sessionId);
  });

  EventsOn('control:error', async (jsonStr: string) => {
    let e: ControlError;
    try {
      e = JSON.parse(jsonStr) as ControlError;
    } catch {
      flashStatus('hived error', true);
      return;
    }
    // Worktree-dirty kill: confirm with the user. The daemon already
    // refused to kill, so we can safely retry with force=true if the
    // user accepts.
    if (e.code === 'worktree_dirty' && e.session_id) {
      const sess = state.sessions.find((s) => s.id === e.session_id);
      const branch =
        sess?.worktreeBranch ?? sess?.worktree_branch ?? 'this worktree';
      const answer = await openChoiceDialog({
        title: 'Close this session anyway?',
        detail: sess?.name ?? 'Session',
        bullets: [`It has uncommitted changes in ${branch}.`],
        note:
          'Closing keeps the worktree and its changes — find it under ' +
          'Worktrees (⌘E) to resume or delete later. Cleaning up deletes ' +
          'the worktree and those changes now, which cannot be undone.',
        choices: [
          { label: 'Cancel', value: 'cancel' },
          { label: 'Close session', value: 'close' },
          {
            label: 'Close and delete worktree',
            value: 'close-and-clean',
            danger: true,
          },
        ],
      });
      if (answer === 'cancel') return;
      // The dialog is async + modal; the session may have been removed
      // (or its worktree resolved) while the dialog was open. Re-check
      // before issuing a second kill that would just produce a confusing
      // "no_such_session" control error.
      if (!state.sessions.find((s) => s.id === e.session_id)) return;
      if (answer === 'close-and-clean') {
        // One daemon-side operation: the worktree is occupied until
        // this session is gone, so closing and then asking to remove it
        // would race its own teardown and be refused as in-use.
        KillSessionAndWorktree(e.session_id).catch(
          reportFailure('close and delete worktree'),
        );
        return;
      }
      KillSession(e.session_id, true).catch(reportFailure('force kill'));
      return;
    }
    // Worktree-browser refusals. worktree_in_use is not overridable,
    // so there is nothing to confirm — say what to do instead.
    if (e.code === 'worktree_in_use') {
      flashStatus('close the sessions in that worktree first', true);
      return;
    }
    if (e.code === 'worktree_unpushed' || e.code === 'worktree_dirty') {
      flashStatus(`worktree kept: ${e.message}`, true);
      return;
    }
    flashStatus(`${e.code}: ${e.message}`, true);
    console.warn('hived control error:', e);
  });
}
