package config

import (
	"encoding/json"
	"testing"
)

// TestKeyBinding_UnmarshalAcceptsString verifies backwards compatibility:
// configs written when each binding was a single JSON string still load as a
// single-element KeyBinding slice.
func TestKeyBinding_UnmarshalAcceptsString(t *testing.T) {
	var kb KeyBinding
	if err := json.Unmarshal([]byte(`"a"`), &kb); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if len(kb) != 1 || kb[0] != "a" {
		t.Errorf("KeyBinding = %v, want [a]", kb)
	}
}

func TestKeyBinding_UnmarshalAcceptsArray(t *testing.T) {
	var kb KeyBinding
	if err := json.Unmarshal([]byte(`["up","k"]`), &kb); err != nil {
		t.Fatalf("unmarshal array: %v", err)
	}
	if len(kb) != 2 || kb[0] != "up" || kb[1] != "k" {
		t.Errorf("KeyBinding = %v, want [up k]", kb)
	}
}

// TestKeyBinding_UnmarshalDropsEmpty trims whitespace and drops empty entries
// so users can't end up with an unusable binding by accident.
func TestKeyBinding_UnmarshalDropsEmpty(t *testing.T) {
	var kb KeyBinding
	if err := json.Unmarshal([]byte(`[" ","k","",""]`), &kb); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(kb) != 1 || kb[0] != "k" {
		t.Errorf("KeyBinding = %v, want [k]", kb)
	}
}

// TestKeyBinding_MarshalsAsArray confirms the canonical on-disk form is the
// JSON array, not the legacy string. Old configs loaded as single-element
// slices will be re-serialized as `["a"]` after the next save.
func TestKeyBinding_MarshalsAsArray(t *testing.T) {
	kb := KeyBinding{"a", "f"}
	b, err := json.Marshal(kb)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := string(b), `["a","f"]`; got != want {
		t.Errorf("Marshal = %s, want %s", got, want)
	}
}

// TestKeyBinding_MarshalNilEmitsEmptyArray verifies that a nil KeyBinding
// serializes as `[]` instead of `null`, keeping the on-disk shape consistent
// regardless of how the slice was constructed in memory.
func TestKeyBinding_MarshalNilEmitsEmptyArray(t *testing.T) {
	var kb KeyBinding
	b, err := json.Marshal(kb)
	if err != nil {
		t.Fatalf("marshal nil: %v", err)
	}
	if got, want := string(b), `[]`; got != want {
		t.Errorf("Marshal(nil) = %s, want %s", got, want)
	}
}

func TestKeyBinding_HelpKey(t *testing.T) {
	cases := []struct {
		in   KeyBinding
		want string
	}{
		{KeyBinding{"up"}, "up"},
		{KeyBinding{"up", "k"}, "up/k"},
		{KeyBinding{"", "k"}, "k"},
		{nil, ""},
	}
	for _, tc := range cases {
		if got := tc.in.HelpKey(); got != tc.want {
			t.Errorf("HelpKey(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestMigrate_V4ToV5_FillsNewKeybindings ensures an upgrade from v4 populates
// the actions added in #112 (Detach, CursorUp/Down/Left/Right, etc.) from
// defaults rather than leaving them empty.
func TestMigrate_V4ToV5_FillsNewKeybindings(t *testing.T) {
	cfg := Config{SchemaVersion: 4}
	got := Migrate(cfg)
	if got.SchemaVersion != currentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, currentSchemaVersion)
	}
	if got.Keybindings.Detach.First() == "" {
		t.Error("Detach should be populated from defaults after v4→v5")
	}
	if got.Keybindings.CursorUp.First() == "" {
		t.Error("CursorUp should be populated from defaults after v4→v5")
	}
	if got.Keybindings.InputMode.First() == "" {
		t.Error("InputMode should be populated from defaults after v4→v5")
	}
}

// TestMigrate_V4ToV5_PreservesUserKeybindings ensures the v4→v5 fill does not
// overwrite a user's existing customizations on actions that already had a
// value.
func TestMigrate_V4ToV5_PreservesUserKeybindings(t *testing.T) {
	cfg := Config{
		SchemaVersion: 4,
		Keybindings: KeybindingsConfig{
			Attach: KeyBinding{"a"},
		},
	}
	got := Migrate(cfg)
	if got.Keybindings.Attach.First() != "a" {
		t.Errorf("Attach = %v, want [a] (user customization preserved)", got.Keybindings.Attach)
	}
}
