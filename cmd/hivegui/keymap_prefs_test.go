package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// A keymap.json that exists but cannot be read must be an error, not the
// defaults: a save after a silent fallback would overwrite the user's file.
func TestGetKeymapUnreadableIsError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIVE_STATE_DIR", dir)
	// A directory where the file should be: ReadFile fails with an error
	// that is not "does not exist", on every platform and as root too.
	if err := os.Mkdir(filepath.Join(dir, "keymap.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := (&App{}).GetKeymap(); err == nil {
		t.Fatal("an unreadable keymap.json must be an error")
	}
}

// SaveKeymap round-trips through GetKeymap: both halves, unbound ([])
// commands kept as empty lists, plugin overrides untouched, version set.
func TestSaveKeymapRoundTrips(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIVE_STATE_DIR", dir)
	a := &App{}
	in := Keymap{
		Mac:   map[string][]string{"new-session": {"Mod+Y"}, "worktrees": {}},
		Other: map[string][]string{"plugin:absent:go": {"Ctrl+Alt+G"}},
	}
	if err := a.SaveKeymap(in); err != nil {
		t.Fatalf("SaveKeymap: %v", err)
	}
	got, err := a.GetKeymap()
	if err != nil {
		t.Fatalf("GetKeymap: %v", err)
	}
	in.Version = 1
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("got %+v, want %+v", got, in)
	}
	if got.Mac["worktrees"] == nil {
		t.Fatal("an unbound command came back as nil, i.e. as its defaults")
	}
	// Atomic write: nothing but the file itself is left in the state dir.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "keymap.json" {
		t.Fatalf("state dir holds %v, want only keymap.json", entries)
	}
}

// A state dir that cannot be created (its parent is a regular file) fails
// the save and leaves nothing behind.
func TestSaveKeymapStateDirUncreatable(t *testing.T) {
	parent := t.TempDir()
	blocker := filepath.Join(parent, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HIVE_STATE_DIR", filepath.Join(blocker, "state"))
	if err := (&App{}).SaveKeymap(Keymap{}); err == nil {
		t.Fatal("SaveKeymap into an uncreatable state dir must fail")
	}
	assertOnlyEntries(t, parent, "file")
}

// A rename that fails (keymap.json is a directory) fails the save and
// removes the temp file it wrote.
func TestSaveKeymapRenameFails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIVE_STATE_DIR", dir)
	if err := os.Mkdir(filepath.Join(dir, "keymap.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	// Something inside, so the rename is refused on every platform.
	if err := os.WriteFile(filepath.Join(dir, "keymap.json", "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (&App{}).SaveKeymap(Keymap{Mac: map[string][]string{"new-session": {"Mod+Y"}}}); err == nil {
		t.Fatal("SaveKeymap over a directory must fail")
	}
	assertOnlyEntries(t, dir, "keymap.json")
}

func assertOnlyEntries(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s holds %v, want %v", dir, got, want)
	}
}

// Export writes the same bytes SaveKeymap does, so an exported file is a
// valid keymap.json and imports back as itself. An unbound command stays
// [] (null would re-import as a malformed entry).
func TestExportKeymapWritesFile(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HIVE_STATE_DIR", state)
	out := filepath.Join(t.TempDir(), "mine.json")
	k := Keymap{
		Mac:   map[string][]string{"new-session": {"Mod+Y"}, "worktrees": {}},
		Other: map[string][]string{"settings": {"Ctrl+Alt+S"}},
	}
	ok, err := exportKeymapWith(func() (string, error) { return out, nil }, k)
	if err != nil || !ok {
		t.Fatalf("exportKeymapWith = %v, %v", ok, err)
	}
	if err := (&App{}).SaveKeymap(k); err != nil {
		t.Fatal(err)
	}
	exported, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(filepath.Join(state, "keymap.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(exported) != string(saved) {
		t.Fatalf("export wrote\n%s\nSaveKeymap wrote\n%s", exported, saved)
	}
	if !strings.Contains(string(exported), `"worktrees": []`) {
		t.Fatalf("an unbound command must export as []:\n%s", exported)
	}
}

func TestExportKeymapCancelled(t *testing.T) {
	ok, err := exportKeymapWith(func() (string, error) { return "", nil }, Keymap{})
	if err != nil || ok {
		t.Fatalf("a cancelled export = %v, %v; want false, nil", ok, err)
	}
}

func TestExportKeymapDialogError(t *testing.T) {
	ok, err := exportKeymapWith(func() (string, error) { return "", errors.New("boom") }, Keymap{})
	if err == nil || ok {
		t.Fatalf("a failed dialog = %v, %v; want false, error", ok, err)
	}
}

// A write that fails (the target's directory does not exist) reports the
// error and claims nothing was exported.
func TestExportKeymapWriteFails(t *testing.T) {
	out := filepath.Join(t.TempDir(), "missing", "mine.json")
	ok, err := exportKeymapWith(func() (string, error) { return out, nil }, Keymap{})
	if err == nil || ok {
		t.Fatalf("a failed write = %v, %v; want false, error", ok, err)
	}
}

func TestPickKeymapFileDialogError(t *testing.T) {
	got, err := pickKeymapFileWith(func() (string, error) { return "", errors.New("boom") })
	if err == nil || got != "" {
		t.Fatalf("a failed dialog = %q, %v; want \"\", error", got, err)
	}
}

func TestPickKeymapFileMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "gone.json")
	if _, err := pickKeymapFileWith(pickFrom(p)); err == nil {
		t.Fatal("a file that cannot be opened must be an error")
	}
}

func TestPickKeymapFileAtLimit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "edge.json")
	if err := os.WriteFile(p, make([]byte, maxKeymapFile), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := pickKeymapFileWith(pickFrom(p))
	if err != nil || len(got) != maxKeymapFile {
		t.Fatalf("a file at the limit = %d bytes, %v; want %d, nil", len(got), err, maxKeymapFile)
	}
}

func pickFrom(path string) func() (string, error) {
	return func() (string, error) { return path, nil }
}

func TestPickKeymapFileReadsAndStripsBOM(t *testing.T) {
	p := filepath.Join(t.TempDir(), "k.json")
	if err := os.WriteFile(p, []byte("\ufeff"+`{"mac":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := pickKeymapFileWith(pickFrom(p))
	if err != nil || got != `{"mac":{}}` {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestPickKeymapFileTooLarge(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(p, make([]byte, maxKeymapFile+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := pickKeymapFileWith(pickFrom(p)); err == nil {
		t.Fatal("a file over the limit must be refused")
	}
}

func TestPickKeymapFileCancelled(t *testing.T) {
	got, err := pickKeymapFileWith(pickFrom(""))
	if err != nil || got != "" {
		t.Fatalf("a cancelled pick = %q, %v; want \"\", nil", got, err)
	}
}
