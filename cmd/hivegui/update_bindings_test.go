package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The thin Wails bindings. They are wrappers, but they are also the
// entire contract the Settings modal codes against — a wrapper that
// returns the wrong shape is a broken settings screen, and nothing else
// would catch it before a hand-test.

func TestGetUpdateSettingsReturnsDefaultsOnAFreshInstall(t *testing.T) {
	isolateStateDir(t)
	got, err := (&App{}).GetUpdateSettings()
	if err != nil {
		t.Fatalf("GetUpdateSettings: %v", err)
	}
	if got.Channel != ChannelRelease {
		t.Errorf("Channel = %q, want %q", got.Channel, ChannelRelease)
	}
	if got.SourceRepo != "" {
		t.Errorf("SourceRepo = %q, want empty", got.SourceRepo)
	}
}

func TestGetUpdateSettingsSurfacesACorruptFile(t *testing.T) {
	dir := isolateStateDir(t)
	writeFile(t, filepath.Join(dir, "update.json"), "{not json")
	if _, err := (&App{}).GetUpdateSettings(); err == nil {
		t.Fatal("GetUpdateSettings = nil error on a corrupt file, want it surfaced to the modal")
	}
}

func TestSourceRepoStatusForReportsDetectedVsConfigured(t *testing.T) {
	repo := fakeHiveCheckout(t)
	deep := filepath.Join(repo, "cmd", "hivegui")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	executablePath = func() (string, error) { return filepath.Join(deep, "hivegui"), nil }
	t.Cleanup(func() { executablePath = os.Executable })

	a := &App{}

	// Empty input: found by walking up, so the UI should say "detected".
	auto := a.SourceRepoStatusFor("")
	if auto.Error != "" {
		t.Fatalf("SourceRepoStatusFor(\"\") returned error %q", auto.Error)
	}
	if !auto.Detected {
		t.Error("Detected = false for an auto-resolved repo, want true")
	}
	if auto.Path != repo {
		t.Errorf("Path = %q, want %q", auto.Path, repo)
	}

	// Explicit input: same path, but the user chose it — the UI must not
	// claim it was detected.
	explicit := a.SourceRepoStatusFor(repo)
	if explicit.Detected {
		t.Error("Detected = true for an explicitly configured repo, want false")
	}
	if explicit.Path != repo {
		t.Errorf("Path = %q, want %q", explicit.Path, repo)
	}
}

// The modal renders Error verbatim, so it has to be populated rather
// than left as an empty status the user cannot interpret.
func TestSourceRepoStatusForReportsWhyItFailed(t *testing.T) {
	stub := t.TempDir()
	executablePath = func() (string, error) { return filepath.Join(stub, "hivegui"), nil }
	t.Cleanup(func() { executablePath = os.Executable })

	st := (&App{}).SourceRepoStatusFor("")
	if st.Error == "" {
		t.Fatal("Error is empty when no checkout resolves, want an explanation")
	}
	if st.Path != "" {
		t.Errorf("Path = %q alongside an error, want empty", st.Path)
	}
	if !strings.Contains(st.Error, "Settings") {
		t.Errorf("Error = %q, want it to tell the user where to fix this", st.Error)
	}
}
