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
// cmd/hivegui/frontend/src/main.js MUST be reachable from this menu.
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
	file.AddSeparator()
	file.AddText("New Window",
		keys.Combo("n", keys.ShiftKey, keys.CmdOrCtrlKey),
		func(_ *menu.CallbackData) { _ = a.OpenNewWindow() })
	file.AddText("Close Session", keys.CmdOrCtrl("w"), emit("menu:close-session"))
	file.AddText("Close Window",
		keys.Combo("w", keys.ShiftKey, keys.CmdOrCtrlKey),
		func(_ *menu.CallbackData) { a.CloseWindow() })
	file.AddSeparator()
	file.AddText("Delete Project…",
		keys.Combo("backspace", keys.ShiftKey, keys.CmdOrCtrlKey),
		emit("menu:delete-project"))

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
	view.AddText("Toggle Grid (⌘↩ alternate)",
		keys.CmdOrCtrl("enter"), emit("menu:toggle-project-grid"))

	sess := m.AddSubmenu("Session")
	sess.AddText("Next Session", keys.CmdOrCtrl("down"), emit("menu:next-session"))
	sess.AddText("Previous Session", keys.CmdOrCtrl("up"), emit("menu:prev-session"))
	sess.AddText("Next Session (⌘→ alternate)", keys.CmdOrCtrl("right"), emit("menu:next-session"))
	sess.AddText("Previous Session (⌘← alternate)", keys.CmdOrCtrl("left"), emit("menu:prev-session"))
	sess.AddSeparator()
	sess.AddText("Move Session Forward",
		keys.Combo("down", keys.ShiftKey, keys.CmdOrCtrlKey),
		emit("menu:move-session-forward"))
	sess.AddText("Move Session Backward",
		keys.Combo("up", keys.ShiftKey, keys.CmdOrCtrlKey),
		emit("menu:move-session-backward"))
	sess.AddText("Move Session Forward (⇧⌘→ alternate)",
		keys.Combo("right", keys.ShiftKey, keys.CmdOrCtrlKey),
		emit("menu:move-session-forward"))
	sess.AddText("Move Session Backward (⇧⌘← alternate)",
		keys.Combo("left", keys.ShiftKey, keys.CmdOrCtrlKey),
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

	// Empty Help submenu — macOS auto-injects a Search field that
	// fuzzy-matches every item in every other menu, so the user can
	// search all actions from the menu bar without opening the palette.
	m.AddSubmenu("Help")

	return m
}
