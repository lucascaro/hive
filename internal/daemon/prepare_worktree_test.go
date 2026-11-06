// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package daemon

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

// PREPARE_WORKTREE / COMMIT_WORKTREE (spec 528).

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// wfProject makes a git repo with two commits on main and a project on
// it, and returns the project id, the repo, and the two commits' SHAs.
func wfProject(t *testing.T, d *Daemon) (projectID, repo, first, second string) {
	t.Helper()
	// The test machine's git config must not bring identity, hooks or
	// signing into the daemon's git calls.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo = t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "config", "user.name", "Test User")
	gitIn(t, repo, "config", "user.email", "test@example.com")
	gitIn(t, repo, "commit", "--allow-empty", "-q", "-m", "one")
	first = gitIn(t, repo, "rev-parse", "HEAD")
	gitIn(t, repo, "commit", "--allow-empty", "-q", "-m", "two")
	second = gitIn(t, repo, "rev-parse", "HEAD")
	p, err := d.Registry().CreateProject(wire.CreateProjectReq{Name: "wf", Cwd: repo})
	if err != nil {
		t.Fatal(err)
	}
	return p.ID, repo, first, second
}

// prepare sends PREPARE_WORKTREE with a req_id and returns the reply.
func prepare(t *testing.T, c net.Conn, req wire.PrepareWorktreeReq) (wire.PreparedWorktreeMsg, *wire.Error) {
	t.Helper()
	req.ReqID = 41
	send(t, c, wire.FramePrepareWorktree, req)
	f := readFrameWhere(t, c, "PREPARED_WORKTREE or ERROR", func(f frame) bool {
		return f.ft == wire.FramePreparedWorktree || f.ft == wire.FrameError
	})
	if f.ft == wire.FrameError {
		e := f.err(t)
		if e.ReqID != 41 {
			t.Errorf("ERROR %+v does not echo req_id 41", e)
		}
		return wire.PreparedWorktreeMsg{}, &e
	}
	var m wire.PreparedWorktreeMsg
	if err := json.Unmarshal(f.payload, &m); err != nil {
		t.Fatal(err)
	}
	if m.ReqID != 41 {
		t.Errorf("PREPARED_WORKTREE req_id = %d, want 41", m.ReqID)
	}
	return m, nil
}

func TestPrepareWorktree_FromArbitraryBase(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := pluginTestControl(t, d, "hivegui/test")
	pid, repo, first, second := wfProject(t, d)
	gitIn(t, repo, "tag", "v1", first)

	for i, tc := range []struct{ base, want string }{
		{"v1", first}, {first[:12], first}, {second, second}, {"main", second}, {"HEAD~1", first},
	} {
		branch := "hive/wf/run-1/n" + string(rune('a'+i))
		m, e := prepare(t, c, wire.PrepareWorktreeReq{ProjectID: pid, WorkflowRunID: "run-1", Branch: branch, Base: tc.base})
		if e != nil {
			t.Fatalf("base %q: %+v", tc.base, e)
		}
		if m.BaseSHA != tc.want || m.HeadSHA != tc.want || m.Reattached || m.Branch != branch || m.ProjectID != pid {
			t.Errorf("base %q: %+v, want sha %s", tc.base, m, tc.want)
		}
		if got := gitIn(t, m.Path, "rev-parse", "HEAD"); got != tc.want {
			t.Errorf("base %q: worktree at %s, want %s", tc.base, got, tc.want)
		}
		if got := gitIn(t, m.Path, "symbolic-ref", "--short", "HEAD"); got != branch {
			t.Errorf("worktree on %s, want %s", got, branch)
		}
		if !worktree.IsManaged(repo, m.Path) {
			t.Errorf("%s is not under .worktrees", m.Path)
		}
		if m.Merged == nil || len(m.Merged) != 0 {
			t.Errorf("merged = %#v, want an empty list", m.Merged)
		}
	}
}

func TestPrepareWorktree_RefusesOptionLikeBase(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := pluginTestControl(t, d, "hivegui/test")
	pid, repo, _, _ := wfProject(t, d)
	marker := filepath.Join(t.TempDir(), "marker")
	for _, base := range []string{"--output=" + marker, "-h", "--upload-pack=touch " + marker, "", "no-such-ref"} {
		_, e := prepare(t, c, wire.PrepareWorktreeReq{ProjectID: pid, WorkflowRunID: "run-1", Branch: "hive/wf/run-1/a", Base: base})
		if e == nil || e.Code != wire.ErrCodeInvalidBase {
			t.Errorf("base %q: error = %+v, want invalid_base", base, e)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("an option-like base reached git as an option")
	}
	if out := gitIn(t, repo, "branch", "--list", "hive/*"); out != "" {
		t.Errorf("a refused prepare left branches behind: %s", out)
	}
}

func TestPrepareWorktree_BranchOutsideRunPrefixRefused(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := pluginTestControl(t, d, "hivegui/test")
	pid, repo, _, _ := wfProject(t, d)
	for _, tc := range []struct{ run, branch, code string }{
		{"run-1", "main", wire.ErrCodeInvalidWorkflowBranch},
		{"run-1", "hive/wf/run-2/a", wire.ErrCodeInvalidWorkflowBranch},
		{"run-1", "hive/wf/run-1/", wire.ErrCodeInvalidWorkflowBranch},
		{"run-1", "-b", wire.ErrCodeInvalidWorkflowBranch},
		{"run-1", "hive/wf/run-1/a b", wire.ErrCodeInvalidWorkflowBranch},
		{"run-1", "hive/wf/run-1/../x", wire.ErrCodeInvalidWorkflowBranch},
		{"Run/1", "hive/wf/Run/1/a", wire.ErrCodeInvalidWorkflowRunID},
		{"", "hive/wf//a", wire.ErrCodeInvalidWorkflowRunID},
	} {
		_, e := prepare(t, c, wire.PrepareWorktreeReq{ProjectID: pid, WorkflowRunID: tc.run, Branch: tc.branch, Base: "main"})
		if e == nil || e.Code != tc.code {
			t.Errorf("run %q branch %q: error = %+v, want %s", tc.run, tc.branch, e, tc.code)
		}
	}
	// A branch that already exists outside the namespace is never touched.
	gitIn(t, repo, "branch", "feature")
	if _, e := prepare(t, c, wire.PrepareWorktreeReq{ProjectID: pid, WorkflowRunID: "run-1", Branch: "feature", Base: "main"}); e == nil {
		t.Error("an existing non-run branch was checked out")
	}
	if _, err := os.Stat(filepath.Join(repo, ".worktrees")); err == nil {
		t.Error("refusals created .worktrees")
	}
	// Unknown project.
	_, e := prepare(t, c, wire.PrepareWorktreeReq{ProjectID: "nope", WorkflowRunID: "run-1", Branch: "hive/wf/run-1/a", Base: "main"})
	if e == nil || e.Code != "no_such_project" {
		t.Errorf("unknown project: %+v", e)
	}
}

func TestPrepareWorktree_PathCollisionResolved(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := pluginTestControl(t, d, "hivegui/test")
	pid, repo, _, _ := wfProject(t, d)
	// Two different branches that sanitize to the same directory name.
	if err := os.MkdirAll(filepath.Join(repo, ".worktrees", "hive-wf-run-1-a-b"), 0o755); err != nil {
		t.Fatal(err)
	}
	m, e := prepare(t, c, wire.PrepareWorktreeReq{ProjectID: pid, WorkflowRunID: "run-1", Branch: "hive/wf/run-1/a/b", Base: "main"})
	if e != nil {
		t.Fatal(e)
	}
	if m.Branch != "hive/wf/run-1/a/b" {
		t.Errorf("branch = %s: collisions suffix the path, never the branch", m.Branch)
	}
	if base := filepath.Base(m.Path); base != "hive-wf-run-1-a-b-2" {
		t.Errorf("path = %s, want the -2 suffix", m.Path)
	}
	m2, e := prepare(t, c, wire.PrepareWorktreeReq{ProjectID: pid, WorkflowRunID: "run-1", Branch: "hive/wf/run-1/a-b", Base: "main"})
	if e != nil {
		t.Fatal(e)
	}
	if base := filepath.Base(m2.Path); base != "hive-wf-run-1-a-b-3" {
		t.Errorf("second collision path = %s, want the -3 suffix", m2.Path)
	}
}

func TestPrepareWorktree_ReattachesExistingRunBranch(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := pluginTestControl(t, d, "hivegui/test")
	pid, _, _, second := wfProject(t, d)
	req := wire.PrepareWorktreeReq{ProjectID: pid, WorkflowRunID: "run-1", Branch: "hive/wf/run-1/a", Base: "main"}
	m1, e := prepare(t, c, req)
	if e != nil {
		t.Fatal(e)
	}
	req.Base = "does-not-matter"
	m2, e := prepare(t, c, req)
	if e != nil {
		t.Fatalf("re-prepare: %+v", e)
	}
	if !m2.Reattached || m2.BaseSHA != "" || m2.Path != m1.Path || m2.HeadSHA != second {
		t.Errorf("m2 = %+v, want the same worktree, reattached, no base_sha", m2)
	}
}

func TestPrepareWorktree_AfterReclaimSucceeds(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := pluginTestControl(t, d, "hivegui/test")
	pid, repo, _, _ := wfProject(t, d)
	req := wire.PrepareWorktreeReq{ProjectID: pid, WorkflowRunID: "run-1", Branch: "hive/wf/run-1/a", Base: "main"}
	m1, e := prepare(t, c, req)
	if e != nil {
		t.Fatal(e)
	}
	// The boot reclaim removes a pristine, sessionless worktree.
	d.Registry().ReclaimOrphanWorktrees(context.Background(),
		[]registry.OrphanWorktreeCandidate{{Root: repo, Path: m1.Path, ProjectID: pid}})
	if _, err := os.Stat(m1.Path); err == nil {
		t.Fatal("the reclaim kept the pristine worktree; this test needs it gone")
	}
	m2, e := prepare(t, c, req)
	if e != nil {
		t.Fatalf("prepare after reclaim: %+v", e)
	}
	if !m2.Reattached || m2.Branch != m1.Branch {
		t.Errorf("m2 = %+v, want the same branch re-attached", m2)
	}
	if got := gitIn(t, m2.Path, "symbolic-ref", "--short", "HEAD"); got != m1.Branch {
		t.Errorf("new worktree on %s, want %s", got, m1.Branch)
	}
}

// A merge that conflicts is a typed reply naming the branch and files,
// not an ERROR, and keeps the worktree.
func TestPrepareWorktree_MergeConflictTyped(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := pluginTestControl(t, d, "hivegui/test")
	pid, repo, _, _ := wfProject(t, d)

	node := func(name, file, body string) string {
		m, e := prepare(t, c, wire.PrepareWorktreeReq{ProjectID: pid, WorkflowRunID: "run-1", Branch: "hive/wf/run-1/" + name, Base: "main"})
		if e != nil {
			t.Fatal(e)
		}
		if err := os.WriteFile(filepath.Join(m.Path, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		send(t, c, wire.FrameCommitWorktree, wire.CommitWorktreeReq{ReqID: 9, ProjectID: pid, WorkflowRunID: "run-1", Branch: m.Branch, Message: "work " + name})
		ack := readFrameWhere(t, c, "ACK", isFrame(wire.FrameAck)).ack(t)
		if !ack.Committed || ack.Head == "" || ack.ReqID != 9 {
			t.Fatalf("commit ack = %+v", ack)
		}
		return m.Branch
	}
	b1 := node("n1", "same.txt", "one\n")
	b2 := node("n2", "same.txt", "two\n")
	b3 := node("n3", "other.txt", "three\n")

	m, e := prepare(t, c, wire.PrepareWorktreeReq{ProjectID: pid, WorkflowRunID: "run-1", Branch: "hive/wf/run-1/merge", Base: "main", Merge: []string{b1, b2, b3}})
	if e != nil {
		t.Fatalf("a conflict is a typed reply, not an ERROR: %+v", e)
	}
	if m.Conflict == nil || m.Conflict.Branch != b2 || len(m.Conflict.Files) != 1 || m.Conflict.Files[0] != "same.txt" {
		t.Fatalf("conflict = %+v", m.Conflict)
	}
	if len(m.Merged) != 1 || m.Merged[0] != b1 {
		t.Errorf("merged = %v, want [%s]", m.Merged, b1)
	}
	if m.HeadSHA != gitIn(t, m.Path, "rev-parse", "HEAD") {
		t.Error("head_sha is not the worktree HEAD")
	}
	if st := gitIn(t, m.Path, "status", "--porcelain"); st != "" {
		t.Errorf("worktree not clean after the aborted merge:\n%s", st)
	}
	if !worktree.IsManaged(repo, m.Path) {
		t.Error("the conflicted worktree is not kept in place")
	}

	// The clean subset merges and reports no conflict.
	m2, e := prepare(t, c, wire.PrepareWorktreeReq{ProjectID: pid, WorkflowRunID: "run-1", Branch: "hive/wf/run-1/merge2", Base: "main", Merge: []string{b1, b3}})
	if e != nil || m2.Conflict != nil || len(m2.Merged) != 2 {
		t.Errorf("clean merge: %+v, %+v", m2, e)
	}
}

func TestPrepareWorktree_NoIdentity(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := pluginTestControl(t, d, "hivegui/test")
	pid, repo, _, _ := wfProject(t, d)
	for _, v := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "EMAIL"} {
		t.Setenv(v, "")
		os.Unsetenv(v)
	}
	gitIn(t, repo, "branch", "hive/wf/run-1/n1")
	gitIn(t, repo, "config", "--unset", "user.name")
	gitIn(t, repo, "config", "--unset", "user.email")
	_, e := prepare(t, c, wire.PrepareWorktreeReq{ProjectID: pid, WorkflowRunID: "run-1", Branch: "hive/wf/run-1/m", Base: "main", Merge: []string{"hive/wf/run-1/n1"}})
	if e == nil || e.Code != wire.ErrCodeGitIdentityMissing {
		t.Errorf("error = %+v, want git_identity_missing", e)
	}
}

// A session created at the prepared path adopts it, so the worktree
// counts as claimed (an unclaimed one is eligible for the boot reclaim).
func TestPrepareWorktree_SessionAdoptsPath(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := pluginTestControl(t, d, "hivegui/test")
	pid, repo, _, _ := wfProject(t, d)
	m, e := prepare(t, c, wire.PrepareWorktreeReq{ProjectID: pid, WorkflowRunID: "run-1", Branch: "hive/wf/run-1/a", Base: "main"})
	if e != nil {
		t.Fatal(e)
	}
	send(t, c, wire.FrameCreateSession, wire.CreateSpec{ProjectID: pid, WorktreePath: m.Path, Cwd: repo})
	var got wire.SessionInfo
	waitFor(t, 10*time.Second, func() bool {
		for _, s := range d.Registry().List() {
			if s.WorktreePath != "" {
				got = s
				return true
			}
		}
		return false
	})
	if worktree.ResolvePath(got.WorktreePath) != worktree.ResolvePath(m.Path) {
		t.Errorf("session worktree = %q, want %q", got.WorktreePath, m.Path)
	}
	if got.WorktreeBranch != m.Branch {
		t.Errorf("session branch = %q, want %q", got.WorktreeBranch, m.Branch)
	}
}

func TestCommitWorktree_Frame(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := pluginTestControl(t, d, "hivegui/test")
	pid, repo, _, _ := wfProject(t, d)
	m, e := prepare(t, c, wire.PrepareWorktreeReq{ProjectID: pid, WorkflowRunID: "run-1", Branch: "hive/wf/run-1/a", Base: "main"})
	if e != nil {
		t.Fatal(e)
	}
	commit := func(run, branch, msg string) (wire.AckMsg, *wire.Error) {
		send(t, c, wire.FrameCommitWorktree, wire.CommitWorktreeReq{ReqID: 12, ProjectID: pid, WorkflowRunID: run, Branch: branch, Message: msg})
		f := readFrameWhere(t, c, "ACK or ERROR", func(f frame) bool { return f.ft == wire.FrameAck || f.ft == wire.FrameError })
		if f.ft == wire.FrameError {
			er := f.err(t)
			return wire.AckMsg{}, &er
		}
		return f.ack(t), nil
	}

	// Clean tree: an idempotent no-op.
	if ack, er := commit("run-1", m.Branch, "m"); er != nil || ack.Committed || ack.Head != m.HeadSHA {
		t.Errorf("clean: %+v, %+v", ack, er)
	}
	if err := os.WriteFile(filepath.Join(m.Path, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ack, er := commit("run-1", m.Branch, "add f")
	if er != nil || !ack.Committed || ack.Head == m.HeadSHA || ack.Head != gitIn(t, m.Path, "rev-parse", "HEAD") {
		t.Errorf("dirty: %+v, %+v", ack, er)
	}
	if got := gitIn(t, repo, "log", "-1", "--format=%an|%s", m.Branch); got != "Test User|add f" {
		t.Errorf("commit = %q", got)
	}
	// Refusals: outside the run prefix, another run's branch, no worktree.
	for _, tc := range []struct{ run, branch, code string }{
		{"run-1", "main", wire.ErrCodeInvalidWorkflowBranch},
		{"run-2", m.Branch, wire.ErrCodeInvalidWorkflowBranch},
		{"run-1", "hive/wf/run-1/none", wire.ErrCodeWorktreeNotFound},
		{"Bad Run", m.Branch, wire.ErrCodeInvalidWorkflowRunID},
	} {
		if _, er := commit(tc.run, tc.branch, "m"); er == nil || er.Code != tc.code || er.ReqID != 12 {
			t.Errorf("%s/%s: error = %+v, want %s", tc.run, tc.branch, er, tc.code)
		}
	}
	// A hook in the repo never runs.
	marker := filepath.Join(t.TempDir(), "hook-ran")
	hook := "#!/bin/sh\ntouch '" + marker + "'\nexit 1\n"
	for _, h := range []string{"pre-commit", "commit-msg"} {
		if err := os.WriteFile(filepath.Join(repo, ".git", "hooks", h), []byte(hook), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(m.Path, "g.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ack, er := commit("run-1", m.Branch, "with hooks present"); er != nil || !ack.Committed {
		t.Errorf("hooks present: %+v, %+v", ack, er)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a repo hook ran during COMMIT_WORKTREE")
	}
}
