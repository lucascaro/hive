// ---------- focus pipeline ----------
//
// Moved verbatim from main.js. ensureTerm is injected (session-term
// imports this module, so the reverse edge must be a dep).

import { state } from './state.js';
import {
  decideFocusAction, ACTION_CLEAR, ACTION_PRESERVE, ACTION_FOCUS,
} from '../lib/focus.js';
import { anyModalOpen } from './modals/registry.js';

let deps = {
  ensureTerm: () => {},
};

export function initFocus(injected) {
  deps = injected;
}

// setActive centralizes "the focused session changed" so every code
// path (click, arrow nav, project switch, switchTo) clears the bell
// indicator the same way and syncs the current project to whatever
// project the new session belongs to.
export function setActive(id) {
  if (id) {
    state.attention.delete(id);
    state.terms.get(id)?.host.classList.remove('attention');
    const s = state.sessions.find((x) => x.id === id);
    const pid = s?.projectId ?? s?.project_id;
    if (pid) state.currentProjectId = pid;
  }
  state.activeId = id;
  // Schedule focus after the next paint so any DOM reorder / visibility
  // change from renderGrid / showSingle has settled. xterm.focus()
  // moves focus to its hidden textarea; that fires .onFocus, which
  // adds .term-focused to the host (the source of truth for the
  // visual focus border).
  if (id) focusActiveTerm();
}

// setFocusedTile is the SOLE writer of .term-focused. It reconciles
// visual focus and keyboard focus atomically — in the same rAF tick —
// so they can never drift. Visual focus is a pure projection of
// state.activeId gated by whether a modal/rename owns the keyboard.
//
// Pass state.activeId to focus the active session. Pass null to drop
// the visual focus everywhere (e.g. when opening the launcher or a
// rename input). Every state transition that could change which tile
// should be focused (setActive, setView, renderGrid, modal open/close,
// rename open/close, dialog close, OS fullscreen toggle, …) MUST end
// by calling setFocusedTile(...).
//
// Previously visual focus was event-driven from focusin/focusout on
// each .term-host, and keyboard focus was driven separately by
// focusActiveTerm()'s ta.focus() call. During view transitions (most
// visibly single → grid: renderGrid's appendChild reorder and the
// helper-textarea mounted by xterm.open() for newly-materialized
// tiles), the two could end up on different tiles. The user would
// see a session lit up while keystrokes went nowhere. Single writer
// makes that impossible: the class is added only here, in the same
// rAF as the helper-textarea focus.
// Transient focus guard for the post-view-switch settle window.
//
// Switching to grid reparents the active tile (renderGrid's appendChild
// reorder) and triggers async ResizeObserver → fit → WebGL resize on the
// newly-visible neighbour tiles. Both momentarily blur the active
// helper-textarea to <body>. A keystroke typed in that sub-frame gap lands
// on <body> and is silently lost — observed as "ello"/"o" instead of
// "hello" right after ⌘⇧G (a real keystroke-loss bug, and the cause of the
// flaky focus E2E). applyFocus's rAF retry re-focuses, but only on the NEXT
// frame, too late for a char already dropped.
//
// This document-level capture guard re-focuses SYNCHRONOUSLY the instant the
// guarded textarea blurs to <body>, so focus is back before the next
// keystroke's event-loop turn. It is armed only for a short window after a
// real-tile focus request and only acts while that tile is still active and
// no modal/rename legitimately owns the keyboard — so it never traps focus.
let _focusGuard = null; // { id, until } | null

function armFocusGuard(id) {
  _focusGuard = { id, until: performance.now() + 500 };
}

document.addEventListener(
  'focusout',
  (e) => {
    const g = _focusGuard;
    if (!g) return;
    if (performance.now() > g.until) { _focusGuard = null; return; }
    if (state.activeId !== g.id) return; // active tile changed → let it go
    if (focusSnapshot(g.id).modalOpen) return; // modal/rename owns keyboard
    const st = state.terms.get(g.id);
    if (!st) return;
    const ta = st.host.querySelector('.xterm-helper-textarea');
    if (!ta || e.target !== ta) return; // only when OUR textarea blurs
    // Only reclaim a transient blur to nothing/<body>; never override the
    // user intentionally focusing another control.
    const dest = e.relatedTarget;
    if (dest && dest !== document.body) return;
    ta.focus();
  },
  true,
);

export function setFocusedTile(id) {
  // First decision: synchronous, before any rAF. If we already know we
  // should clear, do it immediately so a modal/null transition can't be
  // overtaken by a stale in-flight focus rAF.
  const snap = focusSnapshot(id);
  if (snap.modalOpen || id == null || !state.terms.get(id)) {
    _focusGuard = null;
    sweepFocusBorder();
    return;
  }
  // Arm the synchronous blur guard for the settle window, then schedule
  // the focus drive after the next paint so any in-flight DOM transition
  // (showSingle / renderGrid / appendChild / xterm.open) settles before we
  // read activeElement and move focus.
  armFocusGuard(id);
  requestAnimationFrame(() => applyFocus(id, /*attempt=*/0));
}

function applyFocus(id, attempt) {
  const st = state.terms.get(id);
  if (!st) { sweepFocusBorder(); return; }
  const action = decideFocusAction(focusSnapshot(id));
  if (action.kind === ACTION_CLEAR || action.kind === ACTION_PRESERVE) {
    sweepFocusBorder();
    return;
  }
  // Atomic reconcile: sweep + add + focus.
  for (const el of document.querySelectorAll('.term-host.term-focused')) {
    if (el !== st.host) el.classList.remove('term-focused');
  }
  st.host.classList.add('term-focused');
  // Drive browser focus to the DOM helper-textarea. xterm's
  // term.focus() early-returns on a stale-true _focused flag (#159);
  // after this transition the flag is stale-false because the
  // synchronous display:none flip during renderGrid's parent class
  // swap (single → grid) fires focusout. ta.focus() drives the real
  // event; the follow-up term.focus() resyncs xterm's internal state.
  const ta = st.host.querySelector('.xterm-helper-textarea');
  // Only drive focus when it has actually drifted off the target
  // textarea. Re-focusing an already-focused xterm helper-textarea is
  // NOT a harmless no-op: it clears the textarea's pending input mid-
  // keystroke, so a character typed during the post-grid-switch retry
  // window is dropped before xterm's input event emits it (observed as
  // "ello" / "o" instead of "hello"). Because several setFocusedTile
  // calls fire during a grid switch, multiple retry chains overlap and
  // hammer focus() every frame for ~300ms; guarding on real drift keeps
  // the #159/#181/#186 drift-correction while ending the keystroke loss.
  if (ta && document.activeElement !== ta) {
    ta.focus();
    if (typeof st.term?.focus === 'function') st.term.focus();
  }
  // Schedule a verification rAF *next frame* (not this one — focus()
  // just fired and synchronously updated activeElement, so an in-tick
  // check would trivially pass and miss the real failure mode):
  // post-renderGrid side-effects (ResizeObserver → fit → WebGL canvas
  // resize on newly-visible neighbour tiles) can synchronously fire
  // focusout ~10ms later. If activeElement has drifted off `ta` by
  // then, re-focus. Cap retries so a genuine modal-takeover or rename
  // doesn't busy-loop.
  // Poll for several frames. A single rAF check is insufficient
  // because post-renderGrid side-effects (ResizeObserver → fit →
  // WebGL canvas resize on neighbour tiles) can fire focusout AFTER
  // the rAF batch completes — the disturbance arrives one event-loop
  // turn later than the verify. We watch for FOCUS_MAX_RETRIES frames
  // and re-focus whenever activeElement drifts off `ta`. Polling is
  // bounded and idempotent (re-focusing an already-focused element is
  // a no-op).
  const FOCUS_MAX_RETRIES = 8;
  if (ta && attempt < FOCUS_MAX_RETRIES) {
    requestAnimationFrame(() => {
      const verifyAction = decideFocusAction(focusSnapshot(id));
      if (verifyAction.kind !== ACTION_FOCUS) return; // a modal / rename took over
      applyFocus(id, attempt + 1);
    });
  }
  // Optional dev-mode assertion: two rAFs later, the visual focus
  // and the keyboard focus should agree. Console-warn on drift so
  // future variants of #159/#181/#186 are caught in QA.
  if (debugFocusEnabled() && attempt === 0) scheduleFocusConsistencyCheck(id);
}

function sweepFocusBorder() {
  for (const el of document.querySelectorAll('.term-host.term-focused')) {
    el.classList.remove('term-focused');
  }
}

function focusSnapshot(id) {
  const ae = document.activeElement;
  return {
    id,
    modalOpen: anyModalOpen(),
    activeTag: ae ? ae.tagName : '',
    activeClasses: ae ? ae.classList : '',
    knownTermIds: state.terms,
  };
}

function debugFocusEnabled() {
  try { return localStorage.getItem('hive.debug') === '1'; } catch { return false; }
}

function scheduleFocusConsistencyCheck(id) {
  requestAnimationFrame(() => requestAnimationFrame(() => {
    const st = state.terms.get(id);
    if (!st) return;
    const ta = st.host.querySelector('.xterm-helper-textarea');
    const ae = document.activeElement;
    const focusedHost = ae ? ae.closest('.term-host') : null;
    if (focusedHost !== st.host || ae !== ta) {
      // eslint-disable-next-line no-console
      console.warn('[focus] inconsistent state', {
        view: state.view,
        activeId: state.activeId,
        wantId: id,
        aeTag: ae ? ae.tagName : null,
        aeClass: ae ? ae.className : null,
        focusedHostMatches: focusedHost === st.host,
      });
    }
  }));
}

// focusActiveTerm / refocusActiveTerm are thin wrappers retained so
// every existing callsite (and any third-party readers of the code)
// keeps working. Both reduce to setFocusedTile(state.activeId): the
// gate inside setFocusedTile decides whether to apply or clear.
export function focusActiveTerm() {
  setFocusedTile(state.activeId);
}

export function refocusActiveTerm() {
  setFocusedTile(state.activeId);
}
