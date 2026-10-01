package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestGetKeymapMissingIsEmpty(t *testing.T) {
	t.Setenv("HIVE_STATE_DIR", t.TempDir())
	k, err := (&App{}).GetKeymap()
	if err != nil {
		t.Fatalf("GetKeymap: %v", err)
	}
	if !reflect.DeepEqual(k, Keymap{}) {
		t.Fatalf("got %+v, want the empty keymap", k)
	}
}

func writeKeymap(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HIVE_STATE_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "keymap.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestGetKeymapReadsFile(t *testing.T) {
	writeKeymap(t, `{"version":1,"mac":{"new-session":["Mod+Y"],"worktrees":[]},"other":{"settings":["Ctrl+Alt+S"]}}`)
	k, err := (&App{}).GetKeymap()
	if err != nil {
		t.Fatalf("GetKeymap: %v", err)
	}
	want := Keymap{
		Version: 1,
		Mac:     map[string][]string{"new-session": {"Mod+Y"}, "worktrees": {}},
		Other:   map[string][]string{"settings": {"Ctrl+Alt+S"}},
	}
	if !reflect.DeepEqual(k, want) {
		t.Fatalf("got %+v, want %+v", k, want)
	}
	// [] unbinds a command, so it must survive as an empty list, not nil.
	if k.Mac["worktrees"] == nil {
		t.Fatal("an empty chord list decoded as nil")
	}
}

func TestGetKeymapCorruptIsError(t *testing.T) {
	writeKeymap(t, `{"mac":`)
	if _, err := (&App{}).GetKeymap(); err == nil {
		t.Fatal("a keymap.json that does not parse must be an error")
	}
}

func TestGetKeymapStripsBOM(t *testing.T) {
	writeKeymap(t, "\ufeff"+`{"mac":{"new-session":["Mod+Y"]}}`)
	k, err := (&App{}).GetKeymap()
	if err != nil {
		t.Fatalf("GetKeymap: %v", err)
	}
	if got := k.Mac["new-session"]; !reflect.DeepEqual(got, []string{"Mod+Y"}) {
		t.Fatalf("got %v", got)
	}
}
