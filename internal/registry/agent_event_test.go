package registry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// stubClaudeHooksVersion makes the Claude adapter's version gate pass
// (or fail) without forking a real `claude` binary. Test-only.
func stubClaudeHooksVersion(t *testing.T, ok bool) {
	t.Helper()
	v := []byte("1.0.0")
	if ok {
		v = []byte("2.1.260")
	}
	t.Cleanup(agent.SetClaudeVersionProbeForTest(func() ([]byte, error) { return v, nil }))
}

// TestSpawnEnvCarriesHiveIDs pins that every spawned session — agent
// or plain shell, agent-resolved cmd or a client's raw Cmd — carries
// HIVE_SESSION_ID and HIVE_SOCKET in its environment. Verified by
// actually spawning a shell one-liner and reading back what it saw
// (no /proc on every platform, so this is the portable way to check).
func TestSpawnEnvCarriesHiveIDs(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	r.SetSocketPath("/tmp/hive-test-XYZ.sock")

	out := filepath.Join(t.TempDir(), "env.out")
	e, err := r.Create(context.Background(), wire.CreateSpec{
		Name: "raw",
		Cmd:  []string{"sh", "-c", `printf '%s|%s' "$HIVE_SESSION_ID" "$HIVE_SOCKET" > ` + out},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	var content []byte
	for time.Now().Before(deadline) {
		if b, rerr := os.ReadFile(out); rerr == nil {
			content = b
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	want := e.ID + "|/tmp/hive-test-XYZ.sock"
	if string(content) != want {
		t.Errorf("child saw env %q, want %q", content, want)
	}
}

// TestRawCmdGetsEnvNotSpawnArgs pins the "raw Cmd from a client that
// doesn't speak agent IDs" rule: env is injected regardless, but
// SpawnArgs (and SessionIDFlag) are never appended to a caller-supplied
// Cmd — we don't mutate user-supplied argv.
func TestRawCmdGetsEnvNotSpawnArgs(t *testing.T) {
	skipOnWindows(t)
	stubClaudeHooksVersion(t, true)
	rec := captureStartSession(t)
	r := freshRegistry(t)
	r.SetHivedPath("/usr/local/bin/hived-test")

	rawCmd := []string{"claude", "--foo"}
	_, err := r.Create(context.Background(), wire.CreateSpec{
		Name: "raw", Agent: "claude", Cmd: rawCmd,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	got := rec.opts[len(rec.opts)-1]
	if len(got.Cmd) != len(rawCmd) {
		t.Fatalf("Cmd = %v, want unchanged %v", got.Cmd, rawCmd)
	}
	for i := range rawCmd {
		if got.Cmd[i] != rawCmd[i] {
			t.Errorf("Cmd[%d] = %q, want %q", i, got.Cmd[i], rawCmd[i])
		}
	}
	if !hasEnvVar(got.Env, "HIVE_SESSION_ID") {
		t.Errorf("Env = %v, missing HIVE_SESSION_ID", got.Env)
	}
}

// TestCreateAppendsSpawnArgsForResolvedAgent pins the opposite case:
// when the daemon resolves the agent's own Cmd (no explicit spec.Cmd),
// Claude's SpawnArgs (--settings ...) is appended after SessionIDFlag.
func TestCreateAppendsSpawnArgsForResolvedAgent(t *testing.T) {
	skipOnWindows(t)
	stubClaudeHooksVersion(t, true)
	rec := captureStartSession(t)
	r := freshRegistry(t)
	r.SetHivedPath("/usr/local/bin/hived-test")

	e, err := r.Create(context.Background(), wire.CreateSpec{Name: "c", Agent: "claude"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	rec.mu.Lock()
	got := rec.opts[len(rec.opts)-1].Cmd
	rec.mu.Unlock()
	if len(got) < 4 || got[0] != "claude" || got[1] != "--session-id" || got[2] != e.ID {
		t.Fatalf("Cmd = %v, want [claude --session-id %s ...]", got, e.ID)
	}
	if got[len(got)-2] != "--settings" || !strings.Contains(got[len(got)-1], `"UserPromptSubmit"`) {
		t.Errorf("Cmd tail = %v, want --settings <hooks json>", got[len(got)-2:])
	}
}

// TestRestartAppendsSpawnArgs pins the same rule on the restart path
// (registry.go's Restart, which builds its own argv and must append
// SpawnArgs itself rather than relying on Revive to do it).
func TestRestartAppendsSpawnArgs(t *testing.T) {
	skipOnWindows(t)
	stubClaudeHooksVersion(t, true)
	rec := captureStartSession(t)
	r := freshRegistry(t)
	r.SetHivedPath("/usr/local/bin/hived-test")

	e, err := r.Create(context.Background(), wire.CreateSpec{Name: "c", Agent: "claude"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := r.Restart(e.ID); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	rec.mu.Lock()
	got := rec.opts[len(rec.opts)-1]
	rec.mu.Unlock()
	if got.Cmd[len(got.Cmd)-2] != "--settings" {
		t.Errorf("Restart Cmd = %v, want a trailing --settings", got.Cmd)
	}
	if !hasEnvVar(got.Env, "HIVE_SESSION_ID") {
		t.Errorf("Restart Env = %v, missing HIVE_SESSION_ID", got.Env)
	}
}

// TestReviveStartsIdleHeuristicEmptyText pins that a fresh machine
// (what Revive/Restart install via attachSessionHooks) starts idle, on
// the heuristic tier, with no remembered text — "a daemon restart
// starts every session idle" falls out of this with no clearing code.
func TestReviveStartsIdleHeuristicEmptyText(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, err := r.Create(context.Background(), wire.CreateSpec{Name: "c", Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Report a hook event so the machine is no longer at its zero value...
	if err := r.ApplyAgentEvent(e.ID, wire.AgentEvent{
		SessionID: e.ID, Kind: wire.AgentEventWaitingPermission, Source: wire.StateSourceHook,
	}); err != nil {
		t.Fatalf("ApplyAgentEvent: %v", err)
	}
	if info := r.Get(e.ID).Info(); info.State != wire.StateWaitingPermission {
		t.Fatalf("precondition: state = %q, want waiting_permission", info.State)
	}
	// ...then restart it and confirm the new machine is idle/heuristic/empty.
	if err := r.Restart(e.ID); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	info := r.Get(e.ID).Info()
	if info.State != wire.StateIdle || info.StateSource != wire.StateSourceHeuristic ||
		info.LastPrompt != "" || info.LastSummary != "" {
		t.Errorf("post-restart info = %+v, want idle/heuristic/empty", info)
	}
}

// TestAgentEventBeforeReadyApplied pins that Entry.state is never nil:
// an AGENT_EVENT that races a spawn (the entry exists, Phase !=
// PhaseReady) is applied rather than dropped or panicking.
func TestAgentEventBeforeReadyApplied(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)

	release := make(chan struct{})
	restore := SetStartSessionForTest(func(opts session.Options) (*session.Session, error) {
		<-release
		return session.Start(opts)
	})
	t.Cleanup(restore)

	var id string
	done := make(chan struct{})
	go func() {
		e, _ := r.Create(context.Background(), wire.CreateSpec{Name: "slow", Shell: "/bin/bash"})
		if e != nil {
			id = e.ID
		}
		close(done)
	}()

	// Poll until the entry exists but is still spawning.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for _, e := range r.entries {
			if e.Phase == wire.PhaseSpawning {
				id = e.ID
			}
		}
		r.mu.Unlock()
		if id != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if id == "" {
		close(release)
		<-done
		t.Fatal("never observed the entry mid-spawn")
	}

	if err := r.ApplyAgentEvent(id, wire.AgentEvent{
		SessionID: id, Kind: wire.AgentEventPrompt, Source: wire.StateSourceHook, Text: "hi",
	}); err != nil {
		t.Fatalf("ApplyAgentEvent mid-spawn: %v", err)
	}
	// Applied immediately, while the entry is still mid-spawn (no
	// panic, no drop) — Entry.state is never nil.
	r.mu.Lock()
	mid := r.entries[id].stateSnapshot()
	r.mu.Unlock()
	if mid.State != wire.StateWorking || mid.LastPrompt != "hi" {
		t.Errorf("mid-spawn snapshot = %+v, want the event to have applied", mid)
	}

	close(release)
	<-done
	// Once the PTY attaches, attachSessionHooks installs a fresh
	// machine (by design — a daemon restart/revive/attach always
	// starts idle), so the mid-spawn event is superseded, not
	// remembered. That's a separate contract from "was it dropped".
	if info := r.Get(id).Info(); info.State != wire.StateIdle {
		t.Errorf("post-attach state = %q, want idle (fresh machine on attach)", info.State)
	}
	_ = r.Kill(id, true)
}

// TestApplyAgentEventUnknownSession pins ApplyAgentEvent's contract for
// the daemon's ModeEvent arm: an unknown id returns ErrNotFound rather
// than panicking, so the daemon can log and move on.
func TestApplyAgentEventUnknownSession(t *testing.T) {
	r := freshRegistry(t)
	err := r.ApplyAgentEvent("nope", wire.AgentEvent{SessionID: "nope", Kind: wire.AgentEventPing, Source: wire.StateSourceHook})
	if err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func hasEnvVar(env []string, key string) bool {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return true
		}
	}
	return false
}

// TestApplyAgentEventClampsFutureStamp pins the clamp on a reporter's
// clock. Machine.Apply orders events by At and trusted() measures
// staleness from it, so an unclamped future stamp would freeze the
// session for good: every later event sorts older and is dropped, and
// now.Sub(hookSeenAt) stays negative so the heuristic tier can never
// reclaim it either. One bad stamp must cost one event, not the
// session.
func TestApplyAgentEventClampsFutureStamp(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, err := r.Create(context.Background(), wire.CreateSpec{Name: "c", Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// A reporter with a badly skewed clock says the turn is over.
	if err := r.ApplyAgentEvent(e.ID, wire.AgentEvent{
		SessionID: e.ID, Kind: wire.AgentEventTurnEnd, Source: wire.StateSourceHook,
		At: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("ApplyAgentEvent (future): %v", err)
	}
	if info := r.Get(e.ID).Info(); info.State != wire.StateIdle {
		t.Fatalf("precondition: state = %q, want idle", info.State)
	}

	// A correctly stamped event that follows must still be applied.
	// Without the clamp this is dropped and the session is stuck idle
	// for the rest of its life.
	if err := r.ApplyAgentEvent(e.ID, wire.AgentEvent{
		SessionID: e.ID, Kind: wire.AgentEventPrompt, Source: wire.StateSourceHook,
		At: time.Now().UTC().Format(time.RFC3339Nano), Text: "next question",
	}); err != nil {
		t.Fatalf("ApplyAgentEvent (now): %v", err)
	}
	if info := r.Get(e.ID).Info(); info.State != wire.StateWorking {
		t.Errorf("state = %q, want working — a future stamp froze the session", info.State)
	}
}
