package registry

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// initGitRepo creates a temp git repo with one initial commit. Used
// by the worktree integration tests below.
func initGitRepo(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("worktree tests require POSIX shell")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// freshRegistryWithProject opens a registry, creates a project rooted
// at a fresh git repo, and returns both. Cleanup runs via t.Cleanup.
func freshRegistryWithProject(t *testing.T) (*Registry, *Project) {
	t.Helper()
	r := freshRegistry(t)
	repo := initGitRepo(t)
	p, err := r.CreateProject(wire.CreateProjectReq{Name: "git", Cwd: repo})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return r, p
}

func TestCreate_WorktreeNonGitCwd(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	p, _ := r.CreateProject(wire.CreateProjectReq{Name: "plain", Cwd: t.TempDir()})

	e, err := r.Create(wire.CreateSpec{
		ProjectID:   p.ID,
		Shell:       "/bin/bash",
		UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer r.Kill(e.ID, true)

	if e.WorktreePath != "" {
		t.Errorf("non-git project should not get a worktree, got %q", e.WorktreePath)
	}
}

func TestCreate_WorktreeHappyPath(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	e, err := r.Create(wire.CreateSpec{
		ProjectID:   p.ID,
		Shell:       "/bin/bash",
		Agent:       "claude",
		UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer r.Kill(e.ID, true)

	if e.WorktreePath == "" {
		t.Fatalf("expected WorktreePath to be set; got empty")
	}
	// Session name should be derived from the worktree branch so the
	// user can find the worktree dir from the session label, with the
	// agent appended and any "/" in the branch folded to "-".
	if !strings.Contains(e.Name, e.WorktreeBranch) && !strings.Contains(e.Name, strings.ReplaceAll(e.WorktreeBranch, "/", "-")) {
		t.Errorf("session name %q should contain worktree branch %q", e.Name, e.WorktreeBranch)
	}
	if !strings.HasSuffix(e.Name, " claude") {
		t.Errorf("session name %q should end with agent suffix \" claude\"", e.Name)
	}
	if strings.Contains(e.Name, "/") {
		t.Errorf("session name %q must not contain slashes (path-unsafe)", e.Name)
	}
	// macOS resolves /var → /private/var; compare canonicalized paths.
	wtReal, _ := filepath.EvalSymlinks(e.WorktreePath)
	cwdReal, _ := filepath.EvalSymlinks(p.Cwd)
	if !strings.HasPrefix(wtReal, cwdReal) {
		t.Errorf("WorktreePath %q not under project cwd %q", wtReal, cwdReal)
	}
	if _, err := os.Stat(e.WorktreePath); err != nil {
		t.Errorf("worktree dir doesn't exist: %v", err)
	}

	// `git worktree list` should mention our new dir.
	out, err := exec.Command("git", "-C", p.Cwd, "worktree", "list").Output()
	if err != nil {
		t.Fatalf("git worktree list: %v", err)
	}
	if !strings.Contains(string(out), e.WorktreePath) {
		t.Errorf("git worktree list missing %q:\n%s", e.WorktreePath, out)
	}
}

func TestKill_WorktreeRemoved(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	e, err := r.Create(wire.CreateSpec{
		ProjectID:   p.ID,
		Shell:       "/bin/bash",
		UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	wtPath := e.WorktreePath
	if wtPath == "" {
		t.Fatalf("worktree not created")
	}
	// Give the spawned shell a moment so Close has something live.
	time.Sleep(80 * time.Millisecond)

	if err := r.Kill(e.ID, false); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, err := os.Stat(wtPath); err == nil {
		t.Errorf("worktree dir still exists after Kill")
	}
	out, _ := exec.Command("git", "-C", p.Cwd, "worktree", "list").Output()
	if strings.Contains(string(out), wtPath) {
		t.Errorf("git worktree list still references %q", wtPath)
	}
}

func TestKill_DirtyWorktree_NoForce_ErrsAndPreserves(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e, err := r.Create(wire.CreateSpec{
		ProjectID:   p.ID,
		Shell:       "/bin/bash",
		UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer r.Kill(e.ID, true)
	time.Sleep(80 * time.Millisecond)

	// Make the worktree dirty.
	if err := os.WriteFile(filepath.Join(e.WorktreePath, "scratch.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("dirty file: %v", err)
	}

	err = r.Kill(e.ID, false)
	if err != ErrWorktreeDirty {
		t.Fatalf("expected ErrWorktreeDirty, got %v", err)
	}
	// State must be preserved — the entry is still alive and the
	// worktree dir is still present.
	if r.Get(e.ID) == nil {
		t.Errorf("entry vanished after dirty Kill (state should be preserved)")
	}
	if _, err := os.Stat(e.WorktreePath); err != nil {
		t.Errorf("worktree dir was removed despite dirty Kill returning early")
	}
}

func TestKill_DirtyWorktree_ForceRemoves(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e, _ := r.Create(wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	time.Sleep(80 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(e.WorktreePath, "scratch.txt"), []byte("x"), 0o644)

	if err := r.Kill(e.ID, true); err != nil {
		t.Fatalf("force Kill: %v", err)
	}
	if _, err := os.Stat(e.WorktreePath); err == nil {
		t.Errorf("worktree dir still exists after force Kill")
	}
}

// TestCreate_ExplicitCwdInsideWorktree_NoNestedWorktree pins the
// invariants the GUI's ⌘P / ⇧⌘P shortcut relies on: when the caller
// passes Cwd pointing inside an existing worktree with UseWorktree=false,
// the daemon must (a) NOT stack a nested .worktrees/* on top, and
// (b) ADOPT the existing worktree's path+branch onto the new entry so
// the sidebar shows the badge and Kill keeps the worktree alive until
// the last session in it goes away.
func TestCreate_ExplicitCwdInsideWorktree_NoNestedWorktree(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	// Source session: spawns a worktree under p.Cwd/.worktrees/<branch>.
	src, err := r.Create(wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", Agent: "claude", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create source: %v", err)
	}
	defer r.Kill(src.ID, true)
	wtPath := src.WorktreePath
	if wtPath == "" {
		t.Fatalf("expected source to have a worktree")
	}
	time.Sleep(80 * time.Millisecond)

	// Duplicate: explicit cwd inside the existing worktree, UseWorktree
	// off. This is exactly the wire payload the GUI sends on ⌘P.
	dup, err := r.Create(wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", Agent: "claude",
		Cwd: wtPath, UseWorktree: false,
	})
	if err != nil {
		t.Fatalf("Create dup: %v", err)
	}
	defer r.Kill(dup.ID, true)

	if dup.WorktreePath != wtPath {
		t.Errorf("duplicate should adopt source worktree path %q; got %q", wtPath, dup.WorktreePath)
	}
	if dup.WorktreeBranch != src.WorktreeBranch {
		t.Errorf("duplicate should adopt source worktree branch %q; got %q", src.WorktreeBranch, dup.WorktreeBranch)
	}
	// No nested .worktrees/ under the source worktree dir.
	if _, err := os.Stat(filepath.Join(wtPath, ".worktrees")); err == nil {
		t.Errorf("nested .worktrees/ created under source worktree at %q", wtPath)
	}
	// `git worktree list` should still show exactly two entries (main
	// + the source's worktree). A nested worktree would show three.
	out, err := exec.Command("git", "-C", p.Cwd, "worktree", "list").Output()
	if err != nil {
		t.Fatalf("git worktree list: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 worktree entries (main + source), got %d:\n%s",
			len(lines), out)
	}
}

// TestKill_SharedWorktree_KeepsWorktreeUntilLast pins the
// last-session-wins cleanup rule: when several sessions live in the
// same worktree (because they were duplicated from one another),
// killing all but the last must NOT remove the directory. Only the
// final Kill performs `git worktree remove`.
func TestKill_SharedWorktree_KeepsWorktreeUntilLast(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	src, err := r.Create(wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create source: %v", err)
	}
	wtPath := src.WorktreePath
	if wtPath == "" {
		t.Fatalf("expected source to have a worktree")
	}
	time.Sleep(80 * time.Millisecond)

	dup, err := r.Create(wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash",
		Cwd: wtPath, UseWorktree: false,
	})
	if err != nil {
		t.Fatalf("Create dup: %v", err)
	}
	time.Sleep(80 * time.Millisecond)

	// Kill the source (sibling still alive in the worktree). The
	// worktree dir must survive — and the dirty check must be
	// skipped since dirtiness is irrelevant when others remain.
	if err := r.Kill(src.ID, false); err != nil {
		t.Fatalf("Kill src: %v", err)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree %q removed while sibling still uses it: %v", wtPath, err)
	}

	// Killing the last sibling cleans up.
	if err := r.Kill(dup.ID, true); err != nil {
		t.Fatalf("Kill dup: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("worktree %q should be cleaned up after last session; stat err=%v", wtPath, err)
	}
}

func TestRevive_StaleWorktreePath_SelfHeals(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	// Create a worktree session, drop the live PTY without going
	// through Kill (which would also delete the worktree), and then
	// nuke the worktree dir to simulate the user wiping it.
	e, _ := r.Create(wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	time.Sleep(80 * time.Millisecond)
	wtPath := e.WorktreePath
	if e.sess != nil {
		_ = e.sess.Close()
		// Pretend the daemon restarted: detach the live session.
		r.mu.Lock()
		e.sess = nil
		r.mu.Unlock()
	}
	_ = os.RemoveAll(wtPath)

	// Revive should self-heal: clear WorktreePath/Branch on the
	// entry and start the session at the project cwd.
	if err := r.Revive(e.ID, session.Options{Shell: "/bin/bash", Cwd: p.Cwd}); err != nil {
		t.Fatalf("Revive: %v", err)
	}
	got := r.Get(e.ID)
	if got == nil {
		t.Fatalf("entry vanished after Revive")
	}
	if got.WorktreePath != "" {
		t.Errorf("expected WorktreePath cleared after self-heal, got %q", got.WorktreePath)
	}
	if got.WorktreeBranch != "" {
		t.Errorf("expected WorktreeBranch cleared after self-heal, got %q", got.WorktreeBranch)
	}
	// Cleanup the new live session.
	_ = r.Kill(e.ID, true)
}
