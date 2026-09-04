//go:build darwin

package main

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// buildAppMenu wires every keyboard shortcut in the GUI into the
// native macOS menu. Menu items emit `menu:<action>` events that the
// frontend listens for and dispatches to the same handlers used by
// the in-window keyboard listener — so the menu stays in sync with
// keyboard behavior by going through one code path.
//
// Requirement: every keyboard shortcut in
// cmd/hivegui/frontend/src/main.ts MUST be reachable from this menu.
// macOS shows only one accelerator per item; alternate keys (e.g.
// ⌘← as another way to trigger Previous Session) are still wired in
// the JS keyboard handler.
func buildAppMenu(a *App) *menu.Menu {
	emit := func(name string) func(*menu.CallbackData) {
		return func(_ *menu.CallbackData) {
			if a.ctx == nil {
				return
			}
			wruntime.EventsEmit(a.ctx, name)
		}
	}

	m := menu.NewMenu()
	m.Append(menu.AppMenu()) // About / Hide / Quit (⌘Q)

	file := m.AddSubmenu("File")
	file.AddText("New Project…", keys.CmdOrCtrl("n"), emit("menu:new-project"))
	file.AddText("New Session", keys.CmdOrCtrl("t"), emit("menu:new-session"))
	file.AddText("New Session in Worktree",
		keys.Combo("t", keys.ShiftKey, keys.CmdOrCtrlKey),
		emit("menu:new-session-worktree"))
	file.AddText("Duplicate Session",
		keys.CmdOrCtrl("p"),
		emit("menu:duplicate-session"))
	file.AddText("Duplicate Session (choose tool)…",
		keys.Combo("p", keys.ShiftKey, keys.CmdOrCtrlKey),
		emit("menu:duplicate-session-choose-tool"))
	file.AddText("Restart Session", nil, emit("menu:restart-session"))
	file.AddSeparator()
	file.AddText("New Window",
		keys.Combo("n", keys.ShiftKey, keys.CmdOrCtrlKey),
		func(_ *menu.CallbackData) { _ = a.OpenNewWindow() })
	file.AddText("Close Session", keys.CmdOrCtrl("w"), emit("menu:close-session"))
	file.AddText("Close Window",
		keys.Combo("w", keys.ShiftKey, keys.CmdOrCtrlKey),
		func(_ *menu.CallbackData) { a.CloseWindow() })
	// ⌘Z, the reflex after an accidental ⌘W. Hive has no Edit menu, so
	// there is no stock Undo item to collide with, and ⌘ is not a
	// terminal modifier — xterm.js never received this chord, so
	// binding it takes nothing away from the focused agent.
	file.AddText("Reopen Closed Session",
		keys.CmdOrCtrl("z"),
		emit("menu:reopen-closed-session"))
	file.AddSeparator()
	file.AddText("Delete Project…",
		keys.Combo("backspace", keys.ShiftKey, keys.CmdOrCtrlKey),
		emit("menu:delete-project"))
	file.AddSeparator()
	file.AddText("Check for Updates…", nil, emit("menu:check-for-updates"))
	// Reload relaunches every GUI window and leaves hived — and every
	// running shell and agent — alone. It is the cheap half of what
	// used to be a single "Restart Hive".
	//
	// No accelerator, even though this one is harmless. ⌘R is the
	// browser reload reflex, and a user who fires it out of habit while
	// an agent is mid-run loses their window (and their scroll
	// position) for nothing. The palette and this menu are enough.
	file.AddText("Reload GUI", nil, emit("menu:reload-gui"))
	// No accelerator: this terminates every running shell and agent,
	// which is not something to leave one fat-finger away. The label
	// names the cost, because it now sits next to an item that looks
	// similar and costs nothing.
	file.AddText("Restart Daemon… (ends all sessions)", nil, emit("menu:restart-hive"))
	// macOS convention puts Settings in the app menu, but Wails v2
	// builds that menu entirely in Objective-C from a role enum
	// (WailsMenu.m's appendRole) — processMenuItem returns as soon as
	// it sees Role != 0, so an appended item is never traversed.
	// Hand-building the app menu instead would forfeit Hide / Hide
	// Others / Show All, which need selectors Go can't invoke. File is
	// the next-best home; ⌘, is what users actually reach for.
	file.AddText("Settings…", keys.CmdOrCtrl(","), emit("menu:settings"))

	m.Append(menu.EditMenu()) // Cut / Copy / Paste / Select All

	view := m.AddSubmenu("View")
	view.AddText("Command Palette…",
		keys.Combo("k", keys.ShiftKey, keys.CmdOrCtrlKey),
		emit("menu:command-palette"))
	view.AddSeparator()
	view.AddText("Zoom In", keys.CmdOrCtrl("="), emit("menu:zoom-in"))
	view.AddText("Zoom Out", keys.CmdOrCtrl("-"), emit("menu:zoom-out"))
	view.AddText("Actual Size", keys.CmdOrCtrl("0"), emit("menu:zoom-reset"))
	view.AddSeparator()
	view.AddText("Toggle Sidebar", keys.CmdOrCtrl("s"), emit("menu:toggle-sidebar"))
	view.AddSeparator()
	view.AddText("Toggle Project Grid", keys.CmdOrCtrl("g"), emit("menu:toggle-project-grid"))
	view.AddText("Toggle All Sessions Grid",
		keys.Combo("g", keys.ShiftKey, keys.CmdOrCtrlKey),
		emit("menu:toggle-all-grid"))

	sess := m.AddSubmenu("Session")
	sess.AddText("Next Session", keys.CmdOrCtrl("down"), emit("menu:next-session"))
	sess.AddText("Previous Session", keys.CmdOrCtrl("up"), emit("menu:prev-session"))
	sess.AddSeparator()
	sess.AddText("Next Session Needing Attention",
		keys.CmdOrCtrl("b"), emit("menu:next-attention"))
	sess.AddText("Jump Back to Where You Were",
		keys.Combo("b", keys.ShiftKey, keys.CmdOrCtrlKey),
		emit("menu:jump-back"))
	sess.AddSeparator()
	sess.AddText("Move Session Forward",
		keys.Combo("down", keys.ShiftKey, keys.CmdOrCtrlKey),
		emit("menu:move-session-forward"))
	sess.AddText("Move Session Backward",
		keys.Combo("up", keys.ShiftKey, keys.CmdOrCtrlKey),
		emit("menu:move-session-backward"))
	sess.AddSeparator()
	sess.AddText("Next Project", keys.CmdOrCtrl("]"), emit("menu:next-project"))
	sess.AddText("Previous Project", keys.CmdOrCtrl("["), emit("menu:prev-project"))
	sess.AddSeparator()
	for i := 1; i <= 9; i++ {
		k := string(rune('0' + i))
		sess.AddText("Switch to Session "+k, keys.CmdOrCtrl(k), emit("menu:switch-"+k))
	}

	m.Append(menu.WindowMenu()) // Minimize / Zoom / Front

	// Debug submenu — surfaces the hive.debug tracer (scroll/replay,
	// grid relayout, focus reconciliation, keydown routing, and a
	// main-thread heartbeat) without requiring devtools (production
	// WKWebView builds ship with the web inspector disabled). The toggle
	// arms the tracer and reloads (the tracer latches its on/off at page
	// load); "Copy Debug Trace" puts the captured event ring + counters on
	// the clipboard so it can be pasted straight into a bug report — no
	// console needed.
	//
	// The label names the state the item MOVES to, so it also reports the
	// current one — an armed tracer has no other visible sign. a.debugTrace
	// is pushed up by the frontend at startup (App.SetDebugTrace), which
	// rebuilds this menu.
	debug := m.AddSubmenu("Debug")
	traceLabel := "Turn Debug Trace On (Reloads)"
	if a.debugTrace {
		traceLabel = "Turn Debug Trace Off (Reloads)"
	}
	debug.AddText(traceLabel, nil, emit("menu:toggle-scroll-debug"))
	debug.AddText("Copy Debug Trace", nil, emit("menu:copy-scroll-trace"))

	// Help submenu — macOS auto-injects a Search field that
	// fuzzy-matches every item in every other menu, so the user can
	// search all actions from the menu bar without opening the palette.
	help := m.AddSubmenu("Help")
	// One accelerator covers both ⌘/ and ⌘?: AppKit matches menu key
	// equivalents on the unshifted character, so Cmd+Shift+/ (which the
	// webview would report as "?") fires this ⌘/ item too. Verified by
	// hand on macOS — both chords open the overlay.
	//
	// So do NOT add a second item for ⌘?, and do not "fix" this to
	// keys.Combo("/", CmdOrCtrlKey, ShiftKey): that would narrow the mask
	// to require Shift and stop plain ⌘/ from matching here.
	//
	// The '?' branch in app/keyboard.ts is therefore unreachable on
	// darwin (the menu consumes the chord first) and exists for
	// Windows/Linux, where buildAppMenu returns nil and there is no menu
	// to handle it.
	help.AddText("Keyboard Shortcuts", keys.CmdOrCtrl("/"), emit("menu:keyboard-shortcuts"))

	return m
}
