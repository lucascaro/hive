//go:build e2e

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/daemon"
)

// insideSession runs `hived <args…>` as an agent's shell would: HIVE_SOCKET
// is the narrowed events socket and HIVE_SESSION_ID the caller.
func insideSession(t *testing.T, d *spawnedDaemon, sessionID string, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	cmd := exec.Command(hivedBinary(t), args...)
	cmd.Env = append(os.Environ(),
		"HIVE_SESSION_ID="+sessionID,
		"HIVE_SOCKET="+daemon.EventSocketPath(d.sockPath),
	)
	var so, se strings.Builder
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		exit = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("hived %v: %v", args, err)
	}
	return so.String(), se.String(), exit
}

func TestE2E_SessionListShowsTheProjectsSessions(t *testing.T) {
	d := spawnDaemon(t)
	sender, target := orchSessions(t, d, false)
	out, errs, code := insideSession(t, d, sender, "session", "list")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	for _, want := range []string{sender, target, "boss", "worker", "claude", "idle"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
}

// The sibling is already idle, so wait must not block — in a session
// that was never granted anything (no grant for read-only verbs).
func TestE2E_WaitOnAnIdleSiblingReturnsImmediately(t *testing.T) {
	d := spawnDaemon(t)
	sender, _ := orchSessions(t, d, false)
	start := time.Now()
	out, errs, code := insideSession(t, d, sender, "wait", "worker")
	if code != 0 || strings.TrimSpace(out) != "idle" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want 0 and idle", code, out, errs)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("took %s, want immediate", time.Since(start))
	}
}

func TestE2E_WaitExitCodes(t *testing.T) {
	d := spawnDaemon(t)
	sender, _ := orchSessions(t, d, false)

	// Timeout: a cat session never starts working, so this waits out --timeout.
	_, errs, code := insideSession(t, d, sender, "wait", "worker", "--state", "working", "--timeout", "1s")
	if code != 1 || !strings.Contains(errs, "timed out") {
		t.Errorf("timeout: exit %d, stderr %q; want 1 and timed out", code, errs)
	}
	// Unknown session, and waiting on yourself, are argument errors.
	if _, errs, code = insideSession(t, d, sender, "wait", "ghost"); code != 2 {
		t.Errorf("unknown session: exit %d, stderr %q; want 2", code, errs)
	}
	if _, errs, code = insideSession(t, d, sender, "wait", "boss"); code != 2 {
		t.Errorf("self: exit %d, stderr %q; want 2", code, errs)
	}
}
