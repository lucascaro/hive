package daemon

import "testing"

func TestSocketPathHIVESocketOverride(t *testing.T) {
	t.Setenv("HIVE_SOCKET", "/tmp/hive-iso/test.sock")
	if got := SocketPath(); got != "/tmp/hive-iso/test.sock" {
		t.Errorf("HIVE_SOCKET ignored: got %q", got)
	}
}

func TestSocketPathDefaultsWithoutOverride(t *testing.T) {
	t.Setenv("HIVE_SOCKET", "")
	got := SocketPath()
	if got == "" {
		t.Error("default SocketPath returned empty string")
	}
	if got == "/tmp/hive-iso/test.sock" {
		t.Errorf("default leaked test override: %q", got)
	}
}
