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
		{"NewProject", km.NewProject.Keys()[0], cfg.Keybindings.NewProject.First()},
		{"NewSession", km.NewSession.Keys()[0], cfg.Keybindings.NewSession.First()},
		{"NewTeam", km.NewTeam.Keys()[0], cfg.Keybindings.NewTeam.First()},
		{"KillSession", km.KillSession.Keys()[0], cfg.Keybindings.KillSession.First()},
		{"KillTeam", km.KillTeam.Keys()[0], cfg.Keybindings.KillTeam.First()},
		{"Rename", km.Rename.Keys()[0], cfg.Keybindings.Rename.First()},
		{"Filter", km.Filter.Keys()[0], cfg.Keybindings.Filter.First()},
		{"Help", km.Help.Keys()[0], cfg.Keybindings.Help.First()},
		{"Quit", km.Quit.Keys()[0], cfg.Keybindings.Quit.First()},
		{"QuitKill", km.QuitKill.Keys()[0], cfg.Keybindings.QuitKill.First()},
		{"ColorNext", km.ColorNext.Keys()[0], cfg.Keybindings.ColorNext.First()},
		{"ColorPrev", km.ColorPrev.Keys()[0], cfg.Keybindings.ColorPrev.First()},
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

// TestUniqueKeys verifies the helper deduplicates while preserving order.
func TestUniqueKeys(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"up"}, []string{"up"}},
		{[]string{"up", "up"}, []string{"up"}},
		{[]string{"k", "up"}, []string{"k", "up"}},
		{[]string{"up", "k"}, []string{"up", "k"}},
		{[]string{"a", "b", "a"}, []string{"a", "b"}},
		{[]string{"", "up"}, []string{"up"}},        // empty primary skipped
		{[]string{"", ""}, []string{}},               // all-empty → no keys
		{[]string{}, []string{}},                     // no input
	}
	for _, tc := range cases {
		got := uniqueKeys(tc.in...)
		if len(got) != len(tc.want) {
			t.Errorf("uniqueKeys(%v) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("uniqueKeys(%v)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

// TestNewKeyMap_NavUpAlwaysIncludesArrowKey verifies that "up"/"down" are
// always present in NavUp/NavDown bindings, even when config sets vim keys.
func TestNewKeyMap_NavUpAlwaysIncludesArrowKey(t *testing.T) {
	// Simulate an old config with vim-style nav keys.
	kb := config.DefaultConfig().Keybindings
	kb.NavUp = config.KeyBinding{"k"}
	kb.NavDown = config.KeyBinding{"j"}
	km := NewKeyMap(kb)

	hasKey := func(b key.Binding, target string) bool {
		for _, k := range b.Keys() {
			if k == target {
				return true
			}
		}
		return false
	}

	if !hasKey(km.NavUp, "up") {
		t.Errorf("NavUp keys %v must always include 'up' (arrow alias)", km.NavUp.Keys())
	}
	if !hasKey(km.NavUp, "k") {
		t.Errorf("NavUp keys %v must include configured key 'k'", km.NavUp.Keys())
	}
	if !hasKey(km.NavDown, "down") {
		t.Errorf("NavDown keys %v must always include 'down' (arrow alias)", km.NavDown.Keys())
	}
	if !hasKey(km.NavDown, "j") {
		t.Errorf("NavDown keys %v must include configured key 'j'", km.NavDown.Keys())
	}
}

// TestNewKeyMap_CollapseExpandVimAliases verifies h/l are included as
// vim-style aliases for CollapseItem/ExpandItem.
func TestNewKeyMap_CollapseExpandVimAliases(t *testing.T) {
	km := NewKeyMap(config.DefaultConfig().Keybindings)

	hasKey := func(b key.Binding, target string) bool {
		for _, k := range b.Keys() {
			if k == target {
				return true
			}
		}
		return false
	}
	if !hasKey(km.CollapseItem, "left") {
		t.Error("CollapseItem must include 'left' (arrow key)")
	}
	if !hasKey(km.CollapseItem, "h") {
		t.Error("CollapseItem must include 'h' (vim alias)")
	}
	if !hasKey(km.ExpandItem, "right") {
		t.Error("ExpandItem must include 'right' (arrow key)")
	}
	if !hasKey(km.ExpandItem, "l") {
		t.Error("ExpandItem must include 'l' (vim alias)")
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
