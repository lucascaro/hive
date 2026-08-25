package registry

import (
	"context"
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

// runGit runs a git command in dir, failing the test on a non-zero
// exit. The registry-level worktree tests drive real git the same way
// internal/worktree's do — no mocking, since the whole point is that
// the git plumbing behaves.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// gitOutput returns the trimmed stdout of a git command, or "" on
// failure. Used for assertions that must not abort the test.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
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

	e, err := r.Create(context.Background(), wire.CreateSpec{
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

	e, err := r.Create(context.Background(), wire.CreateSpec{
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

	e, err := r.Create(context.Background(), wire.CreateSpec{
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
	e, err := r.Create(context.Background(), wire.CreateSpec{
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

// force lets the SESSION close; it does not destroy the worktree. The
// uncommitted file is the user's work — closing a tab must never be
// what deletes it. Removal is an explicit act in the worktree browser
// (RemoveWorktree), which has its own confirm.
func TestKill_DirtyWorktree_ForceKeepsWorktree(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e, _ := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	time.Sleep(80 * time.Millisecond)
	wtPath := e.WorktreePath
	_ = os.WriteFile(filepath.Join(wtPath, "scratch.txt"), []byte("x"), 0o644)

	if err := r.Kill(e.ID, true); err != nil {
		t.Fatalf("force Kill: %v", err)
	}
	if r.Get(e.ID) != nil {
		t.Errorf("entry survived force Kill; the session should be gone")
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree dir was removed by force Kill: %v — uncommitted work must survive", err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "scratch.txt")); err != nil {
		t.Errorf("the uncommitted file was destroyed: %v", err)
	}
}

// The counterpart: a worktree with nothing in it is still pruned on
// close, so throwaway sessions don't litter .worktrees/.
func TestKill_PristineWorktreeIsPruned(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e, _ := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	time.Sleep(80 * time.Millisecond)
	wtPath := e.WorktreePath
	if wtPath == "" {
		t.Fatal("no worktree created")
	}

	if err := r.Kill(e.ID, false); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, err := os.Stat(wtPath); err == nil {
		t.Errorf("pristine worktree %s survived Kill; it should have been pruned", wtPath)
	}
}

// Committed-but-unpushed work is the case a dirty check alone misses:
// `git status` is clean, yet deleting the worktree loses the commit.
func TestKill_UnpushedCommits_KeepsWorktree(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e, _ := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	time.Sleep(80 * time.Millisecond)
	wtPath := e.WorktreePath
	if wtPath == "" {
		t.Fatal("no worktree created")
	}
	mustWriteFile(t, filepath.Join(wtPath, "committed.txt"), "work")
	runGit(t, wtPath, "add", "committed.txt")
	runGit(t, wtPath, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "work")

	// Not dirty, so Kill does not even prompt — it just closes.
	if err := r.Kill(e.ID, false); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree with an unpushed commit was pruned: %v", err)
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
	src, err := r.Create(context.Background(), wire.CreateSpec{
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
	dup, err := r.Create(context.Background(), wire.CreateSpec{
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

	src, err := r.Create(context.Background(), wire.CreateSpec{
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

	dup, err := r.Create(context.Background(), wire.CreateSpec{
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
	e, _ := r.Create(context.Background(), wire.CreateSpec{
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

// Revive must use the entry's project cwd, ignoring whatever cwd
// the caller passed in opts. This matches the daemon-startup case
// where hived's launch dir (often `/`) is meaningless and using it
// breaks PATH resolution for project-local binaries (e.g. a
// `node_modules/.bin/codex` symlink installed by `npm ci`).
func TestRevive_UsesProjectCwd_NotCallerOpts(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	time.Sleep(80 * time.Millisecond)

	// Pretend the daemon restarted: detach the live session.
	if e.sess != nil {
		_ = e.sess.Close()
		r.mu.Lock()
		e.sess = nil
		r.mu.Unlock()
	}

	// Pass a bogus cwd that does not exist. If Revive honored it,
	// session.Start would fail with ENOENT. If Revive correctly
	// substitutes the project cwd, the session starts cleanly.
	bogus := filepath.Join(t.TempDir(), "does", "not", "exist")
	if err := r.Revive(e.ID, session.Options{Shell: "/bin/bash", Cwd: bogus}); err != nil {
		t.Fatalf("Revive: %v (expected project cwd to override bogus opts.Cwd)", err)
	}
	_ = r.Kill(e.ID, true)
}

// Create must link the project's untracked agent config into the new
// worktree, and doing so must not make the worktree look dirty — a
// pristine session has to stay killable without force.
func TestCreate_WorktreeLinksAgentConfig(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	skillsDir := filepath.Join(p.Cwd, ".claude", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "s.md"), []byte("skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID:   p.ID,
		Shell:       "/bin/bash",
		UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if e.WorktreePath == "" {
		t.Fatalf("worktree not created")
	}

	got, err := os.ReadFile(filepath.Join(e.WorktreePath, ".claude", "skills", "s.md"))
	if err != nil || string(got) != "skill" {
		t.Fatalf("skill not visible in worktree: %q, %v", got, err)
	}

	// The linked config must not register as uncommitted work: Kill
	// without force would otherwise refuse with ErrWorktreeDirty on
	// every pristine session.
	time.Sleep(80 * time.Millisecond)
	if err := r.Kill(e.ID, false); err != nil {
		t.Fatalf("Kill(force=false) on a pristine worktree: %v", err)
	}
}

// TestDisposeWorktree_RefusesUnmanagedPaths is the guard that matters
// most in this package: teardown must never delete a directory hive
// did not create. It is reachable directly now that disposeWorktree is
// its own function, so each refusal can be asserted without staging a
// whole session kill around it.
//
// The IsManaged predicate itself is unit-tested in internal/worktree;
// what is pinned here is that registry teardown actually consults it,
// and leaves the directory on disk when it says no.
func TestDisposeWorktree_RefusesUnmanagedPaths(t *testing.T) {
	r, p := freshRegistryWithProject(t)

	// A directory that is emphatically not a hive worktree: the
	// project's own checkout, with a file in it that must survive.
	precious := filepath.Join(p.Cwd, "PRECIOUS.txt")
	mustWriteFile(t, precious, "do not delete me")

	r.disposeWorktree("sess-1", p.Cwd, p.Cwd, "main", false)
	if _, err := os.Stat(precious); err != nil {
		t.Fatalf("teardown deleted from the project's own checkout: %v", err)
	}

	// Even asked explicitly. removeWorktree means "the user confirmed
	// losing this worktree's work", not "delete whatever path you were
	// handed" — the managed check comes first and is not overridable.
	r.disposeWorktree("sess-1", p.Cwd, p.Cwd, "main", true)
	if _, err := os.Stat(precious); err != nil {
		t.Fatalf("an explicit remove deleted the project's own checkout: %v", err)
	}

	// A sibling directory that is not a worktree of this repo at all.
	outside := t.TempDir()
	keep := filepath.Join(outside, "KEEP.txt")
	mustWriteFile(t, keep, "unrelated")
	r.disposeWorktree("sess-1", p.Cwd, outside, "main", true)
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("teardown deleted an unrelated directory: %v", err)
	}
}

// TestDisposeWorktree_RemovesManagedWorktree is the positive case, so
// the refusals above cannot pass by disposing of nothing at all.
func TestDisposeWorktree_RemovesManagedWorktree(t *testing.T) {
	r, p := freshRegistryWithProject(t)
	sess, err := r.Create(context.Background(), wire.CreateSpec{
		Name: "wt", ProjectID: p.ID, Cols: 80, Rows: 24,
		Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	wtPath := sess.WorktreePath
	if wtPath == "" {
		t.Fatal("create did not produce a worktree")
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree missing before teardown: %v", err)
	}

	r.disposeWorktree(sess.ID, p.Cwd, wtPath, sess.WorktreeBranch, false)
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("pristine managed worktree survived teardown: err=%v", err)
	}
}
