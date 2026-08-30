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

// Switching channels must discard whatever the previous channel's check
// produced. Without this, saving release→latest and pressing Update
// with no re-check in between installs a GitHub release on the latest
// channel — the wrong artifact entirely.
func TestSaveUpdateSettingsForgetsStateOnChannelChange(t *testing.T) {
	isolateStateDir(t)
	repo := fakeHiveCheckout(t)
	a := &App{}
	a.rememberCheck(UpdateInfo{
		Available: true, Latest: "2.5.0", Channel: ChannelRelease, Stage: StageAvailable,
	})

	if err := a.SaveUpdateSettings(UpdateSettings{Channel: ChannelLatest, SourceRepo: repo}); err != nil {
		t.Fatalf("SaveUpdateSettings: %v", err)
	}
	got := a.UpdateStatus()
	if got.Available || got.Latest != "" {
		t.Errorf("UpdateStatus = %+v after a channel change, want it cleared", got)
	}
	if err := a.StartUpdate(); err == nil {
		t.Error("StartUpdate = nil error right after a channel change, want it to require a fresh check")
	}
}

// Re-saving the same channel is not a change and must not throw away a
// check the user is about to act on.
func TestSaveUpdateSettingsKeepsStateWhenChannelIsUnchanged(t *testing.T) {
	isolateStateDir(t)
	a := &App{}
	a.rememberCheck(UpdateInfo{
		Available: true, Latest: "2.5.0", Channel: ChannelRelease, Stage: StageAvailable,
	})
	if err := a.SaveUpdateSettings(UpdateSettings{Channel: ChannelRelease}); err != nil {
		t.Fatalf("SaveUpdateSettings: %v", err)
	}
	if got := a.UpdateStatus(); !got.Available || got.Latest != "2.5.0" {
		t.Errorf("UpdateStatus = %+v after re-saving the same channel, want it preserved", got)
	}
}

// The channel is not the only setting a staged bundle depends on. A
// latest-channel bundle is built from a specific checkout, so pointing
// Hive at a different one has to invalidate it too — otherwise Restart
// installs a build from the repo the user just moved away from.
func TestSaveUpdateSettingsForgetsStateOnSourceRepoChange(t *testing.T) {
	isolateStateDir(t)
	first := fakeHiveCheckout(t)
	second := fakeHiveCheckout(t)
	a := &App{}
	if err := a.SaveUpdateSettings(UpdateSettings{Channel: ChannelLatest, SourceRepo: first}); err != nil {
		t.Fatalf("SaveUpdateSettings: %v", err)
	}
	a.rememberCheck(UpdateInfo{
		Available: true, Latest: "abc1234", Channel: ChannelLatest, Stage: StageAvailable,
	})

	// Same channel, different checkout.
	if err := a.SaveUpdateSettings(UpdateSettings{Channel: ChannelLatest, SourceRepo: second}); err != nil {
		t.Fatalf("SaveUpdateSettings: %v", err)
	}
	if got := a.UpdateStatus(); got.Available || got.Latest != "" {
		t.Errorf("UpdateStatus = %+v after a source-repo change, want it cleared", got)
	}
	if err := a.ApplyUpdateAndRestart(); err == nil {
		t.Error("ApplyUpdateAndRestart = nil error after a source-repo change, want a refusal")
	}
}

// Re-saving byte-identical settings is not a change; it must not throw
// away a check the user is about to act on.
func TestSaveUpdateSettingsKeepsStateWhenNothingChanged(t *testing.T) {
	isolateStateDir(t)
	repo := fakeHiveCheckout(t)
	a := &App{}
	if err := a.SaveUpdateSettings(UpdateSettings{Channel: ChannelLatest, SourceRepo: repo}); err != nil {
		t.Fatal(err)
	}
	a.rememberCheck(UpdateInfo{
		Available: true, Latest: "abc1234", Channel: ChannelLatest, Stage: StageAvailable,
	})
	if err := a.SaveUpdateSettings(UpdateSettings{Channel: ChannelLatest, SourceRepo: repo}); err != nil {
		t.Fatal(err)
	}
	if got := a.UpdateStatus(); !got.Available || got.Latest != "abc1234" {
		t.Errorf("UpdateStatus = %+v after an identical re-save, want it preserved", got)
	}
}

// A corrupt update.json cannot tell us what the previous settings were,
// so a save must invalidate rather than compare against the defaults it
// stood in for.
func TestSaveUpdateSettingsForgetsStateWhenPriorFileIsUnreadable(t *testing.T) {
	dir := isolateStateDir(t)
	a := &App{}
	a.rememberCheck(UpdateInfo{
		Available: true, Latest: "2.5.0", Channel: ChannelRelease, Stage: StageAvailable,
	})
	writeFile(t, filepath.Join(dir, "update.json"), "{not json")

	// Saving settings that happen to equal the defaults the corrupt read
	// returned — the case a naive comparison would treat as "unchanged".
	if err := a.SaveUpdateSettings(UpdateSettings{Channel: ChannelRelease}); err != nil {
		t.Fatalf("SaveUpdateSettings: %v", err)
	}
	if got := a.UpdateStatus(); got.Available {
		t.Errorf("UpdateStatus = %+v after saving over a corrupt file, want it cleared", got)
	}
}
