package tui

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/lucascaro/hive/internal/config"
)

func TestNewKeyMap_BindingsMatchConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	km := NewKeyMap(cfg.Keybindings)

	cases := []struct {
		name    string
		binding string
		want    string
	}{
		{"NewProject", km.NewProject.Keys()[0], cfg.Keybindings.NewProject},
		{"NewSession", km.NewSession.Keys()[0], cfg.Keybindings.NewSession},
		{"NewTeam", km.NewTeam.Keys()[0], cfg.Keybindings.NewTeam},
		{"KillSession", km.KillSession.Keys()[0], cfg.Keybindings.KillSession},
		{"KillTeam", km.KillTeam.Keys()[0], cfg.Keybindings.KillTeam},
		{"Rename", km.Rename.Keys()[0], cfg.Keybindings.Rename},
		{"Filter", km.Filter.Keys()[0], cfg.Keybindings.Filter},
		{"Help", km.Help.Keys()[0], cfg.Keybindings.Help},
		{"Quit", km.Quit.Keys()[0], cfg.Keybindings.Quit},
		{"QuitKill", km.QuitKill.Keys()[0], cfg.Keybindings.QuitKill},
		{"ColorNext", km.ColorNext.Keys()[0], cfg.Keybindings.ColorNext},
		{"ColorPrev", km.ColorPrev.Keys()[0], cfg.Keybindings.ColorPrev},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.binding != tc.want {
				t.Errorf("%s key = %q, want %q", tc.name, tc.binding, tc.want)
			}
		})
	}
}

func TestNewKeyMap_FixedBindings(t *testing.T) {
	cfg := config.DefaultConfig()
	km := NewKeyMap(cfg.Keybindings)

	// Confirm/Cancel are always fixed regardless of config
	confirmKeys := km.Confirm.Keys()
	cancelKeys := km.Cancel.Keys()

	if len(confirmKeys) == 0 {
		t.Error("Confirm binding has no keys")
	}
	if len(cancelKeys) == 0 {
		t.Error("Cancel binding has no keys")
	}
}

func TestNewKeyMap_AttachIncludesEnter(t *testing.T) {
	cfg := config.DefaultConfig()
	km := NewKeyMap(cfg.Keybindings)
	keys := km.Attach.Keys()
	found := false
	for _, k := range keys {
		if k == "enter" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Attach keys %v should include 'enter'", keys)
	}
}

func TestNewKeyMap_NavUpDownDefaultArrowKeys(t *testing.T) {
	cfg := config.DefaultConfig()
	km := NewKeyMap(cfg.Keybindings)

	upKeys := km.NavUp.Keys()
	if len(upKeys) == 0 || upKeys[0] != "up" {
		t.Errorf("NavUp default key = %v, want [up]", upKeys)
	}
	downKeys := km.NavDown.Keys()
	if len(downKeys) == 0 || downKeys[0] != "down" {
		t.Errorf("NavDown default key = %v, want [down]", downKeys)
	}
}

func TestKeyMap_ShortHelp_NonEmpty(t *testing.T) {
	cfg := config.DefaultConfig()
	km := NewKeyMap(cfg.Keybindings)
	bindings := km.ShortHelp()
	if len(bindings) == 0 {
		t.Fatal("ShortHelp() returned empty slice")
	}
	for i, b := range bindings {
		h := b.Help()
		if h.Key == "" && h.Desc == "" {
			t.Errorf("ShortHelp()[%d] has empty Key and Desc", i)
		}
	}
}

func TestKeyMap_FullHelp_CoversAllBindings(t *testing.T) {
	cfg := config.DefaultConfig()
	km := NewKeyMap(cfg.Keybindings)
	columns := km.FullHelp()
	if len(columns) < 2 {
		t.Fatalf("FullHelp() returned %d columns, want at least 2", len(columns))
	}

	// Collect all binding Keys present in FullHelp.
	seen := map[string]bool{}
	for _, col := range columns {
		for _, b := range col {
			for _, k := range b.Keys() {
				seen[k] = true
			}
		}
	}

	// Confirm and Cancel are context-specific overlay bindings (shown only in
	// confirm dialogs), not part of the general help overlay.
	excluded := map[string]bool{"Confirm": true, "Cancel": true}

	// Every other binding in the KeyMap should appear somewhere in FullHelp.
	v := reflect.ValueOf(km)
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Type() != reflect.TypeOf(key.Binding{}) {
			continue
		}
		fieldName := v.Type().Field(i).Name
		if excluded[fieldName] {
			continue
		}
		b, ok := f.Interface().(key.Binding)
		if !ok {
			continue
		}
		for _, k := range b.Keys() {
			if !seen[k] {
				t.Errorf("KeyMap.%s (key=%q) not present in FullHelp()", fieldName, k)
			}
		}
	}
}

func TestGridKeyMap_ShortHelp_IncludesNavAndGridActions(t *testing.T) {
	cfg := config.DefaultConfig()
	km := NewKeyMap(cfg.Keybindings)
	gk := NewGridKeyMap(km)
	bindings := gk.ShortHelp()
	if len(bindings) == 0 {
		t.Fatal("GridKeyMap.ShortHelp() returned empty slice")
	}
	// Verify at least navigate, reorder, attach, kill are present (non-empty desc).
	descSet := map[string]bool{}
	for _, b := range bindings {
		if d := b.Help().Desc; d != "" {
			descSet[d] = true
		}
	}
	for _, want := range []string{"navigate", "reorder", "attach", "kill"} {
		if !descSet[want] {
			t.Errorf("GridKeyMap.ShortHelp() missing binding with Desc=%q; got: %v", want, descSet)
		}
	}
}
