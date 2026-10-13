//go:build e2e

package main

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// notInASession keeps the test hermetic when it is run from inside a Hive
// session (as agents' tests are): `hived msg` refuses to run there.
func notInASession(t *testing.T) {
	t.Helper()
	t.Setenv("HIVE_SESSION_ID", "")
}

// TestE2E_MsgReachesTheShell: `hived msg` types the note and Enter into
// a real session, and the shell runs it. The marker is an arithmetic
// expansion so its RESULT (m338-3) only appears if the line was executed;
// the typed echo of the command itself reads m338-$((1+2)).
func TestE2E_MsgReachesTheShell(t *testing.T) {
	notInASession(t)
	d := spawnDaemon(t)
	id := firstSession(t, d)
	a := dialAttach(t, d, id)

	out, err := exec.Command(hivedBinary(t), "msg", id, "echo m338-$((1+2))").CombinedOutput()
	if err != nil {
		t.Fatalf("hived msg: %v\n%s", err, out)
	}
	// Named before sending, and the delivery kind after.
	if !strings.Contains(string(out), "to ") || !(strings.Contains(string(out), "typed") || strings.Contains(string(out), "queued")) {
		t.Errorf("output = %q, want the resolved target and a delivery kind", out)
	}
	if _, err := a.WaitForData([]byte("m338-3"), 10*time.Second); err != nil {
		t.Fatalf("the shell never ran the message: %v", err)
	}
}

func TestE2E_MsgUnknownSessionFails(t *testing.T) {
	notInASession(t)
	spawnDaemon(t)
	out, err := exec.Command(hivedBinary(t), "msg", "no-such-session", "hi").CombinedOutput()
	if err == nil {
		t.Fatalf("expected a nonzero exit, got:\n%s", out)
	}
	if !strings.Contains(string(out), "no session") {
		t.Errorf("output = %q, want the unknown-target message", out)
	}
}
