// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package daemon

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// B3-R7 (spec 529, B-25): ARCHIVE_SESSION and REMOVE_WORKTREE ack their
// req_id, and a plugin may remove a kept hive/wf/<run>/ branch's worktree
// with unpushed commits.

func isAckOrError(f frame) bool { return f.ft == wire.FrameAck || f.ft == wire.FrameError }

func r7Worktree(t *testing.T, repo, branch string, commit bool) string {
	t.Helper()
	wt := filepath.Join(repo, ".worktrees", strings.ReplaceAll(branch, "/", "-"))
	for _, args := range [][]string{{"worktree", "add", "-q", "-b", branch, wt}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if commit {
		if err := os.WriteFile(filepath.Join(wt, "a.txt"), []byte("work"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"add", "a.txt"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "a"}} {
			if out, err := exec.Command("git", append([]string{"-C", wt}, args...)...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}
	return wt
}

func r7Archivable(t *testing.T, d *Daemon) string {
	t.Helper()
	e, err := d.reg.Create(context.Background(), wire.CreateSpec{ProjectID: projectIDOf(t, d), Shell: "/bin/bash"})
	if err != nil {
		t.Fatal(err)
	}
	return e.ID
}

func TestArchiveSession_AcksReqID(t *testing.T) {
	d, _ := startDaemonInRepo(t)
	id := r7Archivable(t, d)
	c := controlConn(t, d)

	send(t, c, wire.FrameArchiveSession, wire.ArchiveSessionReq{SessionID: id, ReqID: 61})
	// The ACK follows the ARCHIVED refresh.
	readFrameWhere(t, c, "ARCHIVED", isFrame(wire.FrameArchived))
	a := readFrameWhere(t, c, "ACK", isAckOrError).ack(t)
	if a.ReqID != 61 || a.SessionID != id {
		t.Errorf("ack = %+v, want req_id 61 session %s", a, id)
	}
	if d.reg.Get(id) != nil {
		t.Error("session still live")
	}

	// A refusal echoes the id and sends no ACK.
	send(t, c, wire.FrameArchiveSession, wire.ArchiveSessionReq{SessionID: "nope", ReqID: 62})
	if e := readFrameWhere(t, c, "ERROR", isAckOrError); e.ft != wire.FrameError || e.err(t).ReqID != 62 {
		t.Errorf("got %v %s, want ERROR echoing 62", e.ft, e.payload)
	}
}

func TestArchiveSession_NoReqIDNoAck(t *testing.T) {
	d, _ := startDaemonInRepo(t)
	id := r7Archivable(t, d)
	c := controlConn(t, d)

	send(t, c, wire.FrameArchiveSession, wire.ArchiveSessionReq{SessionID: id})
	readFrameWhere(t, c, "ARCHIVED", isFrame(wire.FrameArchived))
	// A later, answered request: if an ACK had been queued behind the
	// ARCHIVED it would arrive before this ERROR.
	send(t, c, wire.FrameArchiveSession, wire.ArchiveSessionReq{SessionID: "nope", ReqID: 63})
	if f := readFrameWhere(t, c, "ACK or ERROR", isAckOrError); f.ft != wire.FrameError {
		t.Errorf("got an unrequested %v: %s", f.ft, f.payload)
	}
}

func TestRemoveWorktree_AcksReqID(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	wt := r7Worktree(t, repo, "plain", false)
	c := controlConn(t, d)

	send(t, c, wire.FrameRemoveWorktree, wire.RemoveWorktreeReq{ProjectID: pid, Path: wt, ReqID: 71})
	// The ACK follows the WORKTREES inventory.
	readFrameWhere(t, c, "WORKTREES", isFrame(wire.FrameWorktrees))
	if a := readFrameWhere(t, c, "ACK", isAckOrError).ack(t); a.ReqID != 71 {
		t.Errorf("ack = %+v, want req_id 71", a)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Error("worktree still on disk")
	}
}

func TestRemoveWorktree_NoReqIDNoAck(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	wt := r7Worktree(t, repo, "quiet", false)
	c := controlConn(t, d)

	send(t, c, wire.FrameRemoveWorktree, wire.RemoveWorktreeReq{ProjectID: pid, Path: wt})
	readFrameWhere(t, c, "WORKTREES", isFrame(wire.FrameWorktrees))
	send(t, c, wire.FrameRemoveWorktree, wire.RemoveWorktreeReq{ProjectID: pid, Path: wt, ReqID: 72})
	if f := readFrameWhere(t, c, "ACK or ERROR", isAckOrError); f.ft != wire.FrameError {
		t.Errorf("got an unrequested %v: %s", f.ft, f.payload)
	}
}

func TestRemoveWorktree_ErrorEchoesReqID(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	wt := r7Worktree(t, repo, "dirty", false)
	if err := os.WriteFile(filepath.Join(wt, "scratch.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := controlConn(t, d)

	send(t, c, wire.FrameRemoveWorktree, wire.RemoveWorktreeReq{ProjectID: pid, Path: wt, ReqID: 73})
	f := readFrameWhere(t, c, "ERROR", isAckOrError)
	if f.ft != wire.FrameError {
		t.Fatalf("got %v, want ERROR", f.ft)
	}
	if e := f.err(t); e.Code != wire.ErrCodeWorktreeDirty || e.ReqID != 73 {
		t.Errorf("error = %+v, want worktree_dirty echoing 73", e)
	}
}

// removeFromPlugin sends REMOVE_WORKTREE for wt over a plugin socket and
// returns the first ACK or ERROR.
func removeFrom(t *testing.T, c net.Conn, req wire.RemoveWorktreeReq) frame {
	t.Helper()
	send(t, c, wire.FrameRemoveWorktree, req)
	return readFrameWhere(t, c, "ACK or ERROR", isAckOrError)
}

func TestRemoveWorktree_RunBranchUnpushedExemptForPlugin(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	wt := r7Worktree(t, repo, "hive/wf/run-1/x", true)
	c, _ := pluginConn(t, d, "wf", "wf")

	f := removeFrom(t, c, wire.RemoveWorktreeReq{ProjectID: pid, Path: wt, WorkflowRunID: "run-1", ReqID: 81})
	if f.ft != wire.FrameAck || f.ack(t).ReqID != 81 {
		t.Fatalf("got %v %s, want ACK 81", f.ft, f.payload)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Error("worktree still on disk")
	}
	if got := gitOut(t, repo, "branch", "--list", "hive/wf/run-1/x"); got == "" {
		t.Error("run branch was deleted")
	}
}

func TestRemoveWorktree_ExemptionStillRefusesDirty(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	wt := r7Worktree(t, repo, "hive/wf/run-1/x", true)
	if err := os.WriteFile(filepath.Join(wt, "scratch.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _ := pluginConn(t, d, "wf", "wf")

	f := removeFrom(t, c, wire.RemoveWorktreeReq{ProjectID: pid, Path: wt, WorkflowRunID: "run-1", ReqID: 82})
	if f.ft != wire.FrameError || f.err(t).Code != wire.ErrCodeWorktreeDirty {
		t.Fatalf("got %v %s, want worktree_dirty", f.ft, f.payload)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("dirty worktree removed: %v", err)
	}
}

func TestRemoveWorktree_ExemptionNotForOtherRunOrPrefix(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	c, _ := pluginConn(t, d, "wf", "wf")
	for i, tc := range []struct{ branch, run string }{
		{"hive/wf/run-2/x", "run-1"},
		{"feature/x", "run-1"},
		{"hive/wf/run-1/y", "../run-1"},
	} {
		wt := r7Worktree(t, repo, tc.branch, true)
		f := removeFrom(t, c, wire.RemoveWorktreeReq{ProjectID: pid, Path: wt, WorkflowRunID: tc.run, ReqID: 90 + i})
		if f.ft != wire.FrameError || f.err(t).Code != wire.ErrCodeWorktreeUnpushed {
			t.Errorf("%s/%s: got %v %s, want worktree_unpushed", tc.branch, tc.run, f.ft, f.payload)
		}
		if _, err := os.Stat(wt); err != nil {
			t.Errorf("%s/%s: worktree removed: %v", tc.branch, tc.run, err)
		}
	}
}

func TestRemoveWorktree_ExemptionNotForUserPrincipal(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	wt := r7Worktree(t, repo, "hive/wf/run-1/x", true)
	c := controlConn(t, d)

	f := removeFrom(t, c, wire.RemoveWorktreeReq{ProjectID: pid, Path: wt, WorkflowRunID: "run-1", ReqID: 83})
	if f.ft != wire.FrameError || f.err(t).Code != wire.ErrCodeWorktreeUnpushed {
		t.Fatalf("got %v %s, want worktree_unpushed", f.ft, f.payload)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree removed for a user caller: %v", err)
	}
}

func TestRemoveWorktree_ExemptionNotWithDeleteBranch(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	wt := r7Worktree(t, repo, "hive/wf/run-1/x", true)
	c, _ := pluginConn(t, d, "wf", "wf")

	f := removeFrom(t, c, wire.RemoveWorktreeReq{ProjectID: pid, Path: wt, WorkflowRunID: "run-1", DeleteBranch: true, ReqID: 84})
	if f.ft != wire.FrameError || f.err(t).Code != wire.ErrCodeWorktreeUnpushed {
		t.Fatalf("got %v %s, want worktree_unpushed", f.ft, f.payload)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree removed: %v", err)
	}
}

func TestRemoveWorktree_ArchivedHolderStillInUse(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	e, err := d.reg.Create(context.Background(), wire.CreateSpec{ProjectID: pid, Shell: "/bin/bash", UseWorktree: true})
	if err != nil || e.WorktreePath == "" {
		t.Skipf("worktree session unavailable: %v", err)
	}
	if err := d.reg.Archive(e.ID); err != nil {
		t.Fatal(err)
	}
	_ = repo
	c, _ := pluginConn(t, d, "wf", "wf")

	f := removeFrom(t, c, wire.RemoveWorktreeReq{ProjectID: pid, Path: e.WorktreePath, WorkflowRunID: "run-1", ReqID: 85})
	if f.ft != wire.FrameError || f.err(t).Code != wire.ErrCodeWorktreeInUse {
		t.Fatalf("got %v %s, want worktree_in_use", f.ft, f.payload)
	}
	if _, err := os.Stat(e.WorktreePath); err != nil {
		t.Errorf("worktree removed from under an archived session: %v", err)
	}
}
