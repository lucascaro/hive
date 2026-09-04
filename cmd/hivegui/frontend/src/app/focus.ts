// ---------- focus pipeline ----------
//
// Moved from the original composition root. This module stays independent
// of session-term because session-term imports the focus pipeline.

import { appStore } from '../store/store.js';
import { termsMap } from '../store/terms.js';
import {
  clearAttentionFor,
  setActiveId,
  setCurrentProjectId,
} from '../store/store.js';
import {
  decideFocusAction,
  ACTION_CLEAR,
  ACTION_PRESERVE,
  ACTION_FOCUS,
  type FocusSnapshot,
} from '../lib/focus.js';
import { anyModalOpen } from '../store/store.js';
import { pushNav } from '../lib/nav-history.js';
import { clearAttention } from './events.js';
import { scrollTrace } from './trace.js';

// Live read of the store. A function, not a destructured snapshot: this
// module runs inside event handlers and must never cache a slice across
// a store write.
const appData = () => appStore.getState();

// setActive centralizes "the focused session changed" so every code
// path (click, arrow nav, project switch, switchTo) clears the bell
// indicator the same way and syncs the current project to whatever
// project the new session belongs to.
// withoutNavHistory runs fn with history recording suppressed, so the
// Ctrl+- / Ctrl+Shift+- handlers can replay the stack without the
// replay itself pushing new entries (which would ping-pong between two
// sessions forever). try/finally so a throw inside fn can't leave the
// flag stuck on and silently stop recording for the rest of the session.
let _navSuppress = false;

export function withoutNavHistory<T>(fn: () => T): T {
  _navSuppress = true;
  try {
    return fn();
  } finally {
    _navSuppress = false;
  }
}

export function setActive(id: string | null) {
  // Record the DEPARTURE before activeId is overwritten. This lives in
  // setActive rather than switchTo because four selection paths reach
  // setActive directly and would otherwise go unrecorded: tile mousedown
  // (session-term.ts), gridSpatialMove / shiftActiveProject /
  // minimizeSession (view.ts). "No matter how you switched" is the
  // feature, so the hook has to sit on the choke point.
  if (!_navSuppress && id && id !== appData().activeId)
    pushNav(appData().nav, appData().activeId);
  const switched = id !== null && id !== appData().activeId;
  if (id) {
    // Only on an actual switch, and clearAttention rather than
    // clearAttentionFor.
    //
    // The kind, because dropping it locally and saying nothing left the
    // daemon still insisting the session wanted you, and the next
    // session list put the flag straight back.
    //
    // The guard, because setActive is a choke point that many paths
    // re-enter for the session that is ALREADY active — a grid move, a
    // project switch, a re-render. Without it every one of those told
    // the daemon "the user just looked", so a bell on the session you
    // are sitting in was wiped before it could be seen. Arriving at a
    // session is the signal; being parked in one is not. Typing is what
    // answers a bell you are already looking at — see noteUserInput.
    if (switched) clearAttention(id);
    termsMap().get(id)?.host.classList.remove('attention');
    const s = appData().sessions.find((x) => x.id === id);
    const pid = s?.projectId ?? s?.project_id;
    if (pid) setCurrentProjectId(pid);
  }
  setActiveId(id);
  // Schedule focus after the next paint so any DOM reorder / visibility
  // change from applyGridLayout / applySingle has settled. xterm.focus()
  // moves focus to its hidden textarea; that fires .onFocus, which
  // adds .term-focused to the host (the source of truth for the
  // visual focus border).
  if (id) focusActiveTerm();
}

// setFocusedTile is the SOLE writer of .term-focused. It reconciles
// visual focus and keyboard focus atomically — in the same rAF tick —
// so they can never drift. Visual focus is a pure projection of
// appData().activeId gated by whether a modal/rename owns the keyboard.
//
// Pass appData().activeId to focus the active session. Pass null to drop
// the visual focus everywhere (e.g. when opening the launcher or a
// rename input). Every state transition that could change which tile
// should be focused (setActive, setView, the grid layout pass, modal open/close,
// rename open/close, dialog close, OS fullscreen toggle, …) MUST end
// by calling setFocusedTile(...).
//
// Previously visual focus was event-driven from focusin/focusout on
// each .term-host, and keyboard focus was driven separately by
// focusActiveTerm()'s ta.focus() call. During view transitions (most
// visibly single → grid: applyGridLayout's appendChild reorder and the
// helper-textarea mounted by xterm.open() for newly-materialized
// tiles), the two could end up on different tiles. The user would
// see a session lit up while keystrokes went nowhere. Single writer
// makes that impossible: the class is added only here, in the same
// rAF as the helper-textarea focus.
// Transient focus guard for the post-view-switch settle window.
//
// Switching to grid reparents the active tile (applyGridLayout's appendChild
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
let _focusGuard: { id: string; until: number } | null = null;

function armFocusGuard(id: string) {
  _focusGuard = { id, until: performance.now() + 500 };
}

// Focusing xterm's helper textarea makes the browser scroll the nearest
// scrollable ancestor to reveal it. That ancestor is `.term-body`, which
// is `overflow: hidden` and — because the helper textarea sits below the
// visible rows — has roughly a viewport's worth of scroll slack. The
// browser therefore scrolls the terminal completely out of its own box
// and the tile renders solid black, until any resize re-lays-out and
// clamps scrollTop back to 0. That is the whole of the "black tile until
// I resize" bug: the content was always painted, just scrolled out of
// sight.
// (xterm's own Terminal.focus() already passes preventScroll, so these
// two call sites were the only ones that could trigger it.)
const FOCUS_OPTS: FocusOptions = { preventScroll: true };

document.addEventListener(
  'focusout',
  (e) => {
    const g = _focusGuard;
    if (!g) return;
    if (performance.now() > g.until) {
      _focusGuard = null;
      return;
    }
    if (appData().activeId !== g.id) return; // active tile changed → let it go
    if (focusSnapshot(g.id).modalOpen) return; // modal/rename owns keyboard
    const st = termsMap().get(g.id);
    if (!st) return;
    const ta = st.host.querySelector<HTMLTextAreaElement>(
      '.xterm-helper-textarea',
    );
    if (!ta || e.target !== ta) return; // only when OUR textarea blurs
    // Only reclaim a transient blur to nothing/<body>; never override the
    // user intentionally focusing another control.
    const dest = e.relatedTarget;
    if (dest && dest !== document.body) return;
    ta.focus(FOCUS_OPTS);
  },
  true,
);

export function setFocusedTile(id: string | null) {
  // First decision: synchronous, before any rAF. If we already know we
  // should clear, do it immediately so a modal/null transition can't be
  // overtaken by a stale in-flight focus rAF.
  const snap = focusSnapshot(id);
  if (snap.modalOpen || id == null || !termsMap().get(id)) {
    _focusGuard = null;
    sweepFocusBorder();
    return;
  }
  // Arm the synchronous blur guard for the settle window, then schedule
  // the focus drive after the next paint so any in-flight DOM transition
  // (applySingle / applyGridLayout / appendChild / xterm.open) settles before we
  // read activeElement and move focus.
  armFocusGuard(id);
  requestAnimationFrame(() => applyFocus(id, /*attempt=*/ 0));
}

function applyFocus(id: string, attempt: number) {
  const st = termsMap().get(id);
  if (!st) {
    sweepFocusBorder();
    return;
  }
  // Freeze probe: count + record every focus-drive attempt. Several
  // setFocusedTile() calls fire during a grid switch and each arms an
  // 8-frame rAF retry chain; in grid mode with many tiles those chains
  // overlap and can hammer focus() every frame. A focusApply count that
  // dwarfs the layout-pass count — or focus-apply events that never stop
  // — would mark the focus reconciliation loop as the storm source.
  if (scrollTrace.rec.enabled) {
    scrollTrace.count('focusApply');
    const ae = document.activeElement;
    scrollTrace.rec('focus-apply', {
      id,
      attempt,
      view: appData().view,
      ae: ae ? `${ae.tagName}.${ae.className || ''}`.trim() : 'none',
    });
  }
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
  // synchronous display:none flip during the layout pass's parent class
  // swap (single → grid) fires focusout. ta.focus() drives the real
  // event; the follow-up term.focus() resyncs xterm's internal state.
  const ta = st.host.querySelector<HTMLTextAreaElement>(
    '.xterm-helper-textarea',
  );
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
    ta.focus(FOCUS_OPTS);
    if (typeof st.term?.focus === 'function') st.term.focus();
  }
  // Schedule a verification rAF *next frame* (not this one — focus()
  // just fired and synchronously updated activeElement, so an in-tick
  // check would trivially pass and miss the real failure mode):
  // post-layout-pass side-effects (ResizeObserver → fit → WebGL canvas
  // resize on newly-visible neighbour tiles) can synchronously fire
  // focusout ~10ms later. If activeElement has drifted off `ta` by
  // then, re-focus. Cap retries so a genuine modal-takeover or rename
  // doesn't busy-loop.
  // Poll for several frames. A single rAF check is insufficient
  // because post-layout-pass side-effects (ResizeObserver → fit →
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

function focusSnapshot(id: string | null): FocusSnapshot {
  const ae = document.activeElement;
  return {
    id,
    modalOpen: anyModalOpen(),
    activeTag: ae ? ae.tagName : '',
    activeClasses: ae ? ae.classList : '',
    knownTermIds: termsMap(),
  };
}

function debugFocusEnabled() {
  try {
    return localStorage.getItem('hive.debug') === '1';
  } catch {
    return false;
  }
}

function scheduleFocusConsistencyCheck(id: string) {
  requestAnimationFrame(() =>
    requestAnimationFrame(() => {
      const st = termsMap().get(id);
      if (!st) return;
      const ta = st.host.querySelector<HTMLTextAreaElement>(
        '.xterm-helper-textarea',
      );
      const ae = document.activeElement;
      const focusedHost = ae ? ae.closest('.term-host') : null;
      if (focusedHost !== st.host || ae !== ta) {
        // eslint-disable-next-line no-console
        console.warn('[focus] inconsistent state', {
          view: appData().view,
          activeId: appData().activeId,
          wantId: id,
          aeTag: ae ? ae.tagName : null,
          aeClass: ae ? ae.className : null,
          focusedHostMatches: focusedHost === st.host,
        });
      }
    }),
  );
}

// focusActiveTerm / refocusActiveTerm are thin wrappers retained so
// every existing callsite (and any third-party readers of the code)
// keeps working. Both reduce to setFocusedTile(appData().activeId): the
// gate inside setFocusedTile decides whether to apply or clear.
export function focusActiveTerm() {
  setFocusedTile(appData().activeId);
}

export function refocusActiveTerm() {
  setFocusedTile(appData().activeId);
}
