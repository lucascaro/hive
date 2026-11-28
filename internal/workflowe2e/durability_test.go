// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package workflowe2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/acp/acptest"
	"github.com/lucascaro/hive/internal/wire"
)

// Durability (spec 529, B3c): an agent node across a plugin crash, a hived
// restart and the plugin's crash loop, against the real daemon and the fake
// adapter. The turns are gated, so a test decides what happened to a turn
// while the plugin was away.
//
// The fake counts gated turns per process, so after a hived restart its first
// turn is "turn-<sid>-1" again: a test clears the gate files while the daemon
// is down, before the new fake can announce a turn with a name already used.

// gateFiles are the turns the fake is holding: announced and not yet let go.
func gateFiles(dir string) []string {
	waiting, _ := filepath.Glob(filepath.Join(dir, "turn-*.waiting"))
	var out []string
	for _, w := range waiting {
		if _, err := os.Stat(strings.TrimSuffix(w, ".waiting") + ".go"); err != nil {
			out = append(out, w)
		}
	}
	return out
}

func (f *fixture) waitGate(dir string) {
	f.t.Helper()
	f.eventually(wait, "a turn to be held", func() bool { return len(gateFiles(dir)) > 0 })
}

// releaseTurn lets every held turn end.
func releaseTurn(dir string) {
	for _, w := range gateFiles(dir) {
		_ = os.WriteFile(strings.TrimSuffix(w, ".waiting")+".go", nil, 0o600)
	}
}

// clearGates forgets the gate files of a fake that no longer exists.
func clearGates(dir string) {
	files, _ := filepath.Glob(filepath.Join(dir, "turn-*"))
	for _, p := range files {
		_ = os.Remove(p)
	}
}

// hiveSession is the daemon's session for node key: its name ends " · key".
func (f *fixture) hiveSession(key string) wire.SessionInfo {
	f.t.Helper()
	var found wire.SessionInfo
	f.eventually(wait, "the session of "+key, func() bool {
		for _, s := range f.d.Registry().List() {
			if strings.HasSuffix(s.Name, " · "+key) {
				found = s
				return true
			}
		}
		return false
	})
	return found
}

func (f *fixture) sessionsNamed(key string) int {
	n := 0
	for _, s := range f.d.Registry().List() {
		if strings.HasSuffix(s.Name, " · "+key) {
			n++
		}
	}
	return n
}

// submitFor submits the running turn's result for node key, as the agent's submit tool would.
func (f *fixture) submitFor(key, summary string) {
	f.t.Helper()
	id := f.hiveSession(key).ID
	b, _ := json.Marshal(map[string]any{"status": "ok", "data": map[string]any{"summary": summary}})
	reg := f.d.Registry()
	f.eventually(wait, "the result to be accepted", func() bool { return reg.SubmitResult(id, reg.ACPNonceForTest(id), b) == nil })
}

// turnEnded waits for the daemon to report that the session's latest turn is over.
func (f *fixture) turnEnded(key string) {
	f.t.Helper()
	id := f.hiveSession(key).ID
	f.eventually(wait, "the turn of "+key+" to end", func() bool {
		msg, err := f.d.Registry().AcpTranscript(id)
		return err == nil && msg.StopReason != ""
	})
}

// killPlugin crashes the engine with no cleanup and returns its pid.
func (f *fixture) killPlugin() {
	f.t.Helper()
	pid := int(f.ok("test.pid", nil)["pid"].(float64))
	if err := kill9(pid); err != nil {
		f.t.Fatal(err)
	}
}

// setEnabled turns the plugin on or off the way Settings does, and waits for it to be ready when on.
func (f *fixture) setEnabled(on bool) {
	f.t.Helper()
	if err := f.c.SetPluginEnabled(pluginID, on); err != nil {
		f.t.Fatal(err)
	}
	if on {
		f.waitReady()
	}
}

// hivedRestart is a daemon restart with the fake's gate files cleared in between.
func (f *fixture) hivedRestart(dir string) {
	f.t.Helper()
	f.stop()
	clearGates(dir)
	f.start()
	f.waitReady()
}

func (f *fixture) startsOf(run, key string) int {
	n := 0
	for _, ty := range f.eventTypes(run, key) {
		if ty == "node.started" {
			n++
		}
	}
	return n
}

func oneAgent(name string) map[string]any { return ir(name, []any{agentNode("a", "do it", nil)}) }

// ----------------------------------------------------------------- plugin crash

func TestDurability_PluginCrash_FinishedWhileDown_RecordedAttemptUnchanged(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	run := f.startRun(pid, oneAgent("fw"))
	f.waitGate(dir)

	// The plugin dies, and is kept down while the agent finishes without it.
	f.killPlugin()
	f.setEnabled(false)
	f.submitFor("a", "finished while down")
	releaseTurn(dir)
	f.turnEnded("a")
	f.setEnabled(true)

	f.waitRun(run, "done", wait)
	if state, attempt := f.nodeState(run, "a"); state != "done" || attempt != 1 {
		t.Errorf("node a = %s attempt %d, want done attempt 1: the result was recorded, not retried", state, attempt)
	}
	if got := f.nodeOutput(run, "a")["summary"]; got != "finished while down" {
		t.Errorf("output = %v", got)
	}
	if n := len(fakeCalls(dir, "session/prompt")); n != 1 {
		t.Errorf("%d prompts, want 1: the turn was not sent again", n)
	}
	if n := len(fakeCalls(dir, "session/new")); n != 1 {
		t.Errorf("%d sessions made, want 1", n)
	}
	if n := f.startsOf(run, "a"); n != 1 {
		t.Errorf("%d node.started events, want 1", n)
	}
}

func TestDurability_PluginCrash_TurnStillRunning_Continues(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	run := f.startRun(pid, oneAgent("run"))
	f.waitGate(dir)

	f.killPlugin()
	f.waitReady() // the Manager restarts it while the turn is still held

	if state, attempt := f.nodeState(run, "a"); state != "running" || attempt != 1 {
		t.Fatalf("node a = %s attempt %d, want it still running attempt 1", state, attempt)
	}
	f.submitFor("a", "after the crash")
	releaseTurn(dir)
	f.waitRun(run, "done", wait)
	if state, attempt := f.nodeState(run, "a"); state != "done" || attempt != 1 {
		t.Errorf("node a = %s attempt %d, want done attempt 1", state, attempt)
	}
	if n := len(fakeCalls(dir, "session/prompt")); n != 1 {
		t.Errorf("%d prompts, want 1", n)
	}
	// The interim hazard B3b left: a recovered node must not start a second session in its worktree.
	if n := len(fakeCalls(dir, "session/new")); n != 1 {
		t.Errorf("%d sessions made, want 1", n)
	}
	if n := f.sessionsNamed("a"); n != 1 {
		t.Errorf("%d sessions for node a, want 1", n)
	}
}

// ---------------------------------------------------------------- hived restart

// interruptedPrompt is what the retry's turn says: the note, then the node's own prompt.
const interruptedPrompt = "Your previous attempt was interrupted"

func TestDurability_HivedRestart_RevivedSessionReprompted(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	run := f.startRun(pid, oneAgent("rv"))
	f.waitGate(dir)
	first := f.hiveSession("a").ID

	f.hivedRestart(dir)

	f.waitGate(dir) // the revived session was prompted again
	f.submitFor("a", "second try")
	releaseTurn(dir)
	f.waitRun(run, "done", wait)
	if state, attempt := f.nodeState(run, "a"); state != "done" || attempt != 2 {
		t.Errorf("node a = %s attempt %d, want done attempt 2", state, attempt)
	}
	if got := f.hiveSession("a").ID; got != first {
		t.Errorf("session = %s, want the revived %s", got, first)
	}
	turns := userTurns(t, dir)
	if len(turns) == 0 || !strings.HasPrefix(turns[len(turns)-1], interruptedPrompt) || !strings.Contains(turns[len(turns)-1], "do it") {
		t.Errorf("retry prompt = %q, want the interrupted note followed by the original prompt", turns)
	}
	if n := len(fakeCalls(dir, "session/new")); n != 1 {
		t.Errorf("%d sessions made, want 1: the revived one is reused", n)
	}
}

// freshAfterRestart: the agent cannot load the old conversation, so the daemon's revive fails, the engine's restart
// fails too, and a fresh session takes over in the same worktree, whose work is kept.
func freshAfterRestart(t *testing.T, flag string) {
	f, dir := newAgentFixture(t, acptest.FlagGate, flag)
	pid, _ := f.project()
	run := f.startRun(pid, oneAgent("fresh"))
	f.waitGate(dir)
	first := f.hiveSession("a")
	wt := first.WorktreePath
	if err := os.WriteFile(filepath.Join(wt, "wip.txt"), []byte("half done"), 0o644); err != nil {
		t.Fatal(err)
	}

	f.hivedRestart(dir)

	f.waitGate(dir)
	f.submitFor("a", "from a fresh session")
	releaseTurn(dir)
	f.waitRun(run, "done", wait)
	if state, attempt := f.nodeState(run, "a"); state != "done" || attempt != 2 {
		t.Errorf("node a = %s attempt %d, want done attempt 2", state, attempt)
	}
	now := f.hiveSession("a")
	if now.ID == first.ID {
		t.Errorf("session = %s, want a fresh one: the old conversation could not be loaded", now.ID)
	}
	if now.WorktreePath != wt {
		t.Errorf("fresh session works in %s, want the interrupted attempt's worktree %s", now.WorktreePath, wt)
	}
	if _, err := os.Stat(filepath.Join(wt, "wip.txt")); err != nil {
		t.Errorf("the interrupted attempt's work in %s was not kept: %v", wt, err)
	}
	if n := f.sessionsNamed("a"); n != 1 {
		t.Errorf("%d sessions for node a, want 1: the dead one is gone", n)
	}
	turns := userTurns(t, dir)
	if len(turns) == 0 || !strings.HasPrefix(turns[len(turns)-1], interruptedPrompt) {
		t.Errorf("fresh session's prompt = %q, want the interrupted note", turns)
	}
}

func TestDurability_HivedRestart_NoLoad_FreshSessionSameWorktree(t *testing.T) {
	freshAfterRestart(t, acptest.FlagNoLoad)
}

func TestDurability_LoadFails_FreshSession(t *testing.T) {
	freshAfterRestart(t, acptest.FlagLoadFails)
}

func TestDurability_MaxAttemptsExhausted_Fails(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	run := f.startRun(pid, oneAgent("max"))
	f.waitGate(dir)

	f.hivedRestart(dir) // attempt 2 starts
	f.waitGate(dir)
	f.hivedRestart(dir) // and is interrupted too

	f.waitRun(run, "failed", wait)
	if e := f.runError(run); !strings.Contains(e, "interrupted 2 times") {
		t.Errorf("run error = %q, want it to name the attempts", e)
	}
	if n := len(fakeCalls(dir, "session/prompt")); n > 2 {
		t.Errorf("%d prompts, want at most 2: no third attempt", n)
	}
}

func TestDurability_CompletedNeverRerun(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	run := f.startRun(pid, ir("twice",
		[]any{agentNode("a", "do a", nil), agentNode("b", "do b", nil)},
		edge("a", "b")))
	f.waitGate(dir)
	f.submitFor("a", "a done")
	releaseTurn(dir)
	f.waitNode(run, "a", "done", wait)
	f.eventually(wait, "b's turn to be held", func() bool { return len(gateFiles(dir)) > 0 && f.sessionsNamed("b") == 1 })

	f.hivedRestart(dir) // b is interrupted; a is done
	f.waitGate(dir)
	f.killPlugin() // then the plugin crashes mid-way through b's second attempt
	f.waitReady()
	f.submitFor("b", "b done")
	releaseTurn(dir)
	f.waitRun(run, "done", wait)

	if n := len(fakeCalls(dir, "session/new")); n != 2 {
		t.Errorf("%d sessions made across both restarts, want 2: one per node", n)
	}
	n := 0
	for _, u := range userTurns(t, dir) {
		if strings.Contains(u, "do a") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("node a was prompted %d times, want once: a finished node is never re-run", n)
	}
	if state, attempt := f.nodeState(run, "a"); state != "done" || attempt != 1 {
		t.Errorf("node a = %s attempt %d, want done attempt 1", state, attempt)
	}
	if state, attempt := f.nodeState(run, "b"); state != "done" || attempt != 2 {
		t.Errorf("node b = %s attempt %d, want done attempt 2", state, attempt)
	}
}

func TestDurability_ResultThenHivedRestart_InterruptedByDesign(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	run := f.startRun(pid, oneAgent("lost"))
	f.waitGate(dir)

	// The agent submits its result while the plugin is down, and hived restarts before the plugin reads it:
	// the daemon's restart resets the transcript and the result with it, so the turn cannot be recorded.
	f.killPlugin()
	f.setEnabled(false)
	f.submitFor("a", "lost with the restart")
	releaseTurn(dir)
	f.turnEnded("a")
	f.stop() // hived restarts while the plugin is off, so it cannot read the result first
	clearGates(dir)
	f.start()
	f.setEnabled(true)

	f.waitGate(dir)
	f.submitFor("a", "second")
	releaseTurn(dir)
	f.waitRun(run, "done", wait)
	if state, attempt := f.nodeState(run, "a"); state != "done" || attempt != 2 {
		t.Errorf("node a = %s attempt %d, want done attempt 2", state, attempt)
	}
	if got := f.nodeOutput(run, "a")["summary"]; got != "second" {
		t.Errorf("output = %v, want the second attempt's result", got)
	}
}

// ---------------------------------------------------------- the engine's own crash

func TestDurability_CreateInFlight_OrphanAdopted(t *testing.T) {
	// The plugin holds each created session for 6 s before recording it, so a crash lands between the daemon's CREATE and the row.
	t.Setenv("HIVE_WF_TEST_PAUSE", "after-create:6000")
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	run := f.startRun(pid, oneAgent("orphan"))
	orphan := f.hiveSession("a")
	if want := "plugin:workflows/workflow:" + run; orphan.SpawnedBy != want {
		t.Fatalf("the session is labelled %q, want %q", orphan.SpawnedBy, want)
	}
	if state, _ := f.nodeState(run, "a"); state == "running" {
		t.Fatalf("node a is already running: the crash would not land between CREATE and the row")
	}
	f.killPlugin()
	f.waitReady()

	f.waitGate(dir)
	f.submitFor("a", "adopted")
	releaseTurn(dir)
	f.waitRun(run, "done", wait)
	if state, attempt := f.nodeState(run, "a"); state != "done" || attempt != 1 {
		t.Errorf("node a = %s attempt %d, want done attempt 1: adoption is not a retry", state, attempt)
	}
	if got := f.nodeInfo(run, "a")["session_id"]; got != orphan.ID {
		t.Errorf("node a runs in %v, want the adopted %s", got, orphan.ID)
	}
	if n := len(fakeCalls(dir, "session/new")); n != 1 {
		t.Errorf("%d sessions made, want 1: the orphan was adopted, not duplicated", n)
	}
	if n := f.sessionsNamed("a"); n != 1 {
		t.Errorf("%d sessions for node a, want 1", n)
	}
}

func TestDurability_CrashBeforePromptAck_SendsTurnOnce(t *testing.T) {
	// The plugin holds the node after it recorded its session and before it sends the first turn.
	t.Setenv("HIVE_WF_TEST_PAUSE", "before-prompt:6000")
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	run := f.startRun(pid, oneAgent("noack"))
	f.waitNode(run, "a", "running", wait)
	if n := len(fakeCalls(dir, "session/prompt")); n != 0 {
		t.Fatalf("%d prompts before the crash, want none", n)
	}
	f.killPlugin()
	f.waitReady()

	f.waitGate(dir)
	f.submitFor("a", "sent once")
	releaseTurn(dir)
	f.waitRun(run, "done", wait)
	if n := len(fakeCalls(dir, "session/prompt")); n != 1 {
		t.Errorf("%d prompts, want exactly 1", n)
	}
	if state, attempt := f.nodeState(run, "a"); state != "done" || attempt != 1 {
		t.Errorf("node a = %s attempt %d, want done attempt 1", state, attempt)
	}
}

func TestDurability_LostPromptAck_AdoptsTranscriptTurn(t *testing.T) {
	// The plugin holds the node after the daemon acked its turn and before it wrote the ack down: the ack is lost with the crash.
	t.Setenv("HIVE_WF_TEST_PAUSE", "after-prompt:6000")
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	run := f.startRun(pid, oneAgent("lostack"))
	f.waitGate(dir) // the turn is running in the daemon
	f.killPlugin()
	f.waitReady()

	f.submitFor("a", "adopted turn")
	releaseTurn(dir)
	f.waitRun(run, "done", wait)
	if n := len(fakeCalls(dir, "session/prompt")); n != 1 {
		t.Errorf("%d PROMPT_ACPs reached the adapter, want exactly 1: the transcript's turn is adopted, never sent twice", n)
	}
	if state, attempt := f.nodeState(run, "a"); state != "done" || attempt != 1 {
		t.Errorf("node a = %s attempt %d, want done attempt 1", state, attempt)
	}
	if got := f.nodeInfo(run, "a"); got["error"] != nil {
		t.Errorf("node a error = %v", got["error"])
	}
}

// The Manager marks a plugin that exits more than five times in a minute `failed` and stops restarting it. A run
// in progress stays in runs.db, and enabling the plugin again picks it up where it was: the agent's turn, held through
// every kill, is still running at attempt 1 when the engine comes back.
func TestDurability_CrashLoopFailedThenRestart_RunsResume(t *testing.T) {
	if testing.Short() {
		t.Skip("kills the plugin six times through the Manager's real backoff (about 32 s)")
	}
	// The loop outlasts the fake's default 30 s hold on a turn.
	f, dir := newAgentFixture(t, acptest.FlagGate, acptest.FlagGateLong)
	pid, _ := f.project()
	run := f.startRun(pid, oneAgent("loop"))
	f.waitGate(dir)

	for i := 0; i < 6; i++ {
		f.killPlugin()
		if i < 5 {
			f.waitReady()
		}
	}
	f.eventually(2*time.Minute, "the plugin to be marked failed", func() bool {
		for _, p := range f.d.Plugins().List() {
			if p.ID == pluginID {
				return p.Status == wire.PluginFailed
			}
		}
		return false
	})

	f.setEnabled(true)
	if state, attempt := f.nodeState(run, "a"); state != "running" || attempt != 1 {
		t.Fatalf("node a = %s attempt %d after the loop, want it still running attempt 1", state, attempt)
	}
	f.submitFor("a", "after the loop")
	releaseTurn(dir)
	f.waitRun(run, "done", wait)
	if state, attempt := f.nodeState(run, "a"); state != "done" || attempt != 1 {
		t.Errorf("node a = %s attempt %d, want done attempt 1", state, attempt)
	}
	if n := len(fakeCalls(dir, "session/prompt")); n != 1 {
		t.Errorf("%d prompts, want 1 (calls %v; turns %q)", n, acptest.Calls(dir), userTurns(t, dir))
	}
	if n := len(fakeCalls(dir, "session/new")); n != 1 {
		t.Errorf("%d sessions made, want 1", n)
	}
}
