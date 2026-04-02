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
