//go:build e2e

// Layer A end-to-end coverage for user-defined agents. See
// e2e_test.go for the harness and isolation guarantees.
//
// Run: go test -tags=e2e ./cmd/hived/...
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
)

// TestE2E_CustomAgentLaunches drives a real hived process end to end
// with a user-defined agent in agents.json.
//
// The registry unit tests cover command resolution, but only this test
// covers main.go's agent.SetCustomDir(stateDir) call — without it the
// daemon silently ignores every custom agent and launches a bare
// shell, which is exactly the failure a unit test would miss.
func TestE2E_CustomAgentLaunches(t *testing.T) {
	d := spawnDaemon(t)

	// The daemon loads agents.json lazily on its first agent.Get, so
	// writing it after spawn (but before creating a session) also
	// exercises the mtime-driven reload that lets the GUI's save reach
	// an already-running daemon with no IPC.
	if err := os.MkdirAll(d.stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	cfg := filepath.Join(d.stateDir, agent.CustomFileName)
	body := `[{"id":"e2e-tool","name":"E2E Tool","cmd":["echo","HIVE_CUSTOM_AGENT_OK"],"color":"#8b5cf6"}]`
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", agent.CustomFileName, err)
	}

	ctl := dialControl(t, d)
	if err := ctl.CreateSession(wire.CreateSpec{
		Name: "custom", Agent: "e2e-tool", Cols: 80, Rows: 24, Shell: "/bin/bash",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	ev, err := ctl.AwaitSessionEvent(wire.SessionEventAdded, 3*time.Second)
	if err != nil {
		t.Fatalf("await added: %v", err)
	}
	if ev.Session.Agent != "e2e-tool" {
		t.Errorf("session agent = %q, want %q", ev.Session.Agent, "e2e-tool")
	}

	// The configured command actually ran in the PTY.
	a := dialAttach(t, d, ev.Session.ID)
	defer a.Close()
	if _, err := a.WaitForData([]byte("HIVE_CUSTOM_AGENT_OK"), 5*time.Second); err != nil {
		t.Fatalf("custom agent command did not run: %v", err)
	}
}

// TestE2E_MalformedCustomAgentsDoesNotBreakDaemon is the robustness
// boundary: a hand-corrupted agents.json must never stop the daemon
// from launching built-in agents.
func TestE2E_MalformedCustomAgentsDoesNotBreakDaemon(t *testing.T) {
	d := spawnDaemon(t)

	if err := os.MkdirAll(d.stateDir, 0o700); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	cfg := filepath.Join(d.stateDir, agent.CustomFileName)
	if err := os.WriteFile(cfg, []byte(`[{"id": "broken",,,`), 0o600); err != nil {
		t.Fatalf("write %s: %v", agent.CustomFileName, err)
	}

	ctl := dialControl(t, d)
	if err := ctl.CreateSession(wire.CreateSpec{
		Name: "plain", Cols: 80, Rows: 24, Shell: "/bin/bash",
	}); err != nil {
		t.Fatalf("create with a corrupt agents.json: %v", err)
	}
	ev, err := ctl.AwaitSessionEvent(wire.SessionEventAdded, 3*time.Second)
	if err != nil {
		t.Fatalf("await added: %v", err)
	}

	a := dialAttach(t, d, ev.Session.ID)
	defer a.Close()
	if err := a.WriteStdin([]byte("echo HIVE_STILL_ALIVE_$((2+3))\n")); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if _, err := a.WaitForData([]byte("HIVE_STILL_ALIVE_5"), 5*time.Second); err != nil {
		t.Fatalf("shell session unusable after a corrupt config: %v", err)
	}
}
