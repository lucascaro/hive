//go:build darwin

package main

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
)

// findItem walks the menu tree for the first item with the given label.
func findItem(items []*menu.MenuItem, label string) *menu.MenuItem {
	for _, it := range items {
		if it.Label == label {
			return it
		}
		if it.SubMenu != nil {
			if found := findItem(it.SubMenu.Items, label); found != nil {
				return found
			}
		}
	}
	return nil
}

// TestSessionMenuAttentionItems pins the ⌘B / ⇧⌘B menu entries.
//
// The menu reaches the frontend by emitting bare event-name strings that
// app/keyboard.ts registers in its menuActions map — a contract no
// compiler checks. Renaming one side leaves the menu item wired to
// nothing, and the app still builds and runs. The JS mirror of this
// assertion lives in frontend/test/dom/attention-jump.test.ts
// ("menu action ids").
func TestSessionMenuAttentionItems(t *testing.T) {
	m := buildAppMenu(&App{})
	if m == nil {
		t.Fatal("buildAppMenu returned nil on darwin")
	}

	for _, tc := range []struct {
		label     string
		wantShift bool
	}{
		{"Next Session Needing Attention", false},
		{"Jump Back to Where You Were", true},
	} {
		item := findItem(m.Items, tc.label)
		if item == nil {
			t.Errorf("menu item %q not found", tc.label)
			continue
		}
		if item.Accelerator == nil {
			t.Errorf("%q has no accelerator", tc.label)
			continue
		}
		if item.Accelerator.Key != "b" {
			t.Errorf("%q bound to %q, want \"b\"", tc.label, item.Accelerator.Key)
		}
		var hasShift, hasCmd bool
		for _, mod := range item.Accelerator.Modifiers {
			switch mod {
			case keys.ShiftKey:
				hasShift = true
			case keys.CmdOrCtrlKey:
				hasCmd = true
			}
		}
		if !hasCmd {
			t.Errorf("%q missing CmdOrCtrl modifier", tc.label)
		}
		if hasShift != tc.wantShift {
			t.Errorf("%q shift = %v, want %v", tc.label, hasShift, tc.wantShift)
		}
	}
}

// walkAccelerators collects every (label, accelerator) pair in the menu
// tree. findItem above searches by label and so can't express "no item
// binds this key", which is exactly the assertion below.
func walkAccelerators(items []*menu.MenuItem, out map[string]*keys.Accelerator) {
	for _, it := range items {
		if it.Accelerator != nil {
			out[it.Label] = it.Accelerator
		}
		if it.SubMenu != nil {
			walkAccelerators(it.SubMenu.Items, out)
		}
	}
}

// TestMenuHasNoEnterAccelerator keeps ⌘↩ out of the native menu (#249).
// Same mechanism as the arrow guard above: AppKit consumes a registered
// key equivalent before the webview sees a keydown, so a menu item here
// would toggle grid no matter what the frontend does — which is exactly
// how the "⌘Enter is unusable inside an agent session" bug worked. The
// Playwright specs cannot catch a re-add: they drive the browser mock,
// which has no native menu.
func TestMenuHasNoEnterAccelerator(t *testing.T) {
	m := buildAppMenu(&App{})
	if m == nil {
		t.Fatal("buildAppMenu returned nil on darwin")
	}
	accels := map[string]*keys.Accelerator{}
	walkAccelerators(m.Items, accels)
	for label, acc := range accels {
		if acc.Key == "enter" || acc.Key == "return" {
			t.Errorf("menu item %q binds %q; ⌘↩ must reach the terminal", label, acc.Key)
		}
	}
	// Guard the walker itself: ⌘G, the binding that replaced ⌘↩, must
	// still be found — otherwise this test passes on an empty walk.
	var g bool
	for _, acc := range accels {
		if acc.Key == "g" {
			g = true
		}
	}
	if !g {
		t.Error("walkAccelerators found no 'g' binding; the walker is broken")
	}
}

// TestMenuHasNoArrowLeftRightAccelerators keeps ⌘←/⌘→ (and their shifted
// forms) out of the native menu. AppKit consumes a registered key
// equivalent before the webview ever sees a keydown, so a menu item here
// steals start/end-of-line from the terminal no matter what the frontend
// does with the event.
func TestMenuHasNoArrowLeftRightAccelerators(t *testing.T) {
	m := buildAppMenu(&App{})
	if m == nil {
		t.Fatal("buildAppMenu returned nil on darwin")
	}
	accels := map[string]*keys.Accelerator{}
	walkAccelerators(m.Items, accels)
	for label, acc := range accels {
		if acc.Key == "left" || acc.Key == "right" {
			t.Errorf("menu item %q binds %q; ⌘←/⌘→ must reach the terminal", label, acc.Key)
		}
	}
	// Guard the walker itself: the vertical twins must still be there.
	var down bool
	for _, acc := range accels {
		if acc.Key == "down" {
			down = true
		}
	}
	if !down {
		t.Error("walkAccelerators found no 'down' binding; the walker is broken")
	}
}
