package session

import (
	"strings"
	"testing"
)

// envValue returns the value exec would resolve for key: last assignment
// wins, matching how os/exec dedups a Cmd.Env with repeated names.
func envValue(env []string, key string) (string, bool) {
	value, found := "", false
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			value, found = strings.TrimPrefix(kv, key+"="), true
		}
	}
	return value, found
}

func TestSessionEnvForcesTERM(t *testing.T) {
	got, ok := envValue(sessionEnv([]string{"TERM=dumb", "PATH=/usr/bin"}), "TERM")
	if !ok || got != "xterm-256color" {
		t.Errorf("TERM = %q (present=%v), want %q", got, ok, "xterm-256color")
	}
}

func TestSessionEnvDropsNoColor(t *testing.T) {
	// NO_COLOR inherited from whatever shell happened to launch the GUI
	// must not reach the session. A tile is a colour-capable xterm.js
	// terminal, so the daemon's ambient value says nothing about it, and
	// every agent spawned into one renders monochrome while it is set.
	if got, ok := envValue(sessionEnv([]string{"NO_COLOR=1", "PATH=/usr/bin"}), "NO_COLOR"); ok {
		t.Errorf("NO_COLOR = %q, want it dropped from the session environment", got)
	}
}

func TestSessionEnvKeepsUnrelatedVars(t *testing.T) {
	got, ok := envValue(sessionEnv([]string{"PATH=/usr/bin", "HOME=/home/q"}), "HOME")
	if !ok || got != "/home/q" {
		t.Errorf("HOME = %q (present=%v), want %q", got, ok, "/home/q")
	}
}
