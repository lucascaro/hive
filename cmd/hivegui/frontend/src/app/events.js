// ---------- bell + attention + daemon events ----------
//
// Moved verbatim from main.js. wireDaemonEvents(deps) registers every
// EventsOn handler; view/focus callbacks and the scroll tracer are
// injected because they live in main.js until later stages.

import { EventsOn, Notify, Confirm, KillSession } from '../bridge.js';
import { state, saveCollapsed } from './state.js';
import { setStatus, flashStatus, reportFailure } from './dom.js';
import { orderedSessions } from './selectors.js';
import { renderSidebar, updateSidebarSelection } from './sidebar.js';
import { pruneCollapsed } from '../lib/collapsed.js';
import { handleScrollbackEvent } from '../lib/scrollback.js';

let deps = {
  switchTo: () => {},
  renderMinimizedTray: () => {},
  updateAppTitle: () => {},
  focusActiveTerm: () => {},
  refocusActiveTerm: () => {},
  isDaemonRestarting: () => false,
  scrollTrace: { rec: Object.assign(() => {}, { enabled: false }) },
};


// onSessionBell is fired by SessionTerm whenever its xterm receives
// BEL. Active + window-focused session: ignore. Otherwise: mark
// attention, repaint sidebar, and fire a desktop notification — but
// only on the transition from no-attention → attention, so a session
// emitting bells in a tight loop doesn't spam the OS notification
// center.
export function onSessionBell(info) {
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
  updateSidebarSelection();
  if (!alreadyAttention) fireBellNotification(info);
}

export function clearAttention(sessionId) {
  if (state.attention.delete(sessionId)) {
    state.terms.get(sessionId)?.host.classList.remove('attention');
    updateSidebarSelection();
  }
}

// fireBellNotification routes through Go because Wails' WKWebView on
// macOS doesn't implement the HTML5 Notification API. The Go side
// dispatches per-platform (NSUserNotification / notify-send / Windows
// toast). The session id is passed as the tag so the OS can dedupe
// repeated bells from the same session and the click handler knows
// which session to switch to.
function fireBellNotification(info) {
  const proj = state.projects.find((p) => p.id === (info.projectId ?? info.project_id));
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
function onSessionDeath(info) {
  state.dismissedDead.delete(info.id);
  const t = state.terms.get(info.id);
  if (t) {
    // Flip attached eagerly so a switch-back before pty:disconnect arrives
    // doesn't try to reuse the dying connection.
    t.attached = false;
    t.setDead(true, info.last_error || 'The process running in this session has exited.');
  }
  // Reuse the attention pulse path so the sidebar entry highlights.
  state.attention.add(info.id);
  state.terms.get(info.id)?.host.classList.add('attention');
  updateSidebarSelection();
  const proj = state.projects.find((p) => p.id === (info.projectId ?? info.project_id));
  // Best-effort like fireBellNotification: the overlay + sidebar pulse
  // already cover the user if the OS notification fails.
  Notify(info.name || 'Session', proj?.name ?? '', 'Session ended.', info.id).catch(() => {});
}

export function wireDaemonEvents(injected) {
  deps = injected;

  // Whenever the window regains focus, clear the active session's
  // attention state — the user is presumably looking at it. Also
  // restore xterm focus: macOS fullscreen toggles, ⌘-tab returns, and
  // menu actions can leave the window focused but no element inside it,
  // so typing would land on the body and be lost.
  window.addEventListener('focus', () => {
    if (state.activeId) clearAttention(state.activeId);
    redeps.focusActiveTerm();
  });


  EventsOn('project:list', (jsonStr) => {
    const { projects } = JSON.parse(jsonStr);
    state.projects = projects || [];
    if (!state.currentProjectId && state.projects[0]) {
      state.currentProjectId = state.projects[0].id;
    }
    // Drop persisted collapse entries for projects that no longer exist
    // so the localStorage key can't grow forever.
    const pruned = pruneCollapsed(state.collapsed, state.projects.map((p) => p.id));
    if (pruned.changed) {
      state.collapsed = pruned.set;
      saveCollapsed();
    }
    renderSidebar();
  });

  EventsOn('project:event', (jsonStr) => {
    const ev = JSON.parse(jsonStr);
    const i = state.projects.findIndex((p) => p.id === ev.project.id);
    if (ev.kind === 'added') {
      if (i < 0) state.projects.push(ev.project);
      // First-ever project: make it current.
      if (!state.currentProjectId) state.currentProjectId = ev.project.id;
    } else if (ev.kind === 'removed') {
      if (i >= 0) state.projects.splice(i, 1);
      if (state.collapsed.delete(ev.project.id)) saveCollapsed();
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
  function processAliveTransition(info) {
    const prev = state.aliveById.get(info.id);
    state.aliveById.set(info.id, !!info.alive);
    if (prev === true && info.alive === false) {
      onSessionDeath(info);
    } else if (prev === undefined && info.alive === false) {
      // Session was born dead (e.g. agent binary not found).
      onSessionDeath(info);
    } else if (prev === false && info.alive === true) {
      state.dismissedDead.delete(info.id);
      const t = state.terms.get(info.id);
      if (t) {
        // Wipe stale frame from the previous (dead) shell so the revived
        // session's prompt lands on a clean screen instead of stacking on
        // the old cursor position.
        try { t.term.reset(); } catch {}
        t.attached = false;
        t.setDead(false);
      }
    }
  }

  EventsOn('session:list', (jsonStr) => {
    const { sessions } = JSON.parse(jsonStr);
    state.sessions = sessions || [];
    for (const s of state.sessions) processAliveTransition(s);
    // Drop any minimized ids whose sessions no longer exist (e.g. after
    // a daemon restart or list reset) so the tray doesn't leak stale chips.
    const liveIds = new Set(state.sessions.map((s) => s.id));
    for (const id of Array.from(state.minimized)) {
      if (!liveIds.has(id)) state.minimized.delete(id);
    }
    renderSidebar();
    deps.renderMinimizedTray();
    if (!state.activeId && state.sessions.length > 0) {
      deps.switchTo(orderedSessions()[0].id);
    }
  });

  EventsOn('session:event', (jsonStr) => {
    const ev = JSON.parse(jsonStr);
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
    if (ev.kind === 'removed') {
      state.aliveById.delete(ev.session.id);
      state.dismissedDead.delete(ev.session.id);
      state.minimized.delete(ev.session.id);
      let nextId = null;
      if (state.activeId === ev.session.id) {
        const ord = orderedSessions();
        const idx = ord.findIndex((s) => s.id === ev.session.id);
        const nb = idx > 0 ? ord[idx - 1] : ord[idx + 1];
        nextId = nb?.id ?? null;
      }
      if (i >= 0) state.sessions.splice(i, 1);
      const t = state.terms.get(ev.session.id);
      if (t) {
        t.destroy();
        state.terms.delete(ev.session.id);
      }
      if (state.activeId === ev.session.id) {
        state.activeId = null;
        if (nextId) deps.switchTo(nextId);
      }
    } else if (ev.kind === 'updated') {
      if (i >= 0) state.sessions[i] = ev.session;
      // Push the new name/color/worktree branch into the cached
      // SessionTerm so the grid tile-header refreshes immediately.
      // Without this, renames look broken in grid mode — the sidebar
      // updates but the tile keeps showing the old name.
      const st = state.terms.get(ev.session.id);
      if (st) {
        st.setInfo(ev.session);
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
          try { st.term.reset(); } catch {}
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
    if (ev.kind === 'removed' || ev.kind === 'updated') deps.renderMinimizedTray();
  });

  EventsOn('pty:data', (id, b64) => {
    state.terms.get(id)?.writeData(b64);
  });

  EventsOn('pty:event', (id, jsonStr) => {
    try {
      const ev = JSON.parse(jsonStr);
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
        deps.scrollTrace.rec(ev.kind, {
          id, viewportY: buf?.viewportY, baseY: buf?.baseY,
          wants: st._replayWantsBottom,
        });
      }
      handleScrollbackEvent(st, ev.kind);
    } catch { /* ignore */ }
  });

  EventsOn('pty:disconnect', (id) => {
    const st = state.terms.get(id);
    if (st) {
      st.attached = false;
      // Mark the term as needing reattach. Restart Session closes the
      // daemon-side PTY (which lands here) and respawns; the subsequent
      // session:event(updated, alive=true) is where we re-OpenSession.
      st.needsReattach = true;
    }
  });

  EventsOn('pty:error', (id, jsonStr) => {
    const st = state.terms.get(id);
    if (st) {
      try {
        const e = JSON.parse(jsonStr);
        st.term.write(`\r\n\x1b[31m[hived: ${e.code}: ${e.message}]\x1b[0m\r\n`);
      } catch {}
    }
  });

  EventsOn('control:disconnect', () => {
    // During a user-initiated RestartDaemon we knowingly close the
    // control conn; the banner already says "Restarting hived…". Don't
    // also flash an alarming red status line in that window.
    if (deps.isDaemonRestarting()) return;
    setStatus('control disconnected', true);
  });

  // User clicked a notification toast. Route to that session in the
  // current view (single keeps single, grid keeps grid) without toggling
  // modes. switchTo handles the view-aware repaint.
  EventsOn('bell-click', (sessionId) => {
    if (!sessionId) return;
    const info = state.sessions.find((s) => s.id === sessionId);
    if (!info) return;
    deps.switchTo(sessionId);
    clearAttention(sessionId);
  });

  EventsOn('control:error', async (jsonStr) => {
    let e;
    try { e = JSON.parse(jsonStr); } catch { flashStatus('hived error', true); return; }
    // Worktree-dirty kill: confirm with the user. The daemon already
    // refused to kill, so we can safely retry with force=true if the
    // user accepts.
    if (e.code === 'worktree_dirty' && e.session_id) {
      const sess = state.sessions.find((s) => s.id === e.session_id);
      const branch = sess?.worktreeBranch ?? sess?.worktree_branch ?? 'this worktree';
      const ok = await Confirm(
        'Discard uncommitted changes?',
        `${sess?.name ?? 'Session'} has uncommitted changes in ${branch}.\n\n` +
        `Discard them and remove the worktree?`,
      );
      if (!ok) return;
      // Confirm() is async + modal; the session may have been removed
      // (or its worktree resolved) while the dialog was open. Re-check
      // before issuing a second kill that would just produce a confusing
      // "no_such_session" control error.
      if (!state.sessions.find((s) => s.id === e.session_id)) return;
      KillSession(e.session_id, true).catch(reportFailure('force kill'));
      return;
    }
    flashStatus(`${e.code}: ${e.message}`, true);
    console.warn('hived control error:', e);
  });

}
