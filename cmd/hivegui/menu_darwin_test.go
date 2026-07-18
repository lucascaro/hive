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
// app/keyboard.js registers in its menuActions map — a contract no
// compiler checks. Renaming one side leaves the menu item wired to
// nothing, and the app still builds and runs. The JS mirror of this
// assertion lives in frontend/test/dom/attention-jump.test.js
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
