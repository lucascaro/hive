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

// userTurns are the prompts the fake adapter received, in order: its store keeps one {user, agent} line per turn.
func userTurns(t *testing.T, dir string) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "fake-*.jsonl"))
	var out []string
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if line == "" {
				continue
			}
			var turn acptest.Turn
			if err := json.Unmarshal([]byte(line), &turn); err != nil {
				t.Fatal(err)
			}
			out = append(out, turn.User)
		}
	}
	return out
}

// ------------------------------------------------------------------------ F3

func TestF3_EndTurnWithoutResult_RepromptedOnce(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	f.onTurns(dir, func(tc *turn) {
		if tc.n == 2 {
			tc.submit(map[string]any{"summary": "second try"})
		}
	})
	run := f.startRun(pid, ir("f3", []any{agentNode("a", "do it", nil)}))
	f.waitRun(run, "done", wait)
	turns := userTurns(t, dir)
	if len(turns) != 2 || turns[0] != "do it" || turns[1] != "You ended the turn without calling submit_result. Call it now with your result." {
		t.Errorf("prompts = %q, want the task, then one re-prompt", turns)
	}
	if got := f.nodeOutput(run, "a")["summary"]; got != "second try" {
		t.Errorf("output = %v", got)
	}
}

func TestF3_SecondMissFails(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	f.onTurns(dir, func(*turn) {})
	run := f.startRun(pid, ir("f3", []any{agentNode("a", "do it", nil)}))
	f.waitRun(run, "failed", wait)
	if n := len(userTurns(t, dir)); n != 2 {
		t.Errorf("%d prompts, want 2: one re-prompt and no more", n)
	}
	if e := f.runError(run); !strings.Contains(e, "submit_result") {
		t.Errorf("run error = %q, want it to name submit_result", e)
	}
}

// ---------------------------------------------------------------- permissions

// permissionRun starts a one-agent run whose first turn asks for a permission, and returns once the node is waiting.
func permissionRun(t *testing.T) (f *fixture, run string, hiveID string) {
	t.Helper()
	f, _ = newAgentFixture(t, acptest.FlagPermission)
	pid, _ := f.project()
	run = f.startRun(pid, ir("perm", []any{agentNode("a", "do it", nil)}))
	f.waitNode(run, "a", "waiting", wait)
	for _, s := range f.d.Registry().List() {
		hiveID = s.ID
	}
	return f, run, hiveID
}

func answerPermission(t *testing.T, f *fixture, run, hiveID string) {
	t.Helper()
	w, _ := f.nodeInfo(run, "a")["waiting"].(map[string]any)
	// The agent submits while its turn is blocked on the permission, as it would after the user allowed a tool.
	reg := f.d.Registry()
	if err := reg.SubmitResult(hiveID, reg.ACPNonceForTest(hiveID), []byte(`{"status":"ok","data":{"summary":"allowed"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := f.c.AnswerPermission(wire.AnswerPermissionReq{SessionID: hiveID, RequestID: w["request_id"].(string), OptionID: "allow"}); err != nil {
		t.Fatal(err)
	}
}

func TestPermission_BecomesNodeWaiting(t *testing.T) {
	f, run, hiveID := permissionRun(t)
	w, _ := f.nodeInfo(run, "a")["waiting"].(map[string]any)
	if w["reason"] != "permission" || w["session_id"] != hiveID || w["request_id"] == "" || w["request_id"] == nil {
		t.Fatalf("waiting = %v, want a permission wait carrying the session and request ids", w)
	}
	opts, _ := w["options"].([]any)
	if len(opts) != 2 || opts[0].(map[string]any)["option_id"] != "allow" {
		t.Errorf("options = %v", opts)
	}
	if got := f.runState(run); got != "waiting" {
		t.Errorf("run is %s, want waiting", got)
	}
	answerPermission(t, f, run, hiveID)
	f.waitRun(run, "done", wait)
}

func TestPermission_WaitEmitsNodeResumedAfterAnswer(t *testing.T) {
	f, run, hiveID := permissionRun(t)
	answerPermission(t, f, run, hiveID)
	f.waitRun(run, "done", wait)
	waiting, resumed, finished := -1, -1, -1
	for i, ty := range f.eventTypes(run, "a") {
		switch ty {
		case "node.waiting":
			waiting = i
		case "node.resumed":
			resumed = i
		case "node.finished":
			finished = i
		}
	}
	if !(waiting >= 0 && waiting < resumed && resumed < finished) {
		t.Errorf("event order waiting=%d resumed=%d finished=%d, want waiting < resumed < finished", waiting, resumed, finished)
	}
}

// ------------------------------------------------------------ cancel, timeout

func TestCancel_WaitsForCancelledThenKills(t *testing.T) {
	// The plugin is its own process: the 10 s grace is shortened through its environment, honoured only with the test seam on.
	t.Setenv("HIVE_WF_CANCEL_GRACE_MS", "300")
	f, dir := newAgentFixture(t, acptest.FlagGate, acptest.FlagSlowCancel)
	pid, _ := f.project()
	// The agent has work in progress when it is stopped: an unforced kill would refuse, and the work must survive the forced one.
	f.onTurns(dir, func(tc *turn) {
		tc.write("wip.txt", "half done")
		tc.Hold()
	})
	run := f.startRun(pid, ir("cancel", []any{agentNode("a", "do it", nil)}))
	f.eventually(wait, "the turn to start", func() bool {
		files, _ := filepath.Glob(filepath.Join(dir, "turn-*.waiting"))
		return len(files) == 1
	})
	wt, _ := f.nodeInfo(run, "a")["worktree"].(string)
	f.ok("run.cancel", map[string]any{"run_id": run})
	// The fake ignores the cancel until released, so only the kill can end the session.
	f.eventually(30*time.Second, "the session to be killed", func() bool {
		for _, s := range f.d.Registry().List() {
			if strings.HasPrefix(s.Name, "cancel ") {
				return false
			}
		}
		return true
	})
	t.Cleanup(func() { _ = acptest.ReleaseCancel(dir) })
	if got := fakeCalls(dir, "session/cancel"); len(got) != 1 {
		t.Errorf("session/cancel calls = %v, want one", got)
	}
	if _, err := os.Stat(filepath.Join(wt, "wip.txt")); err != nil {
		t.Errorf("the agent's unfinished work in %q was not kept: %v", wt, err)
	}
}

func TestTimeout_FailsNodeNamingTimeout(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	run := f.startRun(pid, ir("slow", []any{agentNode("a", "do it", map[string]any{"timeout_s": 1})}))
	f.waitRun(run, "failed", wait)
	if e := f.runError(run); !strings.Contains(e, "timed out after 1s") {
		t.Errorf("run error = %q, want it to name the timeout", e)
	}
	if got := fakeCalls(dir, "session/cancel"); len(got) != 1 {
		t.Errorf("session/cancel calls = %v, want one", got)
	}
}
