package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
)

// TestMain lets the test binary double as `hived hook`: the daemon
// resolves os.Executable() as the hived path and wires Claude's hooks to
// it, so when a real claude fires a hook it lands here and takes the
// same runHook path production does.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "hook" {
		runHook(os.Stdin)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// captureSink accumulates everything the session writes, so the probe
// can answer the folder-trust dialog and see the permission prompt.
type captureSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *captureSink) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *captureSink) contains(s string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Contains(c.buf.String(), s)
}

// TestClaudeProbeWaitingPermission is the live check the fixture tests
// cannot give: a REAL claude, launched by the daemon from a directory
// where a mutating Bash command is not pre-approved, reports waiting_permission through
// the hook tier when asked to run a shell command. Opt-in — it costs
// one API call:
//
//	HIVE_PROBE_CLAUDE=1 go test ./cmd/hived/ -run TestClaudeProbe -v
func TestClaudeProbeWaitingPermission(t *testing.T) {
	if os.Getenv("HIVE_PROBE_CLAUDE") != "1" {
		t.Skip("set HIVE_PROBE_CLAUDE=1 to run the real-claude probe")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not on PATH")
	}
	// When this test itself runs inside a Claude Code session, the child
	// claude would inherit that session's identity and permission mode
	// and never prompt. Strip the nesting markers so it behaves like the
	// claude a user launches from Hive.
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "CLAUDECODE" || k == "CLAUDE_PID" || strings.HasPrefix(k, "CLAUDE_CODE_") {
			_ = os.Unsetenv(k)
		}
	}
	d := startHookTestDaemon(t)
	cwd := t.TempDir()

	// Explicit argv: the user's own Claude default may be auto mode, in
	// which case nothing ever prompts. --permission-mode default forces
	// the prompt; the hooks are appended by hand because a raw Cmd is
	// deliberately never rewritten by the daemon (env still is).
	def, _ := agent.Get(agent.IDClaude)
	exe, _ := os.Executable()
	hooks := def.SpawnArgs(agent.SpawnInfo{HivedPath: exe})
	if len(hooks) == 0 {
		t.Fatal("claude adapter returned no SpawnArgs; version gate?")
	}
	cmd := append([]string{"claude", "--permission-mode", "default"}, hooks...)
	e, err := d.Registry().Create(context.Background(), wire.CreateSpec{
		Agent: "claude", Cmd: cmd, Cwd: cwd, Cols: 120, Rows: 40,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := e.ID
	t.Cleanup(func() { _ = d.Registry().Kill(id, true) })

	wait := func(within time.Duration, cond func(wire.SessionInfo) bool, what string) wire.SessionInfo {
		t.Helper()
		deadline := time.Now().Add(within)
		for {
			info, ok := findSessionByID(d, id)
			if ok && cond(info) {
				return info
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s; last info = %+v", what, info)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Wait for the process to be attached, then tap its output.
	wait(20*time.Second, func(i wire.SessionInfo) bool { return i.Alive }, "session alive")
	sess := d.Registry().Get(id).Session()
	if sess == nil {
		t.Fatal("no live session")
	}
	sink := &captureSink{}
	unsub, err := sess.SubscribeWithAtomicReplay(sink, func(replay []byte) error {
		_, err := sink.Write(replay)
		return err
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer unsub()

	// A fresh directory gets the folder-trust dialog; Enter accepts.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if sink.contains("trust") {
			_, _ = sess.Write([]byte("\r"))
			break
		}
		if info, _ := findSessionByID(d, id); info.StateSource == wire.StateSourceHook {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// SessionStart is a ping: it promotes the tier without moving state.
	wait(30*time.Second, func(i wire.SessionInfo) bool {
		return i.StateSource == wire.StateSourceHook
	}, "hook tier (SessionStart ping)")

	// Let the prompt paint before typing into it.
	time.Sleep(2 * time.Second)
	if _, err := sess.Write([]byte("use the Bash tool to run: touch probe.txt\r")); err != nil {
		t.Fatalf("write prompt: %v", err)
	}
	wait(10*time.Second, func(i wire.SessionInfo) bool {
		return i.State == wire.StateWorking && i.StateSource == wire.StateSourceHook
	}, "working after prompt")

	t.Cleanup(func() {
		if dir := os.Getenv("HIVE_PROBE_DUMP"); dir != "" {
			sink.mu.Lock()
			_ = os.WriteFile(dir+"/probe-screen.bin", sink.buf.Bytes(), 0o644)
			sink.mu.Unlock()
		}
	})
	info := wait(90*time.Second, func(i wire.SessionInfo) bool {
		return i.State == wire.StateWaitingPermission
	}, "waiting_permission")
	if !info.NeedsAttention {
		t.Errorf("NeedsAttention = false on waiting_permission")
	}
	if !strings.Contains(info.LastPrompt, "touch") {
		t.Errorf("LastPrompt = %q, want the typed prompt", info.LastPrompt)
	}

	// Deny (Esc), then leave. Either the hook's session_end or the
	// PTY exit must land the session at exited.
	_, _ = sess.Write([]byte("\x1b"))
	time.Sleep(time.Second)
	_, _ = sess.Write([]byte("/exit\r"))
	wait(20*time.Second, func(i wire.SessionInfo) bool {
		return i.State == wire.StateExited
	}, "exited")
}
