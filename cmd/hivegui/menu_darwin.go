//go:build darwin

package main

import (
	"log"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// buildAppMenu wires the GUI's commands into the native macOS menu. Menu
// items emit `menu:<action>` events that the frontend dispatches through
// the same command bus as its keyboard listener (app/commands.ts
// MENU_COMMANDS), so a key and its menu item run one code path.
//
// The accelerators below are the DEFAULTS. They must equal the chords in
// cmd/hivegui/frontend/src/lib/bindings.ts — both sides are pinned to
// testdata/menu-default-accelerators.json. The user's keymap (spec 477)
// overrides them per command id through a.menuAccel (SetMenuAccelerators).
// macOS shows only one accelerator per item; a command's other chords,
// and every non-⌘ chord, reach the webview as keydowns instead.
//
// The caller holds a.menuMu (App.rebuildMenu), or builds before Wails
// starts; this never takes the lock.
func buildAppMenu(a *App) *menu.Menu {
	return buildAppMenuRecorded(a, nil)
}

// buildAppMenuRecorded builds the menu and, when record is non-nil, fills
// it with every item's resolved accelerator by command id ("" = none).
// Tests use it to prove each item goes through accel().
func buildAppMenuRecorded(a *App, record map[string]string) *menu.Menu {
	accel := func(id string, def *keys.Accelerator) *keys.Accelerator {
		acc := def
		if s, ok := a.menuAccel[id]; ok {
			acc = parseMenuAccel(id, s)
		}
		if a.menuSuspended {
			acc = nil
		}
		if record != nil {
			record[id] = accelString(acc)
		}
		return acc
	}
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
	file.AddText("New Project…", accel("new-project", keys.CmdOrCtrl("n")), emit("menu:new-project"))
	file.AddText("New Session", accel("new-session", keys.CmdOrCtrl("t")), emit("menu:new-session"))
	file.AddText("New Session in Worktree", accel("new-session-worktree", keys.Combo("t", keys.ShiftKey, keys.CmdOrCtrlKey)), emit("menu:new-session-worktree"))
	file.AddText("Duplicate Session", accel("duplicate-session", keys.CmdOrCtrl("p")), emit("menu:duplicate-session"))
	file.AddText("Duplicate Session (choose tool)…", accel("duplicate-session-choose-tool", keys.Combo("p", keys.ShiftKey, keys.CmdOrCtrlKey)), emit("menu:duplicate-session-choose-tool"))
	file.AddText("Restart Session", accel("restart-session", nil), emit("menu:restart-session"))
	file.AddSeparator()
	file.AddText("New Window",
		accel("new-window", keys.Combo("n", keys.ShiftKey, keys.CmdOrCtrlKey)),
		func(_ *menu.CallbackData) { _ = a.OpenNewWindow() })
	file.AddText("Close Session", accel("close-session", keys.CmdOrCtrl("w")), emit("menu:close-session"))
	file.AddText("Close Window",
		accel("close-window", keys.Combo("w", keys.ShiftKey, keys.CmdOrCtrlKey)),
		func(_ *menu.CallbackData) { a.CloseWindow() })
	// ⌘Z, the reflex after an accidental ⌘W. The stock Edit menu below
	// also claims ⌘Z for Undo; this File item has always won it. ⌘ is not
	// a terminal modifier — xterm.js never received this chord, so
	// binding it takes nothing away from the focused agent.
	file.AddText("Reopen Closed Session", accel("reopen-closed-session", keys.CmdOrCtrl("z")), emit("menu:reopen-closed-session"))
	file.AddSeparator()
	// ⌘I files a note about anything, from anywhere; ⇧⌘I opens the
	// active project's inbox. Both are ⌘ chords the terminal never
	// receives, so binding them takes nothing from the focused agent.
	file.AddText("Capture Idea…", accel("quick-idea", keys.CmdOrCtrl("i")), emit("menu:quick-idea"))
	file.AddText("Idea Inbox…", accel("idea-inbox", keys.Combo("i", keys.ShiftKey, keys.CmdOrCtrlKey)), emit("menu:idea-inbox"))
	file.AddSeparator()
	file.AddText("Delete Project…", accel("delete-project", keys.Combo("backspace", keys.ShiftKey, keys.CmdOrCtrlKey)), emit("menu:delete-project"))
	file.AddSeparator()
	file.AddText("Check for Updates…", accel("check-for-updates", nil), emit("menu:check-for-updates"))
	// Reload relaunches every GUI window and leaves hived — and every
	// running shell and agent — alone. It is the cheap half of what
	// used to be a single "Restart Hive".
	//
	// No accelerator, even though this one is harmless. ⌘R is the
	// browser reload reflex, and a user who fires it out of habit while
	// an agent is mid-run loses their window (and their scroll
	// position) for nothing. The palette and this menu are enough.
	file.AddText("Reload GUI", accel("reload-gui", nil), emit("menu:reload-gui"))
	// No accelerator: this terminates every running shell and agent,
	// which is not something to leave one fat-finger away. The label
	// names the cost, because it now sits next to an item that looks
	// similar and costs nothing.
	file.AddText("Restart Daemon… (ends all sessions)", accel("restart-hive", nil), emit("menu:restart-hive"))
	// macOS convention puts Settings in the app menu, but Wails v2
	// builds that menu entirely in Objective-C from a role enum
	// (WailsMenu.m's appendRole) — processMenuItem returns as soon as
	// it sees Role != 0, so an appended item is never traversed.
	// Hand-building the app menu instead would forfeit Hide / Hide
	// Others / Show All, which need selectors Go can't invoke. File is
	// the next-best home; ⌘, is what users actually reach for.
	file.AddText("Settings…", accel("settings", keys.CmdOrCtrl(",")), emit("menu:settings"))

	m.Append(menu.EditMenu()) // Cut / Copy / Paste / Select All

	view := m.AddSubmenu("View")
	view.AddText("Command Palette…", accel("command-palette", keys.Combo("k", keys.ShiftKey, keys.CmdOrCtrlKey)), emit("menu:command-palette"))
	view.AddSeparator()
	view.AddText("Zoom In", accel("zoom-in", keys.CmdOrCtrl("=")), emit("menu:zoom-in"))
	view.AddText("Zoom Out", accel("zoom-out", keys.CmdOrCtrl("-")), emit("menu:zoom-out"))
	view.AddText("Actual Size", accel("zoom-reset", keys.CmdOrCtrl("0")), emit("menu:zoom-reset"))
	view.AddSeparator()
	// Find lives under View rather than Edit because Edit is Wails'
	// stock menu.EditMenu() (appended above) — it gives us Cut / Copy /
	// Paste / Select All for free and there is no supported way to add
	// an item to it. View is the next-best home.
	//
	// This menu item is the REAL entry point for ⌘F on macOS: the native
	// accelerator intercepts the key before the webview. (The macOS chord
	// on find-in-session in frontend/src/lib/bindings.ts exists so this
	// accelerator can be derived from it.) It opens the box, or with
	// the box already open refocuses it and selects the query — the
	// find-field convention. It never closes it; Escape does.
	view.AddText("Find in Session…", accel("find-in-session", keys.CmdOrCtrl("f")), emit("menu:find-in-session"))
	view.AddSeparator()
	view.AddText("Toggle Sidebar", accel("toggle-sidebar", keys.CmdOrCtrl("s")), emit("menu:toggle-sidebar"))
	view.AddSeparator()
	view.AddText("Toggle Project Grid", accel("toggle-project-grid", keys.CmdOrCtrl("g")), emit("menu:toggle-project-grid"))
	view.AddText("Toggle All Sessions Grid", accel("toggle-all-grid", keys.Combo("g", keys.ShiftKey, keys.CmdOrCtrlKey)), emit("menu:toggle-all-grid"))
	view.AddSeparator()
	view.AddText("Toggle Agent Activity", accel("toggle-activity", keys.CmdOrCtrl("j")), emit("menu:toggle-activity"))
	view.AddText("Agent Activity Grid", accel("activity-grid", keys.Combo("j", keys.ShiftKey, keys.CmdOrCtrlKey)), emit("menu:activity-grid"))

	sess := m.AddSubmenu("Session")
	sess.AddText("Next Session", accel("next-session", keys.CmdOrCtrl("down")), emit("menu:next-session"))
	sess.AddText("Previous Session", accel("prev-session", keys.CmdOrCtrl("up")), emit("menu:prev-session"))
	sess.AddSeparator()
	sess.AddText("Next Session Needing Attention", accel("next-attention", keys.CmdOrCtrl("b")), emit("menu:next-attention"))
	sess.AddText("Jump Back to Where You Were", accel("jump-back", keys.Combo("b", keys.ShiftKey, keys.CmdOrCtrlKey)), emit("menu:jump-back"))
	sess.AddSeparator()
	sess.AddText("Move Session Forward", accel("move-forward", keys.Combo("down", keys.ShiftKey, keys.CmdOrCtrlKey)), emit("menu:move-session-forward"))
	sess.AddText("Move Session Backward", accel("move-backward", keys.Combo("up", keys.ShiftKey, keys.CmdOrCtrlKey)), emit("menu:move-session-backward"))
	sess.AddSeparator()
	sess.AddText("Next Project", accel("next-project", keys.CmdOrCtrl("]")), emit("menu:next-project"))
	sess.AddText("Previous Project", accel("prev-project", keys.CmdOrCtrl("[")), emit("menu:prev-project"))
	sess.AddSeparator()
	for i := 1; i <= 9; i++ {
		k := string(rune('0' + i))
		sess.AddText("Switch to Session "+k, accel("switch-"+k, keys.CmdOrCtrl(k)), emit("menu:switch-"+k))
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
	debug.AddText(traceLabel, accel("toggle-scroll-debug", nil), emit("menu:toggle-scroll-debug"))
	debug.AddText("Copy Debug Trace", accel("copy-scroll-trace", nil), emit("menu:copy-scroll-trace"))

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
	// The '?' help chord in frontend/src/lib/bindings.ts is therefore unreachable on
	// darwin (the menu consumes the chord first) and exists for
	// Windows/Linux, where buildAppMenu returns nil and there is no menu
	// to handle it.
	help.AddText("Keyboard Shortcuts", accel("keyboard-shortcuts", keys.CmdOrCtrl("/")), emit("menu:keyboard-shortcuts"))

	return m
}

// parseMenuAccel turns a keymap accelerator ("cmdorctrl+shift+t") into a
// Wails one. "" is no accelerator. One that will not parse is ALSO none:
// falling back to the default would leave the old chord firing through
// the menu after the user moved it.
func parseMenuAccel(id, s string) *keys.Accelerator {
	if s == "" {
		return nil
	}
	acc, err := keys.Parse(s)
	if err != nil {
		log.Printf("menu: ignoring accelerator %q for %s: %v", s, id, err)
		return nil
	}
	return acc
}

// accelString is the canonical form both sides of the parity fixture
// use: modifiers in a fixed order, then the key ("+" spelled "plus").
func accelString(acc *keys.Accelerator) string {
	if acc == nil {
		return ""
	}
	parts := []string{}
	for _, want := range []keys.Modifier{keys.CmdOrCtrlKey, keys.ControlKey, keys.OptionOrAltKey, keys.ShiftKey} {
		for _, m := range acc.Modifiers {
			if m == want {
				parts = append(parts, string(m))
				break
			}
		}
	}
	k := strings.ToLower(acc.Key)
	if k == "+" {
		k = "plus"
	}
	return strings.Join(append(parts, k), "+")
}
