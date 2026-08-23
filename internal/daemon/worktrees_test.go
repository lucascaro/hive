package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// startDaemonInRepo brings up a daemon whose bootstrap project cwd is
// a fresh git repo, so worktree operations have something to act on.
// Returns the daemon and the repo path.
func startDaemonInRepo(t *testing.T) (*Daemon, string) {
	t.Helper()
	skipOnWindows(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tmp := shortTempDir(t)
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-q", "-m", "init"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	d, err := New(Config{
		SocketPath: filepath.Join(tmp, "s"),
		StateDir:   filepath.Join(tmp, "state"),
		BootstrapSession: session.Options{
			Shell: "/bin/bash", Cols: 80, Rows: 24, Cwd: repo,
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = d.Run(ctx) }()
	t.Cleanup(func() { cancel(); _ = d.Close() })
	return d, repo
}

// readWorktrees reads control frames until a WORKTREES frame arrives,
// returning it. Fails the test on timeout, reporting any ERROR seen —
// a silent timeout would hide the actual refusal code.
func readWorktrees(t *testing.T, conn interface {
	SetReadDeadline(time.Time) error
	Read([]byte) (int, error)
}) wire.WorktreesResp {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr wire.Error
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			continue
		}
		switch ft {
		case wire.FrameWorktrees:
			var resp wire.WorktreesResp
			if err := jsonUnmarshal(payload, &resp); err != nil {
				t.Fatalf("decode WORKTREES: %v", err)
			}
			return resp
		case wire.FrameError:
			_ = jsonUnmarshal(payload, &lastErr)
		}
	}
	t.Fatalf("timed out waiting for WORKTREES (last error: %+v)", lastErr)
	return wire.WorktreesResp{}
}

// readErrorCode reads until an ERROR frame arrives and returns its code.
func readErrorCode(t *testing.T, conn interface {
	SetReadDeadline(time.Time) error
	Read([]byte) (int, error)
}) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			continue
		}
		if ft == wire.FrameError {
			var e wire.Error
			_ = jsonUnmarshal(payload, &e)
			return e.Code
		}
		if ft == wire.FrameWorktrees {
			t.Fatal("got WORKTREES where an ERROR refusal was expected")
		}
	}
	t.Fatal("timed out waiting for ERROR")
	return ""
}

// projectIDOf returns the daemon's first project id via the registry,
// which is simpler and less racy than draining the snapshot frames.
func projectIDOf(t *testing.T, d *Daemon) string {
	t.Helper()
	projects := d.reg.ListProjects()
	if len(projects) == 0 {
		t.Fatal("daemon has no projects")
	}
	return projects[0].ID
}

func TestControl_ListWorktreesReturnsInventory(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	// A detached worktree plus an orphaned branch: both halves of the
	// browser's model.
	wt := filepath.Join(repo, ".worktrees", "detached-work")
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", "detached-work", wt).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "branch", "stranded").CombinedOutput(); err != nil {
		t.Fatalf("branch: %v\n%s", err, out)
	}

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	if err := wire.WriteJSON(conn, wire.FrameListWorktrees, wire.ListWorktreesReq{ProjectID: pid}); err != nil {
		t.Fatalf("write LIST_WORKTREES: %v", err)
	}

	resp := readWorktrees(t, conn)
	if resp.ProjectID != pid {
		t.Errorf("ProjectID = %q, want %q", resp.ProjectID, pid)
	}
	if resp.RepoRoot == "" {
		t.Error("RepoRoot empty")
	}
	var sawDetached, sawMain bool
	for _, w := range resp.Worktrees {
		if w.Branch == "detached-work" {
			sawDetached = true
		}
		if w.IsMain {
			sawMain = true
		}
	}
	if !sawDetached {
		t.Errorf("detached-work missing from inventory: %+v", resp.Worktrees)
	}
	if !sawMain {
		t.Errorf("main checkout missing from inventory: %+v", resp.Worktrees)
	}
	var sawOrphan bool
	for _, b := range resp.OrphanBranches {
		if b.Name == "stranded" {
			sawOrphan = true
		}
	}
	if !sawOrphan {
		t.Errorf("stranded missing from orphan branches: %+v", resp.OrphanBranches)
	}
}

func TestControl_RemoveWorktreeDirtyReturnsErrorCode(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	wt := filepath.Join(repo, ".worktrees", "dirty")
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", "dirty", wt).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(wt, "scratch.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	if err := wire.WriteJSON(conn, wire.FrameRemoveWorktree, wire.RemoveWorktreeReq{
		ProjectID: pid, Path: wt,
	}); err != nil {
		t.Fatalf("write REMOVE_WORKTREE: %v", err)
	}

	if code := readErrorCode(t, conn); code != wire.ErrCodeWorktreeDirty {
		t.Errorf("error code = %q, want %q", code, wire.ErrCodeWorktreeDirty)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree removed despite the refusal: %v", err)
	}
}

func TestControl_RemoveWorktreeRepliesWithFreshInventory(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	wt := filepath.Join(repo, ".worktrees", "disposable")
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", "disposable", wt).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	if err := wire.WriteJSON(conn, wire.FrameRemoveWorktree, wire.RemoveWorktreeReq{
		ProjectID: pid, Path: wt,
	}); err != nil {
		t.Fatalf("write REMOVE_WORKTREE: %v", err)
	}

	// The reply is the post-mutation inventory — the client never has
	// to ask again, and never renders the removed row.
	resp := readWorktrees(t, conn)
	for _, w := range resp.Worktrees {
		if w.Branch == "disposable" {
			t.Errorf("removed worktree still present in the reply inventory: %+v", w)
		}
	}
	if _, err := os.Stat(wt); err == nil {
		t.Errorf("worktree dir %s still on disk", wt)
	}
}

func TestControl_CreateWorktreeMaterializesBranch(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	if out, err := exec.Command("git", "-C", repo, "branch", "revive-me").CombinedOutput(); err != nil {
		t.Fatalf("branch: %v\n%s", err, out)
	}

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	if err := wire.WriteJSON(conn, wire.FrameCreateWorktree, wire.CreateWorktreeReq{
		ProjectID: pid, Branch: "revive-me",
	}); err != nil {
		t.Fatalf("write CREATE_WORKTREE: %v", err)
	}

	resp := readWorktrees(t, conn)
	var found bool
	for _, w := range resp.Worktrees {
		if w.Branch == "revive-me" {
			found = true
		}
	}
	if !found {
		t.Errorf("revive-me missing from the reply inventory: %+v", resp.Worktrees)
	}
	for _, b := range resp.OrphanBranches {
		if b.Name == "revive-me" {
			t.Errorf("revive-me still listed as an orphan branch")
		}
	}
}

func TestControl_RenameWorktreeMovesBranchAndDir(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	wt := filepath.Join(repo, ".worktrees", "old-name")
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", "old-name", wt).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	if err := wire.WriteJSON(conn, wire.FrameRenameWorktree, wire.RenameWorktreeReq{
		ProjectID: pid, Path: wt, NewBranch: "new-name",
	}); err != nil {
		t.Fatalf("write RENAME_WORKTREE: %v", err)
	}

	resp := readWorktrees(t, conn)
	var found bool
	for _, w := range resp.Worktrees {
		if w.Branch == "new-name" {
			found = true
		}
		if w.Branch == "old-name" {
			t.Errorf("old branch name still in the inventory")
		}
	}
	if !found {
		t.Errorf("new-name missing from the reply inventory: %+v", resp.Worktrees)
	}
	if _, err := os.Stat(filepath.Join(repo, ".worktrees", "new-name")); err != nil {
		t.Errorf("renamed directory missing: %v", err)
	}
}

// A path outside the project's .worktrees dir must be refused at the
// daemon boundary too, not just inside the registry.
func TestControl_RemoveWorktreeRejectsForeignPath(t *testing.T) {
	d, _ := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	victim := t.TempDir()
	if err := os.WriteFile(filepath.Join(victim, "precious.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	if err := wire.WriteJSON(conn, wire.FrameRemoveWorktree, wire.RemoveWorktreeReq{
		ProjectID: pid, Path: victim, Force: true, DeleteBranch: true,
	}); err != nil {
		t.Fatalf("write REMOVE_WORKTREE: %v", err)
	}

	if code := readErrorCode(t, conn); code != "remove_worktree_failed" {
		t.Errorf("error code = %q, want remove_worktree_failed", code)
	}
	if _, err := os.Stat(filepath.Join(victim, "precious.txt")); err != nil {
		t.Fatalf("a directory outside the project was destroyed: %v", err)
	}
}

func TestControl_DeleteBranchRemovesAnOrphan(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	if out, err := exec.Command("git", "-C", repo, "branch", "tidy-me").CombinedOutput(); err != nil {
		t.Fatalf("branch: %v\n%s", err, out)
	}

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	if err := wire.WriteJSON(conn, wire.FrameDeleteBranch, wire.DeleteBranchReq{
		ProjectID: pid, Branch: "tidy-me",
	}); err != nil {
		t.Fatalf("write DELETE_BRANCH: %v", err)
	}

	resp := readWorktrees(t, conn)
	for _, b := range resp.OrphanBranches {
		if b.Name == "tidy-me" {
			t.Errorf("deleted branch still listed as an orphan")
		}
	}
}

func TestControl_DeleteBranchUnmergedReturnsErrorCode(t *testing.T) {
	d, repo := startDaemonInRepo(t)
	pid := projectIDOf(t, d)
	for _, args := range [][]string{
		{"checkout", "-q", "-b", "has-work"},
		{"commit", "--allow-empty", "-q", "-m", "work", "-c", "user.email=t@t", "-c", "user.name=t"},
	} {
		// -c has to precede the subcommand; build it explicitly.
		full := append([]string{"-C", repo}, args...)
		if args[0] == "commit" {
			full = []string{"-C", repo, "-c", "user.email=t@t", "-c", "user.name=t",
				"commit", "--allow-empty", "-q", "-m", "work"}
		}
		if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", full, err, out)
		}
	}
	if out, err := exec.Command("git", "-C", repo, "checkout", "-q", "main").CombinedOutput(); err != nil {
		t.Fatalf("checkout main: %v\n%s", err, out)
	}

	conn := dial(t, d)
	defer conn.Close()
	_ = handshake(t, conn, wire.Hello{Mode: wire.ModeControl})
	if err := wire.WriteJSON(conn, wire.FrameDeleteBranch, wire.DeleteBranchReq{
		ProjectID: pid, Branch: "has-work",
	}); err != nil {
		t.Fatalf("write DELETE_BRANCH: %v", err)
	}

	if code := readErrorCode(t, conn); code != wire.ErrCodeBranchUnmerged {
		t.Errorf("error code = %q, want %q", code, wire.ErrCodeBranchUnmerged)
	}
	out, _ := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/has-work").Output()
	if len(out) == 0 {
		t.Error("branch was deleted despite the refusal")
	}
}
