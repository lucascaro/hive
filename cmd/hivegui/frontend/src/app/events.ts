// ---------- bell + attention + daemon events ----------
//
// Moved verbatim from main.js. wireDaemonEvents(deps) registers every
// EventsOn handler; view/focus callbacks and the scroll tracer are
// injected because they live in main.tsx until later stages.

import {
  EventsOn,
  Notify,
  KillSession,
  KillSessionAndWorktree,
  ConnectControl,
  LogFrontend,
  SetSessionAttention,
} from '../bridge.js';
import {
  noteLocalClose,
  onSessionRemoved,
  onSessionRestored,
} from './undo-close.js';
import type { SessionInfo, ProjectInfo } from './state.js';
import { readNeedsAttention } from './state.js';
import {
  addProject,
  addSession,
  clearDismissedDead,
  forgetSession,
  pruneToLiveSessions,
  removeProject,
  removeSession,
  setActiveId,
  setAlive,
  applyProjectList,
  setSessionPhase,
  setSessions,
  updateProject,
  updateSession,
} from '../store/store.js';
import { deleteTerm, termsMap } from '../store/terms.js';
import { appStore } from '../store/store.js';
import { setStatus, flashStatus, reportFailure, setBootState } from './dom.js';
import { orderedSessions } from './selectors.js';
import { handleWorktreesPayload } from './modals/worktrees.js';
import { openChoiceDialog } from './modals/choice-dialog.js';
import type { WorktreesPayload } from '../lib/worktrees.js';
import { PHASE, phaseOf, isReady, isClosing } from '../lib/phase-steps.js';
import { pruneNav } from '../lib/nav-history.js';
import { handleScrollbackEvent, abandonReplays } from '../lib/scrollback.js';
import { createScrollTrace } from '../lib/scroll-debug.js';
import type { ScrollTrace } from '../lib/scroll-debug.js';

// Live read of the store. A function, not a destructured snapshot: this
// module runs inside event handlers and must never cache a slice across
// a store write.
const appData = () => appStore.getState();

export interface EventsDeps {
  switchTo: (id: string) => void;
  // The sidebar, the minimized tray and the empty-state pane all render
  // themselves from the store since Phase 2 of the React rewrite, so
  // none of them needs a repaint call here any more.
  // enforceViewFloor comes through the deps seam rather than a direct
  // view.ts import: view.ts pulls in sidebar and the modals, and
  // events.ts is deliberately kept out of that graph.
  enforceViewFloor: () => void;
  updateAppTitle: () => void;
  focusActiveTerm: () => void;
  refocusActiveTerm: () => void;
  isDaemonRestarting: () => boolean;
  // Runs the update check. Comes through the deps seam rather than a
  // direct banners.ts import for the same reason enforceViewFloor
  // does: banners.ts is not in events.ts's import graph and should
  // stay out of it.
  checkForUpdates: () => void;
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
  enforceViewFloor: () => {},
  updateAppTitle: () => {},
  focusActiveTerm: () => {},
  refocusActiveTerm: () => {},
  isDaemonRestarting: () => false,
  checkForUpdates: () => {},
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
  // 'title', 'attention' and 'state' are session-only kinds driven by the child
  // process rather than by the daemon's own view of the session — a
  // re-title (SessionEventTitle) and a terminal bell
  // (SessionEventAttention). Both are kept out of 'updated' so that
  // churn from the PTY never triggers the full re-render 'updated'
  // means.
  kind: 'added' | 'removed' | 'updated' | 'title' | 'attention' | 'state';
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

// The PTY BEL byte no longer decides anything: the daemon's bell
// scanner sees the same byte and raises needs_attention through the
// `attention` session event below, which is the only writer of this
// window's copy — there is no local flag left to disagree with it.
// SessionTerm's onBell hook was deleted along with onSessionBell; what
// it existed for (the CSS pulse + the OS notification) now lives here,
// driven off the session's own field.
//
// attentionEdge tracks, per session id, whether this window currently
// believes the session needs attention — ONLY to detect the false→true
// EDGE for a desktop notification. It is not a second "wants the user"
// answer: every reader of that question (sidebar, tile, ⌘B, the pulse
// class) reads session.needs_attention straight off the session list.
// See the frozen transition table,
// docs/exec-plans/active/336-session-state-model.md.
const attentionEdge = new Set<string>();
let sawFirstSessionList = false;

// syncAttentionClass keeps a tile's `.attention` pulse class in sync
// with its session's needs_attention field and fires the notification
// on the edge. `silent` is for the very first session:list snapshot: a
// session that was already waiting before this window opened should be
// visible, not announced — the same rule the old syncAttentionFromSessions
// followed.
function syncAttentionClass(session: SessionInfo, silent = false) {
  const wants = readNeedsAttention(session);
  termsMap().get(session.id)?.host.classList.toggle('attention', wants);
  const was = attentionEdge.has(session.id);
  if (wants && !was) {
    attentionEdge.add(session.id);
    if (silent) return;
    const isActive = session.id === appData().activeId;
    // A session you are already watching gets no desktop notification —
    // a bell is a request, and having the window focused is not an
    // answer to it. noteUserInput/setActive are what answer it.
    if (!(isActive && document.hasFocus())) fireBellNotification(session);
  } else if (!wants && was) {
    attentionEdge.delete(session.id);
  }
}

// noteUserInput records that the user typed into a session, which is
// the one unambiguous "I have seen this" signal there is. Window focus
// is not: a focused window can sit untouched for an hour, and a bell
// that arrives while the session is already active fires no focus event
// at all — which is how a session came to sit marked "waiting for you"
// forever while the person it was waiting for was looking right at it.
//
// Cheap on the hot path by design: this runs per keystroke, and the
// common case is that the session doesn't need attention.
export function noteUserInput(sessionId: string) {
  const s = appData().sessions.find((x) => x.id === sessionId);
  if (!s || !readNeedsAttention(s)) return;
  clearAttention(sessionId);
}

export function clearAttention(sessionId: string) {
  // The flag lives on the daemon, not here. Its reply arrives on the
  // session list / `attention` event like any other client's — that is
  // what drives the class and the edge tracker above. Told regardless of
  // this window's belief: another window may have seen the bell first,
  // or this one may have missed it, and skipping the RPC would leave the
  // menu bar and every other window still insisting the session wants
  // you.
  SetSessionAttention(sessionId, false).catch(reportFailure('clear attention'));
}

// fireBellNotification routes through Go because Wails' WKWebView on
// macOS doesn't implement the HTML5 Notification API. The Go side
// dispatches per-platform (NSUserNotification / notify-send / Windows
// toast). The session id is passed as the tag so the OS can dedupe
// repeated bells from the same session and the click handler knows
// which session to switch to.
function fireBellNotification(info: SessionInfo) {
  const proj = appData().projects.find(
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
  clearDismissedDead(info.id);
  const t = termsMap().get(info.id);
  if (t) {
    // Flip attached eagerly so a switch-back before pty:disconnect arrives
    // doesn't try to reuse the dying connection.
    t.attached = false;
    t.setDead(
      true,
      info.last_error || 'The process running in this session has exited.',
    );
  }
  // The pulse class only: a dead session already renders as `exited`
  // from its state, and the attention flag belongs to the daemon.
  termsMap().get(info.id)?.host.classList.add('attention');
  const proj = appData().projects.find(
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

  // Whenever the window regains focus, restore xterm focus: macOS
  // fullscreen toggles, ⌘-tab returns, and menu actions can leave the
  // window focused but no element inside it, so typing would land on
  // the body and be lost.
  //
  // Deliberately NOT a third place that clears attention: the frozen
  // transition table allows exactly two client-driven clears —
  // noteUserInput (a keystroke) and setActive's switch guard — and a
  // window regaining focus is neither. An earlier version of this
  // handler did clear here too, which is a real "I looked" in some
  // cases but not others (a window can be refocused with the mouse over
  // a background tile), so it is left to the two paths that are always
  // unambiguous.
  window.addEventListener('focus', () => {
    deps.refocusActiveTerm();
  });

  // Commands relayed by the daemon from another client — in practice
  // the menu bar, which has no window of its own and so cannot focus a
  // session or open the update flow directly. reload_gui never gets
  // here: Go handles it (see App.handleClientCommand), because the
  // reload destroys this page and a page cannot be put in charge of
  // that.
  EventsOn('client:command', (jsonStr: string) => {
    let cmd: { cmd?: string; session_id?: string };
    try {
      cmd = JSON.parse(jsonStr);
    } catch {
      return;
    }
    if (cmd.cmd === 'focus_session' && cmd.session_id) {
      // Only if we still have it: the menu bar's list can be a moment
      // stale, and switching to a session that has gone would leave
      // the user staring at an empty pane.
      if (appData().sessions.some((s) => s.id === cmd.session_id)) {
        deps.switchTo(cmd.session_id);
      }
      return;
    }
    if (cmd.cmd === 'check_update') {
      deps.checkForUpdates();
    }
  });

  EventsOn('project:list', (jsonStr: string) => {
    const { projects } = JSON.parse(jsonStr) as { projects?: ProjectInfo[] };
    // setProjects also defaults currentProjectId and drops persisted
    // collapse / minimize entries for projects that no longer exist, so
    // the localStorage keys can't grow forever. That pruning belongs to
    // this event, not to any render path: this is the arrival of
    // authoritative project data, and pruning against a
    // not-yet-populated project list would wipe the sets instead.
    applyProjectList(projects || []);
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
    const i = appData().projects.findIndex((p) => p.id === ev.project.id);
    if (ev.kind === 'added') {
      // addProject also makes a first-ever project current.
      addProject(ev.project);
    } else if (ev.kind === 'removed') {
      // removeProject also drops the id from the two persisted sets and
      // re-points currentProjectId when it was this project.
      removeProject(ev.project.id);
    } else if (ev.kind === 'updated') {
      if (i >= 0) updateProject(ev.project);
      // Refresh tile-header project color for every session belonging
      // to this project so grid/single-mode title bars reflect rename
      // and recolor in real time.
      for (const s of appData().sessions) {
        const pid = s.projectId ?? s.project_id;
        if (pid !== ev.project.id) continue;
        const st = termsMap().get(s.id);
        if (st) st.setProject(ev.project.name, ev.project.color);
      }
    }
  });

  // processAliveTransition compares incoming Alive against the last
  // known value for this session and fires the death/revive side
  // effects on the boundary. First sight of a session (no prior entry)
  // just records the value without firing anything.
  function processAliveTransition(info: SessionInfo) {
    const prev = appData().aliveById.get(info.id);
    const phase = phaseOf(info);
    const prevPhase = appData().phaseById.get(info.id);
    const wasPending = prevPhase !== undefined && !isReady(prevPhase);
    setAlive(info.id, !!info.alive);
    setSessionPhase(info.id, phase);
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
      clearDismissedDead(info.id);
      const t = termsMap().get(info.id);
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
    setSessions(sessions || []);
    // A fresh page load (or a GUI reload) starts with no local edge
    // tracking at all, so the very first snapshot is where this window
    // learns what was already waiting — silently, like the old
    // syncAttentionFromSessions: a session that rang before this window
    // existed should be visible, not announced.
    for (const s of appData().sessions) {
      syncAttentionClass(s, !sawFirstSessionList);
      processAliveTransition(s);
      termsMap().get(s.id)?.setPhase(phaseOf(s));
    }
    sawFirstSessionList = true;
    // Drop any ids whose sessions no longer exist (e.g. after a daemon
    // restart or list reset) so the tray doesn't leak stale chips and
    // the transition-detection maps don't grow for the life of the
    // process. A snapshot is the only path that can retire a session
    // without a per-session `removed` event.
    pruneToLiveSessions();
    const liveIds = new Set(appData().sessions.map((s) => s.id));
    // attentionEdge lives here rather than in the store, so
    // pruneToLiveSessions cannot reach it — prune it alongside. A left
    // -over id suppresses the next false→true notification for a
    // recycled id, which is the bug the edge tracking exists to avoid.
    for (const id of attentionEdge)
      if (!liveIds.has(id)) attentionEdge.delete(id);
    pruneNav(appData().nav, (id) => liveIds.has(id));
    if (!appData().activeId && appData().sessions.length > 0) {
      deps.switchTo(orderedSessions()[0].id);
    }
  });

  // What a restore could NOT bring back. Its own event rather than a
  // field on the session, because it describes the transition, not the
  // session — a tile that has been open for an hour should not still
  // be advertising that its worktree was rebuilt.
  EventsOn('session:restored', (jsonStr: string) => {
    try {
      onSessionRestored(JSON.parse(jsonStr));
    } catch {
      // A malformed payload costs the user the "what was lost" line,
      // not the restored session — the tile already arrived on the
      // ordinary session event stream.
    }
  });

  EventsOn('session:event', (jsonStr: string) => {
    const ev = JSON.parse(jsonStr) as SessionEvent;
    const i = appData().sessions.findIndex((s) => s.id === ev.session.id);
    if (ev.kind === 'added' || ev.kind === 'updated') {
      processAliveTransition(ev.session);
    }
    if (ev.kind === 'added') {
      addSession(ev.session);
      deps.switchTo(ev.session.id);
      return;
    }
    // The program on the PTY re-titled itself. Its own kind, not an
    // `updated`, so the store write stays narrow: only the retitled
    // session's object is replaced. components/Sidebar.tsx's memoized
    // SessionItem turns that into a single re-rendered row. (The
    // imperative sidebar needed a whole second patch path here — a
    // rebuild at the child process's redraw rate ate dblclick pairs.)
    if (ev.kind === 'title') {
      if (i >= 0) updateSession(ev.session);
      return;
    }
    // The session started or stopped working, or an agent reported what
    // it is blocked on. Its own kind for the same reason as 'title':
    // this fires at the rate an agent changes what it is doing, and
    // riding 'updated' would make every consumer re-render at that
    // rate. The narrow store write repaints one sidebar row.
    if (ev.kind === 'state') {
      if (i >= 0) updateSession(ev.session);
      // needs_attention is derived from state, so a state change can
      // flip it too (a future hook-tier turn_end, say) — keep the pulse
      // and the notification edge in sync here as well, not only on the
      // `attention` kind below.
      syncAttentionClass(ev.session);
      return;
    }
    // The daemon's bell scanner fired, or another client reported that
    // the user looked. Its own kind, like 'title', so a bell never
    // triggers the full re-render that 'updated' means.
    if (ev.kind === 'attention') {
      if (i >= 0) updateSession(ev.session);
      syncAttentionClass(ev.session);
      return;
    }
    if (ev.kind === 'updated' && phaseOf(ev.session) === PHASE.closing) {
      // Don't make the user watch a teardown: the moment the daemon
      // starts closing, hand focus to the neighbour. The tile itself
      // stays (dimmed) until `removed` lands, which can be seconds
      // later on a big worktree.
      //
      // `closing` only, not isClosing() — that also covers `checking`,
      // the daemon's pre-flight `git status` on the worktree, which can
      // still end in a refusal and the "Close this session anyway?"
      // dialog. Switching then puts that dialog over a different
      // session than the one it is about, and a cancel leaves the user
      // parked on the neighbour.
      if (appData().activeId === ev.session.id) {
        const next = neighbourOf(ev.session.id);
        if (next) deps.switchTo(next);
      }
    }
    if (ev.kind === 'removed') {
      // Offers undo, but only for a close this client issued.
      onSessionRemoved(ev.session.id);
      forgetSession(ev.session.id);
      const nextId =
        appData().activeId === ev.session.id
          ? neighbourOf(ev.session.id)
          : null;
      if (i >= 0) removeSession(ev.session.id);
      const t = termsMap().get(ev.session.id);
      if (t) {
        t.destroy();
        deleteTerm(ev.session.id);
      }
      // Prune AFTER the splice above: until then the removed id is
      // still in appData().sessions, so an exists-check would keep it.
      pruneNav(appData().nav, (id) =>
        appData().sessions.some((s) => s.id === id),
      );
      if (appData().activeId === ev.session.id) {
        setActiveId(null);
        if (nextId) deps.switchTo(nextId);
      }
      // Killing the second-to-last tile leaves a one-tile grid, which is
      // the degenerate state setView refuses to enter.
      deps.enforceViewFloor();
      // No repaint call: removeSession() drops the id from the grid
      // scope, which moves GridView's signature. (The explicit
      // renderGrid() that used to stand here existed because the only
      // other repaint on this path was the switchTo above, which fires
      // solely when the *active* tile was the one killed.)
    } else if (ev.kind === 'updated') {
      // A reorder arrives as `updated` events carrying new .order
      // values. The layout pass appends tiles in gridScopeSessions
      // order, and that order is part of GridView's signature, so the
      // store write below is the repaint — a rename, which changes the
      // sessions array but not the order, still costs nothing.
      if (i >= 0) updateSession(ev.session);
      // Push the new name/color/worktree branch into the cached
      // SessionTerm so the grid tile-header refreshes immediately.
      // Without this, renames look broken in grid mode — the sidebar
      // updates but the tile keeps showing the old name.
      const st = termsMap().get(ev.session.id);
      if (st) {
        st.setInfo(ev.session);
        // Phase drives the loading panel and the attach gate. Set it
        // after setInfo so the panel's labels (branch, agent) come
        // from the fresh info.
        st.setPhase(phaseOf(ev.session));
        const pid = ev.session.projectId ?? ev.session.project_id;
        const proj = appData().projects.find((p) => p.id === pid);
        st.setProject(proj?.name ?? '', proj?.color ?? '');
        // Restart Session path: pty:disconnect already flipped attached
        // off and set needsReattach. Now that the daemon has confirmed
        // a fresh alive=true PTY, reattach the visible term so its
        // resumed stream starts flowing without a manual switch.
        // Hidden terms are left dirty; switchTo and the next layout
        // pass will ensureAttached when they next become visible.
        if (st.needsReattach && ev.session.alive) {
          st.needsReattach = false;
          try {
            st.term?.reset();
          } catch {}
          abandonReplays(st); // the wipe abandons any in-flight restream
          const visible =
            (appData().view === 'single' &&
              appData().activeId === ev.session.id) ||
            (appData().view !== 'single' &&
              st.host.classList.contains('in-grid'));
          if (visible) {
            st.ensureAttached();
            if (appData().activeId === ev.session.id) deps.focusActiveTerm();
          }
        }
      }
      if (appData().activeId === ev.session.id) deps.updateAppTitle();
    }
    // Only `removed` and `updated` reach here — `added` and `title` both
    // returned above. Both are store writes; the sidebar re-renders the
    // rows whose SessionInfo reference actually changed, so `updated`
    // (the high-frequency kind: one per phase step, one per surviving
    // session when a kill recompacts the order, one when the
    // agent-session-id capture poll lands up to 30s after a spawn) costs
    // a row, not a rebuild.
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
    termsMap().get(id)?.writeData(b64);
  });

  EventsOn('pty:event', (id: string, jsonStr: string) => {
    try {
      const ev = JSON.parse(jsonStr) as { kind: string };
      const st = termsMap().get(id);
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
    const st = termsMap().get(id);
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
    const st = termsMap().get(id);
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
    const info = appData().sessions.find((s) => s.id === sessionId);
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
      const sess = appData().sessions.find((s) => s.id === e.session_id);
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
      if (!appData().sessions.find((s) => s.id === e.session_id)) return;
      const sessName = sess?.name ?? 'Session';
      if (answer === 'close-and-clean') {
        // Deleting the worktree is the one close that destroys work,
        // so the banner it raises says so rather than offering a plain
        // "Undo" the restore cannot honour in full.
        noteLocalClose(e.session_id, sessName, true);
        // One daemon-side operation: the worktree is occupied until
        // this session is gone, so closing and then asking to remove it
        // would race its own teardown and be refused as in-use.
        KillSessionAndWorktree(e.session_id).catch(
          reportFailure('close and delete worktree'),
        );
        return;
      }
      noteLocalClose(e.session_id, sessName);
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
