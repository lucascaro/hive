package registry

import "testing"

func TestStateDirHIVEStateDirOverride(t *testing.T) {
	t.Setenv("HIVE_STATE_DIR", "/tmp/hive-iso/state")
	if got := StateDir(); got != "/tmp/hive-iso/state" {
		t.Errorf("HIVE_STATE_DIR ignored: got %q", got)
	}
}

func TestStateDirDefaultsWithoutOverride(t *testing.T) {
	t.Setenv("HIVE_STATE_DIR", "")
	got := StateDir()
	if got == "" {
		t.Error("default StateDir returned empty string")
	}
	if got == "/tmp/hive-iso/state" {
		t.Errorf("default leaked test override: %q", got)
	}
}
