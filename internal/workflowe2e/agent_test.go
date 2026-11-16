// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package workflowe2e

import (
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/acp/acptest"
)

const wait = 60 * time.Second

func TestAgentNode_ResultRecorded(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, repo := f.project()
	f.onTurns(dir, func(tc *turn) {
		tc.write("out.txt", "hello\n")
		tc.submit(map[string]any{"summary": "wrote out.txt"})
	})
	run := f.startRun(pid, ir("one", []any{agentNode("a", "write out.txt", nil)}))
	f.waitRun(run, "done", wait)

	if got := f.nodeOutput(run, "a")["summary"]; got != "wrote out.txt" {
		t.Errorf("output summary = %v", got)
	}
	info := f.nodeInfo(run, "a")
	if info["session_id"] == nil || info["branch"] != "hive/wf/"+run+"/a" || info["worktree"] == nil {
		t.Errorf("node.get = %v, want session_id, branch hive/wf/%s/a and a worktree", info, run)
	}
	// The work was committed to the node's branch before the node was done (R1).
	if !treeHas(t, repo, "hive/wf/"+run+"/a", "out.txt") {
		t.Error("out.txt is not on the node's branch")
	}
	if msg := git(t, repo, "log", "-1", "--format=%s", "hive/wf/"+run+"/a"); msg != "wf: a (attempt 1)" {
		t.Errorf("commit message = %q", msg)
	}
	types := f.eventTypes(run, "a")
	if n := len(types); n < 2 || types[n-2] != "node.output" || types[n-1] != "node.finished" {
		t.Errorf("events = %v, want ... node.output node.finished", types)
	}
	if got := fakeCalls(dir, "session/prompt"); len(got) != 1 {
		t.Errorf("prompts = %v, want exactly one", got)
	}
}
