package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain redirects all config/state I/O to a temporary directory for the
// entire test binary. This prevents any test — even one that forgets to call
// setHomePersist — from reading or writing the user's real ~/.config/hive.
//
// Individual tests can still call setHomePersist(t, t.TempDir()) to get their
// own isolated directory; that just narrows the scope further.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "hive-test-*")
	if err != nil {
		panic("hive tests: cannot create temp dir: " + err.Error())
	}
	defer os.RemoveAll(tmp)

	configDir := filepath.Join(tmp, ".config", "hive")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		panic("hive tests: cannot create config dir: " + err.Error())
	}

	os.Setenv("HIVE_CONFIG_DIR", configDir)
	os.Setenv("HOME", tmp)

	os.Exit(m.Run())
}
