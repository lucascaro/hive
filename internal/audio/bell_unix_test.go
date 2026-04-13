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

			err := playWAVReal("/tmp/irrelevant.wav", 100)
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

func TestVolumeArgs_Afplay(t *testing.T) {
	args := volumeArgs("afplay", "/tmp/test.wav", 75)
	// Expect: ["-v", "0.7500", "/tmp/test.wav"]
	if len(args) != 3 {
		t.Fatalf("got %d args, want 3: %v", len(args), args)
	}
	if args[0] != "-v" {
		t.Errorf("args[0] = %q, want %q", args[0], "-v")
	}
	if args[1] != "0.7500" {
		t.Errorf("args[1] = %q, want %q", args[1], "0.7500")
	}
	if args[2] != "/tmp/test.wav" {
		t.Errorf("args[2] = %q, want %q", args[2], "/tmp/test.wav")
	}
}

func TestVolumeArgs_Paplay(t *testing.T) {
	args := volumeArgs("paplay", "/tmp/test.wav", 75)
	// Expect: ["--volume=49152", "/tmp/test.wav"] (65536 * 75 / 100 = 49152)
	if len(args) != 2 {
		t.Fatalf("got %d args, want 2: %v", len(args), args)
	}
	if args[0] != "--volume=49152" {
		t.Errorf("args[0] = %q, want %q", args[0], "--volume=49152")
	}
	if args[1] != "/tmp/test.wav" {
		t.Errorf("args[1] = %q, want %q", args[1], "/tmp/test.wav")
	}
}

func TestVolumeArgs_Aplay(t *testing.T) {
	args := volumeArgs("aplay", "/tmp/test.wav", 50)
	// aplay has no volume flag — just the path.
	if len(args) != 1 || args[0] != "/tmp/test.wav" {
		t.Errorf("got args %v, want [/tmp/test.wav]", args)
	}
}

func TestVolumeArgs_AfplayFullVolume(t *testing.T) {
	args := volumeArgs("afplay", "/tmp/test.wav", 100)
	if args[1] != "1.0000" {
		t.Errorf("args[1] = %q, want %q", args[1], "1.0000")
	}
}
