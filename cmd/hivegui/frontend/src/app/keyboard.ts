// ---------- keyboard, menu actions, app commands ----------
//
// Moved verbatim from main.js. Font helpers and focusActiveTerm are
// injected via initKeyboard(deps) (they move in stage 6); everything
// else is imported from the sibling modules.

import {
  EventsOn,
  KillSession,
  KillProject,
  Confirm,
  UpdateSession,
  OpenNewWindow,
  CloseWindow,
  OpenTerminalAt,
  SetClipboardText,
  Notify,
} from '../bridge.js';
import { state } from './state.js';
import { flashStatus, reportFailure } from './dom.js';
import {
  orderedSessions,
  activeCwd,
  activeProjectId,
  nextAttentionId,
} from './selectors.js';
import { cmdOrCtrl, isMac } from '../lib/platform.js';
import {
  launcherEl,
  openLauncher,
  duplicateActiveSession,
  duplicateActiveSessionChooseTool,
  restartActiveSession,
} from './modals/launcher.js';
import { editorEl, openProjectEditor } from './modals/project-editor.js';
import { openCommandPalette } from './modals/command-palette.js';
import { openSettings, closeSettings } from './modals/settings.js';
import { openWorktrees, closeWorktrees } from './modals/worktrees.js';
import {
  choiceDialogOpen,
  dismissChoiceDialog,
  choiceDialogEl,
} from './modals/choice-dialog.js';
import { trapFocus } from './modals/focus-trap.js';
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
} from './view.js';
import { manualUpdateCheck, restartHive } from './banners.js';
import { clearAttention } from './events.js';
import { updateSidebarSelection } from './sidebar.js';
import { goBack, goForward } from '../lib/nav-history.js';
import { reorderTarget } from '../lib/reorder.js';
import { scrollTrace } from './trace.js';
import { mustEl } from './el.js';
import type { ProjectInfo } from './state.js';

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
  // Injected from main.ts like focusActiveTerm above: keyboard.ts must
  // not import the focus pipeline directly (see the acyclic-modules
  // note at the wiring block in main.ts). The default still RUNS fn —
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
        view: state.view,
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
    // bindings underneath while it is up. Checked before every modal —
    // it is mounted on <body>, so it can sit over any of them.
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
      const dialogEl = choiceDialogEl();
      if (dialogEl && trapFocus(dialogEl, e)) e.stopPropagation();
      return;
    }
    if (!launcherEl.classList.contains('hidden')) {
      return; // launcher's own listener handles keys
    }
    if (!editorEl.classList.contains('hidden')) {
      return; // editor's own listener handles keys
    }
    const _palette = document.getElementById('command-palette');
    if (_palette && !_palette.classList.contains('hidden')) {
      return; // palette's own listener handles keys
    }
    const _settings = document.getElementById('settings');
    if (_settings && !_settings.classList.contains('hidden')) {
      // Unlike the help overlay, settings is a form with many focusable
      // inputs, so Tab is left alone to walk between them. The modal's
      // own listener also handles Escape and consumes it; this branch is
      // the fallback for when focus is still on the terminal, plus the
      // ⌘, toggle-to-close.
      if (e.key === 'Escape' || (cmdOrCtrl(e) && e.key === ',')) {
        e.preventDefault();
        e.stopPropagation();
        closeSettings();
      } else if (trapFocus(_settings, e)) {
        e.stopPropagation();
      }
      return; // settings owns the keyboard while open
    }
    const _worktrees = document.getElementById('worktrees');
    if (_worktrees && !_worktrees.classList.contains('hidden')) {
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
      } else if (trapFocus(_worktrees, e)) {
        e.stopPropagation();
      }
      return; // the worktree browser owns the keyboard while open
    }
    const _help = document.getElementById('help-overlay');
    if (_help && !_help.classList.contains('hidden')) {
      if (e.key === 'Escape' || isHelpOverlayKey(e)) {
        e.preventDefault();
        e.stopPropagation();
        closeHelpOverlay();
      } else if (trapFocus(_help, e)) {
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
    if (state.activeId) {
      const t = state.terms.get(state.activeId);
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
        setView(state.view === 'grid-all' ? 'single' : 'grid-all');
      } else {
        setView(state.view === 'grid-project' ? 'single' : 'grid-project');
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
      } else if (state.activeId) {
        // force=false: lets the daemon refuse with worktree_dirty if
        // the worktree has uncommitted changes; the control:error
        // handler then shows a confirm dialog and retries with force.
        KillSession(state.activeId, false).catch(reportFailure('close'));
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

export function toggleProjectGrid() {
  setView(state.view === 'grid-project' ? 'single' : 'grid-project');
}

export function toggleAllGrid() {
  setView(state.view === 'grid-all' ? 'single' : 'grid-all');
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
  if (state.view !== 'single') {
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
// recording where you came from in state.attentionReturnId so ⇧⌘B can
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
    if (state.activeId && state.attention.has(state.activeId)) {
      clearAttention(state.activeId);
      updateSidebarSelection();
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
  if (id === state.attentionReturnId) endRound({ reminimize: false });
  else if (!state.attentionReturnId) state.attentionReturnId = state.activeId;

  if (state.minimized.has(id)) {
    state.attentionRestored.add(id);
    restoreSession(id); // un-minimize + re-render tray, then switchTo
  } else {
    switchTo(id);
  }
}

// jumpBack (⇧⌘B) returns to the session held before the first ⌘B and
// ends the round, so the next ⌘B starts a fresh one. The anchored
// session can be killed while you're away, hence the still-exists guard.
export function jumpBack() {
  const id = state.attentionReturnId;
  if (!id || !state.sessions.some((s) => s.id === id)) {
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
// adding a dead id to state.minimized would strand a chip in the tray.
function endRound({ reminimize }: { reminimize: boolean }) {
  state.attentionReturnId = null;
  if (reminimize) {
    for (const rid of state.attentionRestored) {
      if (rid !== state.activeId && state.sessions.some((s) => s.id === rid)) {
        minimizeSession(rid);
      }
    }
  }
  state.attentionRestored.clear();
}

// navBack / navForward (Ctrl+- / Ctrl+Shift+-) walk the session history
// recorded by setActive. withoutNavHistory keeps the replay from being
// recorded as new navigation — otherwise back would immediately push the
// session it just left and the two keys would ping-pong.
//
// sessionExists mirrors jumpBack's still-exists guard: a session on the
// stack can be killed while you are elsewhere, and the stack walk skips
// it rather than dead-ending.
const sessionExists = (id: string) => state.sessions.some((s) => s.id === id);

// navGo performs the switch a history step resolved to. A minimized
// session has to be restored on the way in, exactly as ⌘B does at
// jumpToAttention: gridScopeSessions filters state.minimized, so a bare
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
    if (state.minimized.has(id))
      restoreSession(id); // un-minimize, then switchTo
    else switchTo(id);
  });
}

export function navBack() {
  const id = goBack(state.nav, state.activeId, sessionExists);
  if (!id) {
    flashStatus('nothing to go back to');
    return;
  }
  navGo(id);
}

export function navForward() {
  const id = goForward(state.nav, state.activeId, sessionExists);
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
  'menu:close-session': () => {
    if (state.activeId)
      KillSession(state.activeId, false).catch(reportFailure('close'));
  },
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
// shared by the sidebar ✕ button and the ⇧⌘⌫ shortcut. Kept as one
// function so the prompt text and killSessions logic can't drift.
export async function confirmAndDeleteProject(
  proj: ProjectInfo | undefined | null,
) {
  if (!proj) return;
  const sessions = state.sessions.filter(
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
  confirmAndDeleteProject(state.projects.find((p) => p.id === pid));
}

// The worktree browser is per-project, so the keyboard and palette
// paths resolve the project the same way every other project-scoped
// action does.
export function openWorktreesForActiveProject() {
  const pid = activeProjectId();
  openWorktrees(state.projects.find((p) => p.id === pid) ?? null);
}

// moveActiveSession walks the (project_order, session_order) list.
// reorder=true moves the session within its project only.
export function moveActiveSession(delta: number, reorder: boolean) {
  const ord = orderedSessions();
  const n = ord.length;
  if (n === 0) return;
  const idx = ord.findIndex((s) => s.id === state.activeId);
  if (idx < 0) {
    switchTo(ord[0].id);
    return;
  }
  if (reorder) {
    // reorderTarget returns an index into the GLOBAL ordered list, which
    // is the index space the daemon's Update expects. Sending a
    // per-project index here is what used to scatter sessions across
    // project boundaries.
    const target = reorderTarget(ord, state.activeId, delta);
    if (target == null) return;
    UpdateSession(ord[idx].id, '', '', target).catch(reportFailure('reorder'));
    return;
  }
  const next = (idx + delta + n) % n;
  switchTo(ord[next].id);
}
