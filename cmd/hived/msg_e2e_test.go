// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build e2e

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/wire"
)

// notInASession keeps the test hermetic when it is run from inside a Hive
// session (as agents' tests are): with both HIVE_SESSION_ID and
// HIVE_SOCKET set, `hived msg` takes the in-session path.
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

// orchSessions creates a sender and an agent-kind target (cat, so a
// typed note echoes) in the bootstrap session's project and returns
// their ids.
func orchSessions(t *testing.T, d *spawnedDaemon, grant bool) (sender, target string) {
	t.Helper()
	c := dialControl(t, d)
	for _, spec := range []wire.CreateSpec{
		{Name: "boss", Agent: "claude", Cmd: []string{"/bin/cat"}, Cols: 80, Rows: 24, Orchestrator: grant},
		{Name: "worker", Agent: "claude", Cmd: []string{"/bin/cat"}, Cols: 80, Rows: 24},
	} {
		if err := c.CreateSession(spec); err != nil {
			t.Fatalf("create %s: %v", spec.Name, err)
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := c.ListSessions(); err != nil {
			t.Fatal(err)
		}
		snap, err := c.AwaitSessionsSnapshot(2 * time.Second)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range snap.Sessions {
			switch s.Name {
			case "boss":
				sender = s.ID
			case "worker":
				target = s.ID
			}
		}
		if sender != "" && target != "" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("sessions never appeared")
	return
}

// inSession runs `hived msg` the way an agent's shell would: HIVE_SOCKET
// names the narrowed events socket and HIVE_SESSION_ID the caller.
func inSession(t *testing.T, d *spawnedDaemon, sessionID string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(hivedBinary(t), append([]string{"msg"}, args...)...)
	cmd.Env = append(os.Environ(),
		"HIVE_SESSION_ID="+sessionID,
		"HIVE_SOCKET="+daemon.EventSocketPath(d.sockPath),
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestE2E_MsgFromSessionNeedsTheGrant(t *testing.T) {
	d := spawnDaemon(t)
	sender, _ := orchSessions(t, d, false)
	out, err := inSession(t, d, sender, "worker", "hi")
	if err == nil {
		t.Fatalf("an ungranted session's msg succeeded:\n%s", out)
	}
	if !strings.Contains(out, `"May direct other sessions"`) {
		t.Errorf("output = %q, want the one-line explanation naming the checkbox", out)
	}
}

func TestE2E_MsgFromGrantedSessionCarriesProvenance(t *testing.T) {
	d := spawnDaemon(t)
	sender, target := orchSessions(t, d, true)
	a := dialAttach(t, d, target)
	out, err := inSession(t, d, sender, "worker", "rebase onto main")
	if err != nil {
		t.Fatalf("granted msg failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "typed") && !strings.Contains(out, "queued") {
		t.Errorf("output = %q, want a delivery kind", out)
	}
	if _, err := a.WaitForData([]byte(`Message from Hive session "boss"`), 10*time.Second); err != nil {
		t.Fatalf("the target never saw the provenance prefix: %v", err)
	}
}
