// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package workflowe2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/lucascaro/hive/internal/acp/acptest"
)

// seen records what an agent found in its worktree when its turn started.
type seen struct {
	mu sync.Mutex
	m  map[string]map[string]bool // node key -> file -> present
	wt map[string]string          // node key -> worktree path
}

func newSeen() *seen { return &seen{m: map[string]map[string]bool{}, wt: map[string]string{}} }

func (s *seen) note(tc *turn, files ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := tc.node()
	s.m[k] = map[string]bool{}
	for _, f := range files {
		s.m[k][f] = tc.exists(f)
	}
	s.wt[k] = tc.worktree()
}

func (s *seen) has(key, file string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[key][file]
}

func (s *seen) path(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wt[key]
}

func TestWorktree_SequenceBranchesFromUpstream(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, repo := f.project()
	got := newSeen()
	f.onTurns(dir, func(tc *turn) {
		got.note(tc, "a.txt")
		tc.write(tc.node()+".txt", "x")
		tc.submit(map[string]any{"summary": "ok"})
	})
	run := f.startRun(pid, ir("seq", []any{agentNode("a", "p", nil), agentNode("b", "p", nil)}, edge("a", "b")))
	f.waitRun(run, "done", wait)
	if got.has("a", "a.txt") || !got.has("b", "a.txt") {
		t.Errorf("a saw a.txt: %v, b saw a.txt: %v; want false and true (b starts from a's commit)", got.has("a", "a.txt"), got.has("b", "a.txt"))
	}
	if got.path("a") == got.path("b") {
		t.Errorf("a and b share the worktree %s; worktree \"own\" gives each its own", got.path("a"))
	}
	if !treeHas(t, repo, "hive/wf/"+run+"/b", "a.txt") || treeHas(t, repo, "hive/wf/"+run+"/a", "b.txt") {
		t.Error("b's branch must hold a's work and a's must not hold b's")
	}
}

func TestWorktree_OfSharesPath(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, repo := f.project()
	got := newSeen()
	f.onTurns(dir, func(tc *turn) {
		got.note(tc, "a.txt")
		tc.write(tc.node()+".txt", "x")
		tc.submit(map[string]any{"summary": "ok"})
	})
	run := f.startRun(pid, ir("of", []any{agentNode("a", "p", nil), agentNode("b", "p", sameTree("a"))}, edge("a", "b")))
	f.waitRun(run, "done", wait)
	if got.path("a") == "" || got.path("a") != got.path("b") {
		t.Errorf("worktrees %q and %q, want one shared path", got.path("a"), got.path("b"))
	}
	if !got.has("b", "a.txt") {
		t.Error("b did not see a's file in the shared worktree")
	}
	a, b := f.nodeInfo(run, "a"), f.nodeInfo(run, "b")
	if a["branch"] != b["branch"] || a["session_id"] == b["session_id"] {
		t.Errorf("branches %v / %v, sessions %v / %v: want one branch and two sessions (no two nodes share a session)", a["branch"], b["branch"], a["session_id"], b["session_id"])
	}
	if got := git(t, repo, "log", "--format=%s", "hive/wf/"+run+"/a"); !strings.Contains(got, "wf: a (attempt 1)") || !strings.Contains(got, "wf: b (attempt 1)") {
		t.Errorf("the shared branch's log is %q, want both nodes' commits", got)
	}
	if n := len(branches(t, repo, run)); n != 1 {
		t.Errorf("%d branches, want 1: the sharing node makes none of its own", n)
	}
}

func reviewLoopIR(iters int) map[string]any {
	loop := map[string]any{
		"id": "fix", "kind": "loop", "max_iters": iters,
		"until": map[string]any{"ref": "fix/review.verdict", "op": "eq", "value": "approve"},
		"nodes": []any{
			agentNode("fix/work", "work", nil),
			agentNode("fix/review", "review", map[string]any{"worktree": map[string]any{"of": "fix/work"}, "output": verdictOut}),
		},
		"edges": []any{edge("fix/work", "fix/review")},
	}
	return ir("loop", []any{loop})
}

func TestWorktree_LoopIterationBranchesFromPrevious(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, repo := f.project()
	got := newSeen()
	f.onTurns(dir, func(tc *turn) {
		switch k := tc.node(); k {
		case "fix[1]/work", "fix[2]/work":
			got.note(tc, "w1.txt")
			tc.write("w"+k[4:5]+".txt", "x")
			tc.submit(map[string]any{"summary": "ok"})
		case "fix[1]/review":
			tc.submit(map[string]any{"verdict": "revise", "feedback": "again"})
		default:
			tc.submit(map[string]any{"verdict": "approve", "feedback": "good"})
		}
	})
	run := f.startRun(pid, reviewLoopIR(3))
	f.waitRun(run, "done", wait)
	if got.has("fix[1]/work", "w1.txt") || !got.has("fix[2]/work", "w1.txt") {
		t.Errorf("iteration 1 saw w1.txt: %v, iteration 2: %v; want false and true (the worker revises its own change)", got.has("fix[1]/work", "w1.txt"), got.has("fix[2]/work", "w1.txt"))
	}
	for _, name := range []string{"fix_1/work", "fix_2/work"} {
		if !slices.Contains(branches(t, repo, run), "hive/wf/"+run+"/"+name) {
			t.Errorf("branches = %v, want hive/wf/%s/%s", branches(t, repo, run), run, name)
		}
	}
	if !treeHas(t, repo, "hive/wf/"+run+"/fix_2/work", "w1.txt") || !treeHas(t, repo, "hive/wf/"+run+"/fix_2/work", "w2.txt") {
		t.Error("iteration 2's branch must hold iteration 1's work and its own")
	}
	if treeHas(t, repo, "hive/wf/"+run+"/fix_1/work", "w2.txt") {
		t.Error("iteration 1's branch must not move when iteration 2 works")
	}
	if got := git(t, repo, "log", "--format=%s", "hive/wf/"+run+"/fix_2/work"); !strings.Contains(got, "wf: fix[2]/work (attempt 1)") {
		t.Errorf("iteration 2's log = %q, want its commit named by the instance key", got)
	}
}

// mapIR is split -> map each (agent do) -> join.
func mapIR() map[string]any {
	each := map[string]any{"id": "each", "kind": "map", "over": "split.tasks", "concurrency": 2,
		"nodes": []any{agentNode("each/do", "do {{$item}}", nil)}, "edges": []any{}}
	return ir("fan", []any{agentNode("split", "split", map[string]any{"output": tasksOut}), each, agentNode("join", "join", nil)},
		edge("split", "each"), edge("each", "join"))
}

// mapTurns plays the map's agents: copies write a file each (the same one when they are to conflict), the join records what it sees.
func mapTurns(f *fixture, dir string, n int, conflict bool, joined *seen) {
	f.onTurns(dir, func(tc *turn) {
		switch k := tc.node(); {
		case k == "split":
			tasks := make([]any, n)
			for i := range tasks {
				tasks[i] = string(rune('a' + i))
			}
			tc.submit(map[string]any{"tasks": tasks})
		case k == "join":
			files, _ := filepath.Glob(filepath.Join(tc.worktree(), "copy-*.txt"))
			joined.mu.Lock()
			joined.m["join"] = map[string]bool{}
			for _, p := range files {
				joined.m["join"][filepath.Base(p)] = true
			}
			joined.mu.Unlock()
			tc.submit(map[string]any{"summary": "joined"})
		default:
			if conflict {
				tc.write("same.txt", k)
			} else {
				tc.write("copy-"+strings.NewReplacer("[", "", "]", "", "/", "-").Replace(k)+".txt", k)
			}
			tc.submit(map[string]any{"summary": "ok"})
		}
	})
}

func TestMerge_AfterMap(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, repo := f.project()
	joined := newSeen()
	mapTurns(f, dir, 3, false, joined)
	run := f.startRun(pid, mapIR())
	f.waitRun(run, "done", wait)
	if len(joined.m["join"]) != 3 {
		t.Errorf("the join saw %v, want all three copies' files merged", joined.m["join"])
	}
	// Every copy starts from what is upstream of the map, not from a sibling.
	for i := range 3 {
		b := "hive/wf/" + run + "/each_" + itoa(i) + "/do"
		if !slices.Contains(branches(t, repo, run), b) {
			t.Errorf("no branch %s in %v", b, branches(t, repo, run))
		}
	}
	if treeHas(t, repo, "hive/wf/"+run+"/each_1/do", "copy-each0-do.txt") {
		t.Error("copy 1's branch holds copy 0's file: copies must not see each other")
	}
	if !treeHas(t, repo, "hive/wf/"+run+"/join", "copy-each2-do.txt") {
		t.Error("the join's branch is missing a copy's work")
	}
}

func TestBranchNames_EncodedKeys(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, repo := f.project()
	f.onTurns(dir, func(tc *turn) {
		if tc.node() == "split" {
			tc.submit(map[string]any{"tasks": []any{"a", "b", "c", "d"}})
			return
		}
		tc.submit(map[string]any{"summary": "ok"})
	})
	each := map[string]any{"id": "work/each", "kind": "map", "over": "split.tasks", "concurrency": 4,
		"nodes": []any{agentNode("work/each/do", "do {{$item}}", nil)}, "edges": []any{}}
	group := map[string]any{"id": "work", "kind": "group", "nodes": []any{each}, "edges": []any{}}
	run := f.startRun(pid, ir("names", []any{agentNode("split", "s", map[string]any{"output": tasksOut}), group}, edge("split", "work")))
	f.waitRun(run, "done", wait)
	if !slices.Contains(branches(t, repo, run), "hive/wf/"+run+"/work/each_3/do") {
		t.Errorf("branches = %v, want hive/wf/%s/work/each_3/do", branches(t, repo, run), run)
	}
}

// conflicted runs the map with copies that write the same file differently, and returns once the run has failed.
func conflicted(t *testing.T) (f *fixture, run, repo, dir string, joined *seen) {
	t.Helper()
	f, dir = newAgentFixture(t, acptest.FlagGate)
	pid, repo := f.project()
	joined = newSeen()
	mapTurns(f, dir, 2, true, joined)
	run = f.startRun(pid, mapIR())
	f.waitRun(run, "failed", wait)
	return f, run, repo, dir, joined
}

func TestMerge_ConflictFailsWithRecoveryPath(t *testing.T) {
	f, run, _, dir, _ := conflicted(t)
	info := f.nodeInfo(run, "join")
	c, _ := info["conflict"].(map[string]any)
	if info["state"] != "failed" || c == nil {
		t.Fatalf("join = %v, want failed with a conflict", info)
	}
	wt, _ := c["worktree"].(string)
	files, _ := c["files"].([]any)
	if !strings.HasPrefix(c["branch"].(string), "hive/wf/"+run+"/each_") || len(files) != 1 || files[0] != "same.txt" {
		t.Errorf("conflict = %v, want a copy's branch and same.txt", c)
	}
	if e, _ := info["error"].(string); !strings.Contains(e, "conflicted in same.txt") || !strings.Contains(e, wt) || !strings.Contains(e, "Resolve and commit there, then resume the run.") {
		t.Errorf("error = %q, want the recovery message naming the files and %s", e, wt)
	}
	if st, err := os.Stat(wt); err != nil || !st.IsDir() {
		t.Errorf("the merge worktree %q was not kept: %v", wt, err)
	}
	if n := len(fakeCalls(dir, "session/prompt")); n != 3 {
		t.Errorf("%d prompts, want 3 (split and the two copies); the join never started", n)
	}
	if e := f.runError(run); !strings.Contains(e, "join") {
		t.Errorf("run error = %q, want it to name the join", e)
	}
}

func TestRunRetry_ResumesFailedRunAfterConflictResolved(t *testing.T) {
	f, run, _, _, joined := conflicted(t)
	c, _ := f.nodeInfo(run, "join")["conflict"].(map[string]any)
	wt := c["worktree"].(string)
	// The daemon aborted the conflicting merge, so the kept worktree is clean at the base. The user redoes the merge there,
	// resolves it and commits, which is what the node's message asks for.
	_ = exec.Command("git", "-C", wt, "merge", "--no-ff", "--no-edit", c["branch"].(string)).Run() // stops on the conflict
	if err := os.WriteFile(filepath.Join(wt, "same.txt"), []byte("resolved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, wt, "add", "same.txt")
	git(t, wt, "commit", "-q", "--no-edit")
	f.ok("run.retry", map[string]any{"run_id": run})
	f.waitRun(run, "done", wait)
	if state, attempt := f.nodeState(run, "join"); state != "done" || attempt != 1 {
		t.Errorf("join = %s attempt %d, want done attempt 1 (a retry clears attempts)", state, attempt)
	}
	if _, ok := joined.m["join"]; !ok {
		t.Error("the join never ran after the retry")
	}
	if got := f.nodeInfo(run, "join")["worktree"]; got != wt {
		t.Errorf("the join ran in %v, want the kept worktree %s: re-preparing re-attaches", got, wt)
	}
}
