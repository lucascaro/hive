// ---------- keyboard, menu actions, app commands ----------
//
// Moved verbatim from main.js. Font helpers and focusActiveTerm are
// injected via initKeyboard(deps) (they move in stage 6); everything
// else is imported from the sibling modules.

import {
  EventsOn,
  KillProject,
  Confirm,
  UpdateSession,
  OpenNewWindow,
  CloseWindow,
  OpenTerminalAt,
  SetClipboardText,
  Notify,
} from '../bridge.js';
import { closeActiveSession, reopenLastClosedSession } from './undo-close.js';
import { appStore } from '../store/store.js';
import { termsMap } from '../store/terms.js';
import {
  addAttentionRestored,
  addAttentionRestoredProject,
  clearAttentionRestored,
  isModalOpen,
  setAttentionReturnId,
} from '../store/store.js';
import { flashStatus, reportFailure } from './dom.js';
import {
  orderedSessions,
  activeCwd,
  activeProjectId,
  nextAttentionId,
} from './selectors.js';
import { cmdOrCtrl, isMac } from '../lib/platform.js';
import {
  openLauncher,
  duplicateActiveSession,
  duplicateActiveSessionChooseTool,
  restartActiveSession,
} from './modals/launcher.js';
import { openProjectEditor } from './modals/project-editor.js';
import {
  closeCommandPalette,
  openCommandPalette,
} from './modals/command-palette.js';
import { openSettings, closeSettings } from './modals/settings.js';
import { openWorktrees, closeWorktrees } from './modals/worktrees.js';
import {
  choiceDialogOpen,
  dismissChoiceDialog,
} from './modals/choice-dialog.js';
import { trapFocus } from '../lib/focus-trap.js';
import { inlineRenameActive, cancelInlineRename } from './inline-rename.js';
import {
  openHelpOverlay,
  closeHelpOverlay,
  toggleHelpOverlay,
} from './modals/help-overlay.js';
import { isHelpOverlayKey, navHistoryKey } from '../lib/keymap.js';
import {
  switchTo,
  setView,
  gridSpatialMove,
  shiftActiveProject,
  restoreSession,
  minimizeSession,
  minimizeProject,
  isSessionHidden,
} from './view.js';
import { manualUpdateCheck, reloadGui, restartHive } from './banners.js';
import { clearAttention } from './events.js';
import { goBack, goForward } from '../lib/nav-history.js';
import { readProjectId } from '../lib/wire.js';
import { reorderTarget } from '../lib/reorder.js';
import { scrollTrace } from './trace.js';
import { mustEl, pageEl } from './el.js';
import type { ProjectInfo } from './state.js';

// Live read of the store. A function, not a destructured snapshot: this
// module runs inside event handlers and must never cache a slice across
// a store write.
const appData = () => appStore.getState();

export interface KeyboardDeps {
  bumpFontSize: (delta: number) => void;
  resetFontSize: () => void;
  focusActiveTerm: () => void;
  withoutNavHistory: (fn: () => void) => void;
}

let deps: KeyboardDeps = {
  bumpFontSize: () => {},
  resetFontSize: () => {},
  focusActiveTerm: () => {},
  // Injected from main.tsx like focusActiveTerm above: keyboard.ts must
  // not import the focus pipeline directly (see the acyclic-modules
  // note at the wiring block in main.tsx). The default still RUNS fn —
  // an un-wired harness gets working navigation without suppression,
  // not a silently swallowed switch.
  withoutNavHistory: (fn) => fn(),
};

export function initKeyboard(injected: KeyboardDeps) {
  deps = injected;
}

window.addEventListener(
  'keydown',
  (e) => {
    // Freeze probe: record every keydown that reaches the renderer, with
    // the view and focus target at arrival. This is the discriminator for
    // the "keys do nothing in grid mode" report:
    //   • keydown events keep arriving but `ae` is BODY (not a terminal
    //     textarea) → keyboard focus was lost; the thread is fine.
    //   • NO keydown events recorded during the freeze window → the event
    //     never reached the renderer (thread blocked, or the OS/menu layer
    //     swallowed it). Cross-check against heartbeat-stall gaps.
    if (scrollTrace.rec.enabled) {
      scrollTrace.count('keydown');
      const ae = document.activeElement;
      scrollTrace.rec('keydown', {
        // e.code (physical key: 'KeyA', 'ArrowDown', 'Enter'), NOT e.key — the
        // trace is copied to the clipboard and frozen into localStorage, so
        // logging the typed character would leak passwords / tokens into a
        // pasted bug report. The physical key is all the probe needs (did the
        // event arrive, was it a nav key, where was focus).
        code: e.code,
        mods: `${e.metaKey ? 'M' : ''}${e.ctrlKey ? 'C' : ''}${e.altKey ? 'A' : ''}${e.shiftKey ? 'S' : ''}`,
        view: appData().view,
        ae: ae ? `${ae.tagName}.${ae.className || ''}`.trim() : 'none',
      });
    }
    // An inline rename owns the keyboard while it is open: Escape
    // cancels the edit, and every other key is text the user is
    // typing. Checked FIRST, and here rather than in the input's own
    // listener, because this handler runs in the capture phase — it
    // sees the key before the input does, so the input's
    // stopPropagation cannot win. Without this, Escape inside a rename
    // in the worktree browser closed the whole panel and silently
    // discarded the edit.
    if (inlineRenameActive()) {
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        cancelInlineRename();
      }
      return;
    }
    // A choice dialog is the topmost thing on screen and is asking a
    // question that may destroy work. It owns the keyboard outright:
    // Escape backs out of the question, and nothing else reaches the
    // bindings underneath while it is up. Checked before every modal:
    // its root carries a higher z-index than the dialog shell, so it
    // sits over whichever modal asked the question.
    if (choiceDialogOpen()) {
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        dismissChoiceDialog();
        return;
      }
      // Trapped from HERE, not from the dialog's own listener: that
      // one only fires once focus is already inside the overlay, and a
      // dialog opened over a terminal starts with focus elsewhere — so
      // the first Tab would walk into the page behind it.
      if (trapFocus(pageEl('choice-dialog'), e)) e.stopPropagation();
      return;
    }
    if (isModalOpen('launcher')) {
      return; // launcher's own listener handles keys
    }
    if (isModalOpen('project-editor')) {
      // The editor's own listener handles Enter; Escape and the backdrop
      // are ModalShell's. Tab containment has to live here for
      // the same reason it does for settings: a dialog opened over a
      // terminal starts with focus outside it, so a listener on the
      // dialog would never fire. Without this the editor claims
      // aria-modal="true" while Tab walks out into the sidebar.
      if (trapFocus(pageEl('project-editor'), e)) e.stopPropagation();
      return;
    }
    if (isModalOpen('command-palette')) {
      // The palette's own listener owns the keys — filtering, the
      // selection, Enter — because they all need its per-open state.
      // Escape is the exception, and it is here for the same reason
      // settings and the worktree browser keep theirs here: that
      // listener sits on #command-palette and only fires for keys typed
      // INSIDE it, so anything that moves focus out (the focus
      // pipeline's 8-frame retry is the one that does) leaves the
      // palette with no way to close and every key falling into a gate
      // that swallows it. Caught on CI, reproduced by blurring the
      // search box and pressing Escape.
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        closeCommandPalette();
      }
      return; // the palette owns the keyboard while open
    }
    if (isModalOpen('settings')) {
      // Unlike the help overlay, settings is a form with many focusable
      // inputs, so Tab is left alone to walk between them. The modal's
      // own listener also handles Escape and consumes it; this branch is
      // the fallback for when focus is still on the terminal, plus the
      // ⌘, toggle-to-close.
      if (e.key === 'Escape' || (cmdOrCtrl(e) && e.key === ',')) {
        e.preventDefault();
        e.stopPropagation();
        closeSettings();
      } else if (trapFocus(pageEl('settings'), e)) {
        e.stopPropagation();
      }
      return; // settings owns the keyboard while open
    }
    if (isModalOpen('worktrees')) {
      // Same shape as settings: the modal's own listener owns Escape
      // and the refresh key; this branch is the fallback for when
      // focus is still on the terminal, plus the ⌘E toggle-to-close.
      if (
        e.key === 'Escape' ||
        (cmdOrCtrl(e) && (e.key === 'e' || e.key === 'E'))
      ) {
        e.preventDefault();
        e.stopPropagation();
        closeWorktrees();
      } else if (trapFocus(pageEl('worktrees'), e)) {
        e.stopPropagation();
      }
      return; // the worktree browser owns the keyboard while open
    }
    if (isModalOpen('help')) {
      if (e.key === 'Escape' || isHelpOverlayKey(e)) {
        e.preventDefault();
        e.stopPropagation();
        closeHelpOverlay();
      } else if (trapFocus(pageEl('help-overlay'), e)) {
        // One focusable element in there, so the shared trap degenerates
        // to pinning focus on the close button — same result as the
        // hand-rolled version this replaced.
        e.stopPropagation();
      }
      return; // overlay owns the keyboard while open
    }

    // Dead-session overlay: route Enter/Escape to the active session's
    // overlay if it's shown. In grid mode the user can still click any
    // tile's buttons directly; this just handles the focused tile.
    const deadOverlayId = appData().activeId;
    if (deadOverlayId) {
      const t = termsMap().get(deadOverlayId);
      if (t?.deadOverlayShown) {
        if (e.key === 'Enter') {
          e.preventDefault();
          e.stopPropagation();
          t._closeDead();
          return;
        }
        if (e.key === 'Escape') {
          e.preventDefault();
          e.stopPropagation();
          t._dismissDead();
          return;
        }
      }
    }

    const swallow = () => {
      e.preventDefault();
      e.stopPropagation();
    };

    // Ctrl+` opens an OS terminal at the active session's worktree.
    // Mirrors VS Code; intentionally Ctrl on every platform — macOS
    // reserves ⌘` for native window cycling, so we never bind to it.
    // Handled before the ⌘/Ctrl gate below so it fires on mac too.
    if (
      e.ctrlKey &&
      !e.metaKey &&
      !e.altKey &&
      !e.shiftKey &&
      e.code === 'Backquote'
    ) {
      swallow();
      OpenTerminalAt(activeCwd()).catch(reportFailure('open terminal'));
      return;
    }

    // Session back / forward (Ctrl+- / Ctrl+Shift+- on mac, Ctrl+Alt+-
    // / Ctrl+Alt+Shift+- elsewhere). Handled before the ⌘/Ctrl gate below
    // for the same reason as Ctrl+` above: cmdOrCtrl rejects plain Ctrl on
    // macOS, so a binding placed after it would never fire there.
    const navDir = navHistoryKey(e, isMac);
    if (navDir) {
      swallow();
      if (navDir === 'back') navBack();
      else navForward();
      return;
    }

    const meta = cmdOrCtrl(e);
    if (!meta) return;

    if (e.key === '=' || e.key === '+') {
      swallow();
      deps.bumpFontSize(+1);
      return;
    }
    if (e.key === '-' || e.key === '_') {
      swallow();
      deps.bumpFontSize(-1);
      return;
    }
    if (e.key === '0') {
      swallow();
      deps.resetFontSize();
      return;
    }

    if ((e.key === 'k' || e.key === 'K') && e.shiftKey) {
      swallow();
      openCommandPalette();
      return;
    }
    // ⌘⏎ / Ctrl+Enter — from a grid, zoom into the tile you navigated to.
    // Deliberately ONE-WAY: single → grid stays on ⌘G / ⇧⌘G. A symmetric
    // toggle would have to claim ⌘⏎ in focused mode too, and that is the
    // exact conflict spec #217 documented — Claude/Codex bind Cmd+Enter
    // themselves, so in single view the key must fall through untouched.
    // ⇧⌘⏎ stays unclaimed in every view for the same reason.
    if (e.key === 'Enter') {
      if (e.shiftKey || appData().view === 'single') return;
      swallow();
      focusActiveSession();
      return;
    }
    // Both ⌘/ and ⌘? open the shortcuts panel — see isHelpOverlayKey.
    if (isHelpOverlayKey(e)) {
      swallow();
      openHelpOverlay();
      return;
    }
    // ⌘, / Ctrl+, — the standard Settings shortcut. On macOS the File menu
    // carries the same accelerator (see menu_darwin.go — Wails v2 can't
    // append to the native App menu); on Windows/Linux buildAppMenu returns
    // nil (see menu_other.go), so this is the only path.
    if (e.key === ',') {
      swallow();
      openSettings();
      return;
    }
    if (e.key === 'p' || e.key === 'P') {
      swallow();
      if (e.shiftKey) duplicateActiveSessionChooseTool();
      else duplicateActiveSession();
    } else if (e.key === 't' || e.key === 'T') {
      swallow();
      if (e.shiftKey) openLauncher(undefined, { forceWorktree: true });
      else openLauncher();
    } else if (e.key === 'Backspace' && e.shiftKey) {
      swallow();
      deleteActiveProject();
    } else if (e.key === 'e' || e.key === 'E') {
      swallow();
      openWorktreesForActiveProject();
    } else if (e.key === 's' || e.key === 'S') {
      swallow();
      // Route through toggleSidebar so the keyboard path stays in
      // lockstep with the menu / command-palette path (including the
      // post-reflow refocus added for #208 R3). Inline class flips
      // here previously skipped the refocus and stranded keystrokes
      // on document.body after a prior window resize.
      toggleSidebar();
    } else if (e.key === 'g' || e.key === 'G') {
      swallow();
      if (e.shiftKey) {
        setView(appData().view === 'grid-all' ? 'single' : 'grid-all');
      } else {
        setView(appData().view === 'grid-project' ? 'single' : 'grid-project');
      }
    } else if (e.key === 'n' || e.key === 'N') {
      swallow();
      if (e.shiftKey) {
        OpenNewWindow().catch(reportFailure('new window'));
      } else {
        // ⌘N — new project. (⌥⌘N is reserved by macOS Spotlight.)
        openProjectEditor(null);
      }
    } else if (e.key === 'b' || e.key === 'B') {
      swallow();
      if (e.shiftKey) jumpBack();
      else jumpToAttention();
    } else if (e.key === 'w' || e.key === 'W') {
      swallow();
      if (e.shiftKey) {
        CloseWindow().catch(reportFailure('close window'));
      } else if (appData().activeId) {
        // force=false: lets the daemon refuse with worktree_dirty if
        // the worktree has uncommitted changes; the control:error
        // handler then shows a confirm dialog and retries with force.
        closeActiveSession();
      }
    } else if (e.key === 'z' || e.key === 'Z') {
      // ⌘Z — undo the close you just made. ⇧⌘Z is left unbound: it
      // reads as redo, which this has no counterpart for.
      if (!e.shiftKey) {
        swallow();
        reopenLastClosedSession();
      }
    } else if (/^[1-9]$/.test(e.key)) {
      const idx = parseInt(e.key, 10) - 1;
      const ord = orderedSessions();
      if (idx < ord.length) {
        swallow();
        switchTo(ord[idx].id);
      }
    } else if (e.key === 'ArrowLeft') {
      // Horizontal arrows are only ours in grid mode; handleArrow says
      // so by returning false, and we leave the key to the terminal.
      if (handleArrow(-1, 0, e.shiftKey)) swallow();
    } else if (e.key === 'ArrowRight') {
      if (handleArrow(+1, 0, e.shiftKey)) swallow();
    } else if (e.key === 'ArrowUp') {
      swallow();
      handleArrow(0, -1, e.shiftKey);
    } else if (e.key === 'ArrowDown') {
      swallow();
      handleArrow(0, +1, e.shiftKey);
    } else if (e.key === '[') {
      swallow();
      shiftActiveProject(-1);
    } else if (e.key === ']') {
      swallow();
      shiftActiveProject(+1);
    }
  },
  true,
);

// ---------- menu actions ----------
//
// Native menu items emit `menu:<action>` events from cmd/hivegui/menu.go.
// They dispatch to the same handlers as the keyboard listener above so the
// menu and keyboard stay in lockstep — when you add a shortcut, add it
// here AND in menu.go.

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
    if (staleId && appData().attention.has(staleId)) {
      clearAttention(staleId);
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
function toggleScrollDebug() {
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
function copyScrollTrace() {
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

const menuActions = {
  'menu:new-session': () => openLauncher(),
  'menu:new-session-worktree': () =>
    openLauncher(undefined, { forceWorktree: true }),
  'menu:duplicate-session': duplicateActiveSession,
  'menu:duplicate-session-choose-tool': duplicateActiveSessionChooseTool,
  'menu:restart-session': restartActiveSession,
  'menu:new-project': () => openProjectEditor(null),
  'menu:delete-project': () => deleteActiveProject(),
  'menu:command-palette': () => openCommandPalette(),
  'menu:settings': () => openSettings(),
  'menu:worktrees': () => openWorktreesForActiveProject(),
  'menu:close-session': () => closeActiveSession(),
  'menu:reopen-closed-session': () => reopenLastClosedSession(),
  'menu:zoom-in': () => deps.bumpFontSize(+1),
  'menu:zoom-out': () => deps.bumpFontSize(-1),
  'menu:zoom-reset': () => deps.resetFontSize(),
  'menu:toggle-sidebar': toggleSidebar,
  'menu:toggle-project-grid': toggleProjectGrid,
  'menu:toggle-all-grid': toggleAllGrid,
  'menu:next-session': () => navSession(+1),
  'menu:prev-session': () => navSession(-1),
  'menu:move-session-forward': () => reorderActive(+1),
  'menu:move-session-backward': () => reorderActive(-1),
  'menu:next-attention': jumpToAttention,
  'menu:jump-back': jumpBack,
  'menu:next-project': () => shiftActiveProject(+1),
  'menu:prev-project': () => shiftActiveProject(-1),
  'menu:check-for-updates': () => manualUpdateCheck(),
  'menu:reload-gui': () => reloadGui(),
  'menu:restart-hive': () => restartHive(),
  // Must toggle, not just open: the native ⌘/ accelerator intercepts
  // the key before the webview on macOS, so the keydown close path
  // (Escape/⌘/ in the window listener) never sees ⌘/ while the menu
  // owns it.
  'menu:keyboard-shortcuts': () => toggleHelpOverlay(),
  'menu:toggle-scroll-debug': () => toggleScrollDebug(),
  'menu:copy-scroll-trace': () => copyScrollTrace(),
};
for (const [name, fn] of Object.entries(menuActions)) {
  EventsOn(name, fn);
}
for (let i = 1; i <= 9; i++) {
  EventsOn(`menu:switch-${i}`, () => switchToNthSession(i));
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
  KillProject(proj.id, sessions.length > 0).catch(
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
    // reorderTarget returns an index into the GLOBAL ordered list, which
    // is the index space the daemon's Update expects. Sending a
    // per-project index here is what used to scatter sessions across
    // project boundaries.
    const target = reorderTarget(ord, appData().activeId, delta);
    if (target == null) return;
    UpdateSession(ord[idx].id, '', '', target).catch(reportFailure('reorder'));
    return;
  }
  // Step OVER minimized sessions (their own tray, or their project's):
  // you put them away, so the arrows must not walk you back into them —
  // in a grid view landing on one has no tile and drops you to single.
  // The walk is over the full ordered list, not a filtered one, because
  // orderedSessions() is shared with the sidebar, tray, palette and
  // ⌘1-9, all of which still list everything.
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
