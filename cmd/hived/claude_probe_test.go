// The e2e build (-tags=e2e) has its own TestMain in e2e_test.go; the
// probe is opt-in and needs the hook dispatch below, so it lives in the
// default build only.
//go:build !e2e

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
	"github.com/lucascaro/hive/internal/session"
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

// tail returns the last n bytes written, for a failure message.
func (c *captureSink) tail(n int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.buf.Bytes()
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
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
	//
	// Restored by hand rather than with t.Setenv: the set is discovered
	// at runtime and t.Setenv cannot unset. Without the restore these
	// stay stripped for every later test in the binary.
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if k == "CLAUDECODE" || k == "CLAUDE_PID" || strings.HasPrefix(k, "CLAUDE_CODE_") {
			k, v := k, v
			t.Cleanup(func() { _ = os.Setenv(k, v) })
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

// TestClaudeProbePermissionResolved: allowing the prompt must move the
// session off waiting_permission BEFORE the tool finishes. A tool that
// runs for a minute is "working", not "waiting for you" — and a session
// that only leaves waiting_permission on PostToolUse sits lit for the
// whole minute.
func TestClaudeProbePermissionResolved(t *testing.T) {
	if os.Getenv("HIVE_PROBE_CLAUDE") != "1" {
		t.Skip("set HIVE_PROBE_CLAUDE=1 to run the real-claude probe")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not on PATH")
	}
	//
	// Restored by hand rather than with t.Setenv: the set is discovered
	// at runtime and t.Setenv cannot unset. Without the restore these
	// stay stripped for every later test in the binary.
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if k == "CLAUDECODE" || k == "CLAUDE_PID" || strings.HasPrefix(k, "CLAUDE_CODE_") {
			k, v := k, v
			t.Cleanup(func() { _ = os.Setenv(k, v) })
			_ = os.Unsetenv(k)
		}
	}
	d := startHookTestDaemon(t)
	def, _ := agent.Get(agent.IDClaude)
	exe, _ := os.Executable()
	cmd := append([]string{"claude", "--permission-mode", "default"},
		def.SpawnArgs(agent.SpawnInfo{HivedPath: exe})...)
	e, err := d.Registry().Create(context.Background(), wire.CreateSpec{
		Agent: "claude", Cmd: cmd, Cwd: t.TempDir(), Cols: 120, Rows: 40,
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
	wait(20*time.Second, func(i wire.SessionInfo) bool { return i.Alive }, "alive")
	sess := d.Registry().Get(id).Session()
	wait(30*time.Second, func(i wire.SessionInfo) bool { return i.StateSource == wire.StateSourceHook }, "hook tier")
	time.Sleep(2 * time.Second)
	_, _ = sess.Write([]byte("use the Bash tool to run exactly: sleep 12; touch probe.txt\r"))
	wait(90*time.Second, func(i wire.SessionInfo) bool { return i.State == wire.StateWaitingPermission }, "waiting_permission")
	time.Sleep(500 * time.Millisecond)
	allowed := time.Now()
	_, _ = sess.Write([]byte("\r")) // "Yes"
	// The GUI reports the keystroke as "the user acted on this session";
	// Claude fires nothing between the allow and the tool finishing.
	_ = d.Registry().SetAttention(id, false)
	info := wait(5*time.Second, func(i wire.SessionInfo) bool { return i.State == wire.StateWorking }, "working while the tool runs")
	if took := time.Since(allowed); took > 2*time.Second {
		t.Errorf("left waiting_permission %v after allowing (state=%s)", took, info.State)
	}
	// PostToolUse (still working), then Stop -> waiting_input once the reply ends.
	wait(60*time.Second, func(i wire.SessionInfo) bool { return i.State == wire.StateWaitingInput && i.LastSummary != "" }, "turn_end after the tool")
	_, _ = sess.Write([]byte("/exit\r"))
}

// TestClaudeProbeErrorSurvivesIdlePrompt: a real interactive claude with
// an unknown model fails its turn (StopFailure -> error), and ~60s later
// fires Notification(idle_prompt). With nobody touching the session the
// error must still be showing after that. No API cost: the model is
// rejected before any tokens are spent. Takes ~90s.
func TestClaudeProbeErrorSurvivesIdlePrompt(t *testing.T) {
	if os.Getenv("HIVE_PROBE_CLAUDE") != "1" {
		t.Skip("set HIVE_PROBE_CLAUDE=1 to run the real-claude probe")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not on PATH")
	}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if k == "CLAUDECODE" || k == "CLAUDE_PID" || strings.HasPrefix(k, "CLAUDE_CODE_") {
			k, v := k, v
			t.Cleanup(func() { _ = os.Setenv(k, v) })
			_ = os.Unsetenv(k)
		}
	}
	d := startHookTestDaemon(t)
	def, _ := agent.Get(agent.IDClaude)
	exe, _ := os.Executable()
	cmd := append([]string{"claude", "--model", "claude-nonexistent-xyz"},
		def.SpawnArgs(agent.SpawnInfo{HivedPath: exe})...)
	e, err := d.Registry().Create(context.Background(), wire.CreateSpec{
		Agent: "claude", Cmd: cmd, Cwd: t.TempDir(), Cols: 120, Rows: 40,
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
	wait(20*time.Second, func(i wire.SessionInfo) bool { return i.Alive }, "alive")
	sess := d.Registry().Get(id).Session()
	sink := &captureSink{}
	unsub, err := sess.SubscribeWithAtomicReplay(sink, func(r []byte) error {
		_, err := sink.Write(r)
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
	wait(30*time.Second, func(i wire.SessionInfo) bool { return i.StateSource == wire.StateSourceHook }, "hook tier")
	time.Sleep(2 * time.Second)
	_, _ = sess.Write([]byte("hi\r"))
	wait(60*time.Second, func(i wire.SessionInfo) bool { return i.State == wire.StateError }, "error")

	// Past HookStaleAfter and past Claude's 60s idle_prompt.
	end := time.Now().Add(90 * time.Second)
	for time.Now().Before(end) {
		if info, _ := findSessionByID(d, id); info.State != wire.StateError {
			t.Fatalf("error did not stand: state %q (src %q)", info.State, info.StateSource)
		}
		time.Sleep(250 * time.Millisecond)
	}
	_, _ = sess.Write([]byte("/exit\r"))
}

// TestClaudeProbeTaskToolsOptIn is the tripwire for the plan indicator's
// one external dependency: Claude Code honouring
// CLAUDE_CODE_ENABLE_TODO_TOOLS. On current models (Opus 5 / Sonnet 5)
// Claude provides no task tools unless a session opts in, and without
// them Hive receives no plan at all. If a Claude release renames or
// drops that variable, nothing errors — the variable is silently
// ignored, the sidebar pie simply never appears, and the Settings toggle
// keeps reading "on" while doing nothing. This probe is how that shows
// up on upgrade rather than in a user's sidebar.
//
// Unlike the probes above it passes NO explicit Cmd: it creates
// `Agent: "claude"` so the real spawn path runs — resolveAgentCmd for the
// hooks AND resolveAgentEnv for the opt-in, gated together. A probe that
// hand-assembled the environment would keep passing after the production
// wiring broke.
//
// Discriminating only where the task tools are opt-in, i.e. the current
// default model. On an older model that ships them by default this would
// pass with or without the variable.
//
// Opt-in — it costs one API call:
//
//	HIVE_PROBE_CLAUDE=1 go test ./cmd/hived/ -run TestClaudeProbeTaskToolsOptIn -v
//
// probeWait polls the session's SessionInfo until cond holds, failing
// with the terminal's tail on timeout.
type probeWait func(within time.Duration, cond func(wire.SessionInfo) bool, what string) wire.SessionInfo

// startClaudeProbe starts a real Claude session under a test daemon with
// Hive's hooks wired, past the folder-trust dialog and onto the hook
// tier, and returns its PTY and a poller. Shared by the probes that
// drive Claude through a prompt and watch what the daemon derives.
func startClaudeProbe(t *testing.T) (*session.Session, probeWait) {
	t.Helper()
	// Strip the nesting markers, as the probes above do — and, here, it
	// matters twice: a CLAUDE_CODE_ENABLE_TODO_TOOLS inherited from the
	// Claude session running this test would count as the user's own
	// choice, so Hive would (correctly) not set it, and the probe would
	// not be testing Hive's opt-in at all.
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if k == "CLAUDECODE" || k == "CLAUDE_PID" || strings.HasPrefix(k, "CLAUDE_CODE_") {
			k, v := k, v
			t.Cleanup(func() { _ = os.Setenv(k, v) })
			_ = os.Unsetenv(k)
		}
	}

	d := startHookTestDaemon(t)
	exe, _ := os.Executable()
	// What hived's main does at startup: point Claude's hooks at this
	// binary (TestMain dispatches `hook`), and the settings loader at a
	// fresh directory — no agent-settings.json, so the default (on).
	d.Registry().SetHivedPath(exe)
	agent.SetCustomDir(t.TempDir())
	t.Cleanup(func() { agent.SetCustomDir("") })

	e, err := d.Registry().Create(context.Background(), wire.CreateSpec{
		Agent: "claude", Cwd: t.TempDir(), Cols: 120, Rows: 40,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	id := e.ID
	t.Cleanup(func() { _ = d.Registry().Kill(id, true) })

	// Tapped for the whole run, so a timeout can print what the terminal
	// actually showed instead of leaving the failure to guesswork.
	sink := &captureSink{}
	var wait probeWait = func(within time.Duration, cond func(wire.SessionInfo) bool, what string) wire.SessionInfo {
		t.Helper()
		deadline := time.Now().Add(within)
		for {
			info, ok := findSessionByID(d, id)
			if ok && cond(info) {
				return info
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s; last info = %+v\n--- terminal tail ---\n%s",
					what, info, sink.tail(1500))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	wait(20*time.Second, func(i wire.SessionInfo) bool { return i.Alive }, "alive")
	sess := d.Registry().Get(id).Session()
	if sess == nil {
		t.Fatal("no live session")
	}
	unsub, err := sess.SubscribeWithAtomicReplay(sink, func(replay []byte) error {
		_, err := sink.Write(replay)
		return err
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	t.Cleanup(unsub)

	// A fresh directory gets the folder-trust dialog; Enter accepts. Text
	// typed while it is up would go into the dialog, not the prompt.
	trustBy := time.Now().Add(20 * time.Second)
	for time.Now().Before(trustBy) {
		if sink.contains("trust") {
			_, _ = sess.Write([]byte("\r"))
			break
		}
		if info, _ := findSessionByID(d, id); info.StateSource == wire.StateSourceHook {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	wait(30*time.Second, func(i wire.SessionInfo) bool { return i.StateSource == wire.StateSourceHook }, "hook tier")
	time.Sleep(2 * time.Second)
	return sess, wait
}

func TestClaudeProbeTaskToolsOptIn(t *testing.T) {
	if os.Getenv("HIVE_PROBE_CLAUDE") != "1" {
		t.Skip("set HIVE_PROBE_CLAUDE=1 to run the real-claude probe")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not on PATH")
	}
	sess, wait := startClaudeProbe(t)

	// Task tools need no permission, so nothing here can stall on a
	// prompt. Two tasks and one completion exercise TaskCreate (the ID
	// read from the PostToolUse response) and TaskUpdate (a merge by ID).
	//
	// The Enter goes in its OWN write, after a pause. Sent in the same
	// write as a prompt this long, Claude Code takes the whole burst as a
	// paste, and a return inside a paste does not submit — the first run
	// of this probe sat with the prompt typed into the box, unsent, and
	// timed out. (The shorter prompts in the probes above get away with a
	// single write.)
	_, _ = sess.Write([]byte("Using only your task list tools, and without creating any files: " +
		"create a task named alpha and a task named beta, then mark alpha completed. Then reply done."))
	time.Sleep(700 * time.Millisecond)
	_, _ = sess.Write([]byte("\r"))

	// plan_total > 0 is the whole chain at once: the variable reached the
	// process, Claude provided the tools, the hook parsed TaskCreate, and
	// the daemon merged it.
	info := wait(120*time.Second, func(i wire.SessionInfo) bool { return i.PlanTotal >= 2 }, "a plan from the task tools")
	if info.PlanTotal != 2 {
		t.Errorf("plan_total = %d, want 2", info.PlanTotal)
	}
	wait(60*time.Second, func(i wire.SessionInfo) bool { return i.PlanDone >= 1 }, "TaskUpdate completing alpha")
	_, _ = sess.Write([]byte("/exit\r"))
}

// TestClaudeProbeSubagentCount drives a real Claude session through one
// subagent and checks what phase 1b derives from it: SubagentStart /
// SubagentStop wired and mapped, subagents_running rising and returning
// to 0, and a subagent's tools never showing as the session's tool.
func TestClaudeProbeSubagentCount(t *testing.T) {
	if os.Getenv("HIVE_PROBE_CLAUDE") != "1" {
		t.Skip("set HIVE_PROBE_CLAUDE=1 to run the real-claude probe")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude not on PATH")
	}
	sess, wait := startClaudeProbe(t)

	// Sent in two writes for the same paste reason as the task-tools probe.
	_, _ = sess.Write([]byte("Launch one general-purpose subagent with the Agent tool whose task is: " +
		"run the shell command `sleep 8` and reply ok. Wait for it, then reply done."))
	time.Sleep(700 * time.Millisecond)
	_, _ = sess.Write([]byte("\r"))

	wait(120*time.Second, func(i wire.SessionInfo) bool { return i.SubagentsRunning >= 1 }, "subagents_running to rise")
	// While the subagent runs its Bash, the main thread is inside its
	// Agent call: the session's tool is Agent, never the subagent's Bash.
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		info := wait(time.Second, func(wire.SessionInfo) bool { return true }, "info")
		if info.CurrentTool == "Bash" {
			t.Fatalf("current_tool = Bash while a subagent ran it; subagent tools must not drive the session's tool")
		}
	}
	wait(120*time.Second, func(i wire.SessionInfo) bool { return i.SubagentsRunning == 0 }, "subagents_running back to 0")
	_, _ = sess.Write([]byte("/exit\r"))
}
