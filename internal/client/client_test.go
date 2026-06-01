package client

import (
	"strings"
	"testing"
)

func TestVersionStringIsNonEmpty(t *testing.T) {
	if ClientName() == "" {
		t.Fatal("ClientName() must not be empty")
	}
}

func TestDial_NoDaemonGivesFriendlyError(t *testing.T) {
	_, err := Dial("/nonexistent/path/hived.sock")
	if err == nil {
		t.Fatal("expected error dialing a missing socket")
	}
	if got := err.Error(); !strings.Contains(got, "is Hive running") {
		t.Fatalf("error should hint the daemon is down, got: %q", got)
	}
}
