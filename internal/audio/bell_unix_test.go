//go:build !windows

package audio

import (
	"errors"
	"testing"
)

// TestUnixPlayers_ProbeOrder verifies the first player present on PATH is
// the one invoked, even when later entries are also available. Stubs
// lookPath + runCmd so no real audio tool is required.
func TestUnixPlayers_ProbeOrder(t *testing.T) {
	tests := []struct {
		name      string
		available map[string]bool // players that LookPath should succeed for
		wantBin   string          // player expected to be invoked, or "" for none
		wantErr   bool
	}{
		{"all available, afplay wins", map[string]bool{"afplay": true, "paplay": true, "aplay": true}, "afplay", false},
		{"no afplay, paplay wins", map[string]bool{"paplay": true, "aplay": true}, "paplay", false},
		{"only aplay", map[string]bool{"aplay": true}, "aplay", false},
		{"none available", map[string]bool{}, "", true},
	}

	origLook, origRun := lookPath, runCmd
	t.Cleanup(func() { lookPath, runCmd = origLook, origRun })

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lookPath = func(name string) (string, error) {
				if tc.available[name] {
					return "/stub/" + name, nil
				}
				return "", errors.New("not found")
			}
			var invoked string
			runCmd = func(bin string, args ...string) error {
				invoked = bin
				return nil
			}

			err := playWAVReal("/tmp/irrelevant.wav")
			if tc.wantErr {
				if err == nil {
					t.Error("expected error when no player is available")
				}
				if invoked != "" {
					t.Errorf("runCmd called with %q, want no call", invoked)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if invoked != "/stub/"+tc.wantBin {
				t.Errorf("invoked = %q, want %q", invoked, "/stub/"+tc.wantBin)
			}
		})
	}
}
