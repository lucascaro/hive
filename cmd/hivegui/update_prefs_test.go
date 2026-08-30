package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateStateDir points registry.StateDir() at a temp dir for the
// lifetime of the test, so nothing here can read or write the user's
// real Hive state.
func isolateStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HIVE_STATE_DIR", dir)
	return dir
}

func TestUpdateSettingsRoundTrip(t *testing.T) {
	isolateStateDir(t)
	repo := fakeHiveCheckout(t)

	if err := saveUpdateSettings(UpdateSettings{Channel: ChannelLatest, SourceRepo: repo}); err != nil {
		t.Fatalf("saveUpdateSettings: %v", err)
	}
	got, err := loadUpdateSettings()
	if err != nil {
		t.Fatalf("loadUpdateSettings: %v", err)
	}
	if got.Channel != ChannelLatest {
		t.Errorf("Channel = %q, want %q", got.Channel, ChannelLatest)
	}
	if got.SourceRepo != repo {
		t.Errorf("SourceRepo = %q, want %q", got.SourceRepo, repo)
	}
}

func TestUpdateSettingsDefaultsChannel(t *testing.T) {
	dir := isolateStateDir(t)

	// No file at all.
	got, err := loadUpdateSettings()
	if err != nil {
		t.Fatalf("loadUpdateSettings on a fresh state dir: %v", err)
	}
	if got.Channel != ChannelRelease {
		t.Errorf("Channel = %q on a fresh install, want %q", got.Channel, ChannelRelease)
	}

	// A file with a channel we don't recognise must not select the
	// latest channel by accident — that one shells out to git and
	// build.sh.
	writeFile(t, filepath.Join(dir, "update.json"), `{"channel":"nightly"}`)
	got, err = loadUpdateSettings()
	if err != nil {
		t.Fatalf("loadUpdateSettings: %v", err)
	}
	if got.Channel != ChannelRelease {
		t.Errorf("Channel = %q for an unknown channel, want %q", got.Channel, ChannelRelease)
	}
}

func TestUpdateSettingsCorruptFileIsError(t *testing.T) {
	dir := isolateStateDir(t)
	path := filepath.Join(dir, "update.json")
	writeFile(t, path, "{not json")

	if _, err := loadUpdateSettings(); err == nil {
		t.Fatal("loadUpdateSettings = nil error on a corrupt update.json, want it surfaced")
	}
	// And the file must still be there for the user to fix by hand.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("update.json was removed after a failed load: %v", err)
	}
}

func TestSaveUpdateSettingsRefusesLatestWithoutSourceRepo(t *testing.T) {
	isolateStateDir(t)
	// Point auto-detect somewhere with no checkout above it.
	stub := t.TempDir()
	executablePath = func() (string, error) { return filepath.Join(stub, "hivegui"), nil }
	t.Cleanup(func() { executablePath = os.Executable })

	err := (&App{}).SaveUpdateSettings(UpdateSettings{Channel: ChannelLatest})
	if err == nil {
		t.Fatal("SaveUpdateSettings(latest) = nil error with no resolvable checkout, want a refusal")
	}
	if !strings.Contains(err.Error(), "no hive checkout") {
		t.Errorf("error = %q, want it to name the missing checkout", err)
	}
	if _, statErr := os.Stat(updateSettingsPath()); statErr == nil {
		t.Error("update.json was written despite the refusal")
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
