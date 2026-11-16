// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package workflowe2e

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/acp/acptest"
	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
)

func TestMode_ExplicitLeastPermissiveWhenOmitted(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	f.onTurns(dir, func(tc *turn) { tc.submit(map[string]any{"summary": "ok"}) })
	run := f.startRun(pid, ir("mode", []any{agentNode("a", "p", nil), agentNode("b", "p", map[string]any{"mode": "default"})}, edge("a", "b")))
	f.waitRun(run, "done", wait)
	calls := fakeCalls(dir, "session/set_mode ")
	if len(calls) != 2 || !strings.HasSuffix(calls[0], " plan") || !strings.HasSuffix(calls[1], " default") {
		t.Errorf("session/set_mode = %q, want plan (Modes[0], never the ceiling) then default as named", calls)
	}
}

func TestMode_AboveCeilingRefusedNamesCeiling(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	out := f.rpc("test.start-run", map[string]any{"project_id": pid, "ir": ir("hot", []any{agentNode("a", "p", map[string]any{"mode": "acceptEdits"})}), "input": map[string]any{}})
	e, _ := out["error"].(map[string]any)
	if out["ok"] == true || !strings.Contains(e["message"].(string), `mode "acceptEdits" is above the "default" ceiling set for agent "claude"`) {
		t.Fatalf("reply = %v, want a refusal naming the ceiling", out)
	}
	if runs, _ := f.ok("runs.list", map[string]any{})["runs"].([]any); len(runs) != 0 {
		t.Errorf("%d runs exist after a refused start", len(runs))
	}
	if len(fakeCalls(dir, "session/new")) != 0 {
		t.Error("an adapter was started for a refused run")
	}
}

func TestCatalog_CeilingLoweredBetweenBuildAndStartRefusedBeforeRun(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	// The user raised the claude ceiling, built a workflow that needs it, then lowered it again before running.
	agent.SetCustomDir(f.stateDir)
	t.Cleanup(func() { agent.SetCustomDir("") })
	save := func(ceiling string) {
		t.Helper()
		if err := agent.SaveSettings(agent.Settings{ACPModeCeiling: map[string]string{"claude": ceiling}}); err != nil {
			t.Fatal(err)
		}
	}
	hot := ir("hot", []any{agentNode("a", "p", map[string]any{"mode": "acceptEdits"})})
	f.onTurns(dir, func(tc *turn) { tc.submit(map[string]any{"summary": "ok"}) })
	save("acceptEdits")
	f.waitRun(f.startRun(pid, hot), "done", wait)

	save("plan")
	out := f.rpc("test.start-run", map[string]any{"project_id": pid, "ir": hot, "input": map[string]any{}})
	e, _ := out["error"].(map[string]any)
	if out["ok"] == true || !strings.Contains(e["message"].(string), `above the "plan" ceiling`) {
		t.Fatalf("reply = %v, want a refusal naming the lowered ceiling", out)
	}
	if n := len(fakeCalls(dir, "session/new")); n != 1 {
		t.Errorf("%d adapters were started, want only the first run's", n)
	}
}

// ----------------------------------------------------------- provenance, trust

func TestProvenance_SessionsCarryRunLabel(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, _ := f.project()
	f.onTurns(dir, func(tc *turn) { tc.submit(map[string]any{"summary": "ok"}) })
	run := f.startRun(pid, ir("prov", []any{agentNode("a", "p", nil)}))
	f.waitRun(run, "done", wait)
	want := "plugin:" + pluginID + "/workflow:" + run
	var checked int
	reg := f.d.Registry()
	for _, s := range reg.List() {
		e := reg.Get(s.ID)
		if e == nil || e.SpawnedBy != want {
			continue
		}
		checked++
		msg, err := reg.AcpTranscript(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range msg.Items {
			if it.Kind == wire.AcpItemUser && it.Origin != want {
				t.Errorf("turn %q has origin %q, want %q", it.Text, it.Origin, want)
			}
		}
	}
	if checked != 1 {
		t.Errorf("%d sessions carry %q, want 1", checked, want)
	}
}

func TestNodesOnlyRunInProjectWorktrees(t *testing.T) {
	f, dir := newAgentFixture(t, acptest.FlagGate)
	pid, repo := f.project()
	f.onTurns(dir, func(tc *turn) { tc.submit(map[string]any{"summary": "ok"}) })
	run := f.startRun(pid, ir("trust", []any{agentNode("a", "p", nil), agentNode("b", "p", sameTree("a"))}, edge("a", "b")))
	f.waitRun(run, "done", wait)
	listed := git(t, repo, "worktree", "list", "--porcelain")
	var n int
	for _, s := range f.d.Registry().List() {
		if s.Agent == "" {
			continue
		}
		n++
		if s.ProjectID != pid || s.WorktreePath == "" || !strings.Contains(listed, "worktree "+s.WorktreePath+"\n") {
			t.Errorf("session %s: project %q worktree %q is not a worktree of the run's project:\n%s", s.Name, s.ProjectID, s.WorktreePath, listed)
		}
		if strings.HasPrefix(s.WorktreePath, repo+string(filepath.Separator)) && !strings.Contains(s.WorktreePath, ".worktrees") {
			t.Errorf("session %s runs inside the project's own checkout: %s", s.Name, s.WorktreePath)
		}
	}
	if n != 2 {
		t.Errorf("%d engine sessions, want 2", n)
	}
}
