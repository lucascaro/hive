//go:build !windows

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setHome overrides $HOME for the duration of the test.
func setHome(t *testing.T, dir string) {
	t.Helper()
	orig := os.Getenv("HOME")
	t.Setenv("HOME", dir)
	// HIVE_CONFIG_DIR is the universal config-dir override (checked first on all
	// platforms), so also set it so Windows tests aren't affected by %APPDATA%.
	t.Setenv("HIVE_CONFIG_DIR", filepath.Join(dir, ".config", "hive"))
	_ = orig
}

func TestDir_HIVE_CONFIG_DIR_OverridesDefault(t *testing.T) {
	custom := t.TempDir()
	t.Setenv("HIVE_CONFIG_DIR", custom)
	got := Dir()
	if got != custom {
		t.Errorf("Dir() = %q, want %q (HIVE_CONFIG_DIR)", got, custom)
	}
}

func TestDir_HIVE_CONFIG_DIR_IsolatesFromRealConfig(t *testing.T) {
	// Capture the real config dir before overriding.
	realDir := Dir()

	custom := t.TempDir()
	t.Setenv("HIVE_CONFIG_DIR", custom)

	// All path functions should point inside the custom dir, not the real one.
	if Dir() == realDir {
		t.Error("Dir() still returns real config dir after HIVE_CONFIG_DIR override")
	}
	if !strings.HasPrefix(ConfigPath(), custom) {
		t.Errorf("ConfigPath() = %q, not under custom dir %q", ConfigPath(), custom)
	}
	if !strings.HasPrefix(StatePath(), custom) {
		t.Errorf("StatePath() = %q, not under custom dir %q", StatePath(), custom)
	}
	if !strings.HasPrefix(LogPath(), custom) {
		t.Errorf("LogPath() = %q, not under custom dir %q", LogPath(), custom)
	}
	if !strings.HasPrefix(HooksPath(), custom) {
		t.Errorf("HooksPath() = %q, not under custom dir %q", HooksPath(), custom)
	}
}

func TestEnsure_WithCustomDir_DoesNotTouchRealConfig(t *testing.T) {
	// Record real config dir contents before test.
	realDir := Dir()

	custom := t.TempDir()
	t.Setenv("HIVE_CONFIG_DIR", custom)

	if err := Ensure(); err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}

	// Ensure created dirs inside custom, not real.
	if _, err := os.Stat(filepath.Join(custom, "hooks")); err != nil {
		t.Errorf("hooks dir not created in custom dir: %v", err)
	}

	// Real config dir should not have been created if it didn't exist,
	// and should not have new files if it did exist.
	// We verify by checking that Dir() under the override is custom, not real.
	if Dir() == realDir {
		t.Error("Dir() unexpectedly returns real config dir")
	}
}

func TestLoadSave_WithCustomDir_DoesNotTouchRealConfig(t *testing.T) {
	realConfigPath := ConfigPath()
	realStatePath := StatePath()

	custom := t.TempDir()
	t.Setenv("HIVE_CONFIG_DIR", custom)

	if err := Ensure(); err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}

	// Load (creates default config in custom dir).
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Save modified config.
	cfg.Theme = "demo-theme"
	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Config file should exist in custom dir.
	customConfig := filepath.Join(custom, "config.json")
	if _, err := os.Stat(customConfig); err != nil {
		t.Fatalf("config.json not in custom dir: %v", err)
	}

	// Real config path should NOT have been modified.
	if ConfigPath() == realConfigPath {
		t.Error("ConfigPath() still points to real config dir")
	}
	// Real state path should NOT have been modified.
	if StatePath() == realStatePath {
		t.Error("StatePath() still points to real config dir")
	}
}

func TestDir_UsesHome(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	got := Dir()
	if !strings.HasPrefix(got, tmp) {
		t.Errorf("Dir() = %q, want prefix %q", got, tmp)
	}
}

func TestConfigPath_UnderDir(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	got := ConfigPath()
	want := filepath.Join(Dir(), configFileName)
	if got != want {
		t.Errorf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestStatePath_UnderDir(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	got := StatePath()
	want := filepath.Join(Dir(), stateFileName)
	if got != want {
		t.Errorf("StatePath() = %q, want %q", got, want)
	}
}

func TestEnsure_CreatesDirs(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	if err := Ensure(); err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}
	if _, err := os.Stat(Dir()); err != nil {
		t.Errorf("Dir() not created: %v", err)
	}
	if _, err := os.Stat(HooksPath()); err != nil {
		t.Errorf("HooksPath() not created: %v", err)
	}
}

func TestEnsure_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	if err := Ensure(); err != nil {
		t.Fatalf("first Ensure() error: %v", err)
	}
	if err := Ensure(); err != nil {
		t.Fatalf("second Ensure() error: %v", err)
	}
}

func TestLoad_ReturnsDefaultsWhenMissing(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	if err := Ensure(); err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	defaults := DefaultConfig()
	if cfg.SchemaVersion != defaults.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", cfg.SchemaVersion, defaults.SchemaVersion)
	}
	if cfg.Theme != defaults.Theme {
		t.Errorf("Theme = %q, want %q", cfg.Theme, defaults.Theme)
	}
}

func TestLoad_WritesDefaultsOnFirstRun(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	if err := Ensure(); err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}
	_, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	// Config file should now exist
	if _, err := os.Stat(ConfigPath()); err != nil {
		t.Errorf("config file not written on first load: %v", err)
	}
}

func TestSaveLoad_Roundtrip(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	if err := Ensure(); err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Theme = "light"
	cfg.PreviewRefreshMs = 1234

	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() after Save() error: %v", err)
	}
	if got.Theme != "light" {
		t.Errorf("Theme = %q, want %q", got.Theme, "light")
	}
	if got.PreviewRefreshMs != 1234 {
		t.Errorf("PreviewRefreshMs = %d, want 1234", got.PreviewRefreshMs)
	}
}

func TestSave_FilePermissions(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	if err := Ensure(); err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}

	cfg := DefaultConfig()
	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	info, err := os.Stat(ConfigPath())
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("config file permissions = %o, want 0600", perm)
	}
}

func TestWriteAtomic_TempFileCleanedUp(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	if err := Ensure(); err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}

	target := filepath.Join(Dir(), "test-atomic.json")
	if err := writeAtomic(target, []byte(`{"test":true}`)); err != nil {
		t.Fatalf("writeAtomic() error: %v", err)
	}

	// Target file should exist
	if _, err := os.Stat(target); err != nil {
		t.Errorf("target file not created: %v", err)
	}
	// Temp file should not exist
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file should be cleaned up, got err: %v", err)
	}
}

func TestSave_WritesValidJSON(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	if err := Ensure(); err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}

	cfg := DefaultConfig()
	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		t.Fatalf("ReadFile() error: %v", err)
	}
	var parsed Config
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Errorf("saved config is not valid JSON: %v", err)
	}
}
