package registry

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// configureCustomAgents writes an agents.json into a temp dir and
// points the agent package at it for the duration of the test.
func configureCustomAgents(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, agent.CustomFileName), []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", agent.CustomFileName, err)
	}
	agent.SetCustomDir(dir)
	t.Cleanup(func() { agent.SetCustomDir("") })
}

// TestCreateResolvesCustomAgentCmd is the functional half of custom
// agents: a session created with a user-defined agent id must spawn
// that agent's configured argv, proving the merge at agent.Get()
// reaches Registry.Create's command resolution.
func TestCreateResolvesCustomAgentCmd(t *testing.T) {
	skipOnWindows(t)
	configureCustomAgents(t, `[
	  {"id": "claude-lite", "name": "Claude Lite", "cmd": ["claude", "--model", "haiku"]}
	]`)
	rec := captureStartSession(t)
	r := freshRegistry(t)

	if _, err := r.Create(wire.CreateSpec{
		Name: "lite", Agent: "claude-lite", Shell: "/bin/bash",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	want := []string{"claude", "--model", "haiku"}
	got := rec.opts[len(rec.opts)-1].Cmd
	if !slices.Equal(got, want) {
		t.Errorf("Create cmd = %v, want %v", got, want)
	}
}

// TestReviveResolvesCustomAgentFromPersistedID is the constraint that
// shaped the whole design: persist.go stores only the agent ID, so
// Revive re-resolves the command through agent.Get every time. If the
// daemon could not see the custom config at revive time, a restarted
// Hive would drop these sessions to a bare shell.
func TestReviveResolvesCustomAgentFromPersistedID(t *testing.T) {
	skipOnWindows(t)
	configureCustomAgents(t, `[
	  {"id": "mytool", "name": "My Tool", "cmd": ["mytool", "--fast"]}
	]`)
	rec := captureStartSession(t)
	r := freshRegistry(t)

	a, err := r.Create(wire.CreateSpec{Name: "t1", Agent: "mytool", Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Tear down the live session so Revive respawns, mirroring what
	// daemon startup sees: a persisted entry with no live PTY.
	r.mu.Lock()
	sess := r.entries[a.ID].sess
	r.entries[a.ID].sess = nil
	r.mu.Unlock()
	if sess != nil {
		_ = sess.Close()
		<-sess.Done()
	}

	if err := r.Revive(a.ID, session.Options{Shell: "/bin/bash"}); err != nil {
		t.Fatalf("Revive: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	want := []string{"mytool", "--fast"}
	got := rec.opts[len(rec.opts)-1].Cmd
	if !slices.Equal(got, want) {
		t.Errorf("Revive cmd = %v, want %v", got, want)
	}
}

// TestRestartCustomAgentFallsBackToCmd documents the accepted v1
// ceiling: custom agents have no ResumeArgs/ResumeCmd (JSON cannot
// express a Go func), so Restart re-runs the base command rather than
// resuming the prior conversation.
func TestRestartCustomAgentFallsBackToCmd(t *testing.T) {
	skipOnWindows(t)
	configureCustomAgents(t, `[
	  {"id": "mytool", "name": "My Tool", "cmd": ["mytool", "--fast"]}
	]`)
	rec := captureStartSession(t)
	r := freshRegistry(t)

	a, err := r.Create(wire.CreateSpec{Name: "t1", Agent: "mytool", Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := r.Restart(a.ID); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	want := []string{"mytool", "--fast"}
	got := rec.opts[len(rec.opts)-1].Cmd
	if !slices.Equal(got, want) {
		t.Errorf("Restart cmd = %v, want %v", got, want)
	}
}

// TestCreateWithUnknownAgentFallsBackToShell guards the failure mode a
// broken config would produce: an unresolvable agent id must leave Cmd
// empty so the daemon spawns a plain shell, never a partially-built
// argv.
func TestCreateWithUnknownAgentFallsBackToShell(t *testing.T) {
	skipOnWindows(t)
	agent.SetCustomDir("")
	rec := captureStartSession(t)
	r := freshRegistry(t)

	if _, err := r.Create(wire.CreateSpec{
		Name: "ghost", Agent: "no-such-agent", Shell: "/bin/bash",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if got := rec.opts[len(rec.opts)-1].Cmd; len(got) != 0 {
		t.Errorf("Create cmd = %v, want empty (shell fallback)", got)
	}
}
