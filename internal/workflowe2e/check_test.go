// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package workflowe2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// sleepOnce is a check that records its pid and sleeps the first time it is
// run, and exits 0 at once every time after: the shape of "a check that was
// interrupted, then retried".
func (f *fixture) sleepOnce(dir string) []string {
	marker, pidfile := filepath.Join(dir, "ran-once"), filepath.Join(dir, "pid")
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	// The marker is made before the pid is published, so a test that sees the pid knows the first attempt is committed to sleeping.
	return f.nodeScript("const fs=require('fs');const first=!fs.existsSync(" + q(marker) + ");if(first)fs.writeFileSync(" + q(marker) + ",'1');" +
		"fs.writeFileSync(" + q(pidfile) + ",String(process.pid));if(!first)process.exit(0);setInterval(()=>{},1000)")
}

func (f *fixture) checkPid(dir string) int {
	f.t.Helper()
	var pid int
	f.eventually(30*time.Second, "the check to record its pid", func() bool {
		b, err := os.ReadFile(filepath.Join(dir, "pid"))
		if err != nil {
			return false
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(b)))
		pid = n
		return err == nil
	})
	return pid
}

func TestCheckNode_PluginCrash_InterruptedAndRetried(t *testing.T) {
	f := newFixture(t)
	pid, _ := f.project()
	dir := t.TempDir()
	run := f.startRun(pid, ir("sleeper", []any{check("c", f.sleepOnce(dir)...)}))
	f.waitNode(run, "c", "running", 30*time.Second)
	first := f.checkPid(dir)

	// The plugin dies with no cleanup. The check is in its own process
	// group, so it outlives the plugin.
	enginePid := int(f.ok("test.pid", nil)["pid"].(float64))
	if err := kill9(enginePid); err != nil {
		t.Fatal(err)
	}
	f.waitReady() // the Manager restarts it; its startup reaps the survivor and retries the node

	f.waitRun(run, "done", 60*time.Second)
	if state, attempt := f.nodeState(run, "c"); state != "done" || attempt != 2 {
		t.Errorf("node c = %s attempt %d, want done attempt 2", state, attempt)
	}
	if alive(first) {
		t.Errorf("the first attempt's process %d is still running: it should have been reaped", first)
	}
	if second := f.checkPid(dir); second == first {
		t.Errorf("attempt 2 reused pid %d", first)
	}
}

func TestCheckNode_HivedRestart_InterruptedAndRetried(t *testing.T) {
	f := newFixture(t)
	pid, _ := f.project()
	dir := t.TempDir()
	run := f.startRun(pid, ir("sleeper", []any{check("c", f.sleepOnce(dir)...)}))
	f.waitNode(run, "c", "running", 30*time.Second)
	first := f.checkPid(dir)

	// A normal stop: the plugin gets SIGTERM and takes its checks with it, then hived is started again.
	f.restart()

	f.waitRun(run, "done", 60*time.Second)
	if state, attempt := f.nodeState(run, "c"); state != "done" || attempt != 2 {
		t.Errorf("node c = %s attempt %d, want done attempt 2", state, attempt)
	}
	if alive(first) {
		t.Errorf("the first attempt's process %d survived the restart", first)
	}
}

func TestHumanNode_SurvivesRestart(t *testing.T) {
	f := newFixture(t)
	pid, _ := f.project()
	run := f.startRun(pid, ir("ask", []any{human("ask", "Ship it?")}))
	f.waitNode(run, "ask", "waiting", 30*time.Second)
	before := f.ok("node.get", map[string]any{"run_id": run, "key": "ask"})

	f.restart()

	waiting, _ := f.ok("runs.list", map[string]any{"state": "waiting"})["runs"].([]any)
	if len(waiting) != 1 || waiting[0].(map[string]any)["run_id"] != run {
		t.Fatalf("runs.list waiting = %v, want just %s", waiting, run)
	}
	after := f.ok("node.get", map[string]any{"run_id": run, "key": "ask"})
	delete(before, "nonce")
	delete(after, "nonce")
	b1, _ := json.Marshal(before)
	b2, _ := json.Marshal(after)
	if string(b1) != string(b2) {
		t.Errorf("the question changed across the restart:\n before %s\n after  %s", b1, b2)
	}
	if out := f.rpc("human.answer", map[string]any{"run_id": run, "key": "ask", "answer": map[string]any{"yes": "no"}}); out["ok"] != false {
		t.Errorf("an answer of the wrong type was accepted: %v", out)
	}
	f.ok("human.answer", map[string]any{"run_id": run, "key": "ask", "answer": map[string]any{"yes": true}})
	f.waitRun(run, "done", 30*time.Second)
}

// A project whose repo has no commit cannot give a worktree a base: PREPARE
// answers with an error, and the run must fail with it promptly rather than
// wait out the request timeout.
func TestCheckNode_EmptyRepo_FailsPromptly(t *testing.T) {
	f := newFixture(t)
	pid, _ := f.projectWith(false)
	run := f.startRun(pid, ir("empty", []any{check("c", "true")}))
	f.waitRun(run, "failed", 20*time.Second)
	if state, _ := f.nodeState(run, "c"); state != "failed" {
		t.Errorf("node c = %s, want failed", state)
	}
}

// B-4: a check with no upstream head runs in a clean worktree of its own,
// on hive/wf/<run>/<key>, at the base the run pinned with its first PREPARE.
func TestCheckNode_NoUpstreamHead_RunsInBaseWorktree(t *testing.T) {
	f := newFixture(t)
	projectID, repo := f.project()
	// The user's uncommitted edit: it must not be in any run worktree.
	if err := os.WriteFile(filepath.Join(repo, "uncommitted.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := git(t, repo, "rev-parse", "HEAD")
	reports := t.TempDir()
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	// What the check saw: its cwd, its branch and HEAD, and whether the uncommitted file was there.
	report := func(name string) []string {
		return f.nodeScript("const fs=require('fs'),cp=require('child_process');const g=a=>cp.execFileSync('git',a,{encoding:'utf8'}).trim();" +
			"fs.writeFileSync(" + q(filepath.Join(reports, name)) + ",JSON.stringify({cwd:process.cwd(),branch:g(['symbolic-ref','--short','HEAD']),head:g(['rev-parse','HEAD']),has:fs.existsSync('uncommitted.txt')}))")
	}
	run := f.startRun(projectID, ir("roots",
		[]any{check("root", report("root")...), human("gate", "Go on?"), check("after", report("after")...)},
		edge("gate", "after")))

	f.waitNode(run, "root", "done", 30*time.Second)
	f.waitNode(run, "gate", "waiting", 30*time.Second)
	// The project moves on while the run waits for its human: the second check must still start from the run's base.
	if err := os.WriteFile(filepath.Join(repo, "later.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "later.txt")
	git(t, repo, "commit", "-q", "-m", "two")
	f.ok("human.answer", map[string]any{"run_id": run, "key": "gate", "answer": map[string]any{"yes": true}})
	f.waitRun(run, "done", 30*time.Second)

	realRepo, _ := filepath.EvalSymlinks(repo)
	for _, name := range []string{"root", "after"} {
		b, err := os.ReadFile(filepath.Join(reports, name))
		if err != nil {
			t.Fatalf("%s left no report: %v", name, err)
		}
		var r struct {
			Cwd, Branch, Head string
			Has               bool
		}
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		realCwd, _ := filepath.EvalSymlinks(r.Cwd)
		if realCwd == realRepo || !strings.HasPrefix(realCwd, filepath.Join(realRepo, ".worktrees")+string(filepath.Separator)) {
			t.Errorf("%s ran in %s: want a worktree under %s/.worktrees, never the project's checkout", name, r.Cwd, realRepo)
		}
		if want := "hive/wf/" + run + "/" + name; r.Branch != want {
			t.Errorf("%s ran on branch %q, want %q", name, r.Branch, want)
		}
		if r.Head != base {
			t.Errorf("%s ran at %s, want the run's pinned base %s", name, r.Head, base)
		}
		if r.Has {
			t.Errorf("%s saw the user's uncommitted file: it ran in the main checkout's state", name)
		}
	}
}
