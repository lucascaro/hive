package muxtmux

import (
	"fmt"
	"os"
	"regexp"
	"testing"
)

func TestNewInstanceName_Format(t *testing.T) {
	name := newInstanceName(12345)
	matched, _ := regexp.MatchString(`^hive-sessions-12345-[0-9a-f]{4}$`, name)
	if !matched {
		t.Errorf("newInstanceName(12345) = %q; want hive-sessions-12345-<4 hex chars>", name)
	}
}

func TestNewInstanceName_Unique(t *testing.T) {
	// Two calls with the same pid should produce different names with very
	// high probability (collision is 1/65536). This guards against a
	// regression where the random suffix becomes deterministic.
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		name := newInstanceName(42)
		seen[name] = true
	}
	if len(seen) < 50 {
		t.Errorf("newInstanceName produced only %d unique names over 100 calls", len(seen))
	}
}

func TestParseInstancePID(t *testing.T) {
	tests := []struct {
		name    string
		wantPID int
		wantOK  bool
	}{
		{"hive-sessions-12345-abcd", 12345, true},
		{"hive-sessions-1-0000", 1, true},
		{"hive-sessions-999999-ffff", 999999, true},
		// Non-matches:
		{"hive-sessions", 0, false},                 // canonical
		{"hive-sessions-abc-1234", 0, false},        // non-numeric pid
		{"hive-sessions-123-xyz1", 0, false},        // non-hex suffix
		{"hive-sessions-123-abcde", 0, false},       // 5 hex chars, not 4
		{"hive-sessions-123-abc", 0, false},         // 3 hex chars
		{"hive-old-123-abcd", 0, false},             // wrong prefix
		{"hive-sessions-123-abcd-extra", 0, false},  // trailing garbage
		{"", 0, false},
	}
	for _, tc := range tests {
		pid, ok := parseInstancePID(tc.name)
		if ok != tc.wantOK || pid != tc.wantPID {
			t.Errorf("parseInstancePID(%q) = (%d, %v); want (%d, %v)",
				tc.name, pid, ok, tc.wantPID, tc.wantOK)
		}
	}
}

func TestPidAlive_Self(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Error("pidAlive(self) = false; want true")
	}
}

func TestPidAlive_Dead(t *testing.T) {
	// PID 1 is always alive on Unix (init/launchd). Use an impossibly high
	// pid instead to simulate "dead".
	if pidAlive(2_000_000_000) {
		t.Error("pidAlive(2e9) = true; want false")
	}
}

func TestPidAlive_Zero(t *testing.T) {
	if pidAlive(0) {
		t.Error("pidAlive(0) = true; want false")
	}
	if pidAlive(-1) {
		t.Error("pidAlive(-1) = true; want false")
	}
}

func TestGroupedSessionPattern_OnlyMatchesGrouped(t *testing.T) {
	// Canonical session must NOT match — otherwise sweep would kill it.
	if groupedSessionPattern.MatchString(CanonicalSession) {
		t.Errorf("groupedSessionPattern matched canonical %q", CanonicalSession)
	}
	// A realistic grouped name must match.
	sample := fmt.Sprintf("hive-sessions-%d-abcd", os.Getpid())
	if !groupedSessionPattern.MatchString(sample) {
		t.Errorf("groupedSessionPattern did not match grouped %q", sample)
	}
}

func TestIsGroupedSession(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// Positive matches:
		{"hive-sessions-12345-abcd", true},
		{"hive-sessions-1-0000", true},
		{"hive-sessions-999999-ffff", true},
		{fmt.Sprintf("hive-sessions-%d-a1b2", os.Getpid()), true},
		// Negative matches:
		{"hive-sessions", false},                // canonical
		{"hive-sessions-abc-1234", false},       // non-numeric pid
		{"hive-sessions-123-xyz1", false},       // non-hex suffix
		{"hive-sessions-123-abcde", false},      // 5 hex chars, not 4
		{"hive-sessions-123-abc", false},         // 3 hex chars
		{"hive-old-123-abcd", false},            // wrong prefix
		{"hive-sessions-123-abcd-extra", false}, // trailing garbage
		{"", false},
		{"not-hive", false},
	}
	for _, tc := range tests {
		if got := IsGroupedSession(tc.name); got != tc.want {
			t.Errorf("IsGroupedSession(%q) = %v; want %v", tc.name, got, tc.want)
		}
	}
}
