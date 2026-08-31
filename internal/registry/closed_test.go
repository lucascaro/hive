package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

// shellOpts is the spawn config every test here reuses. Restore takes
// the same session.Options the daemon hands Revive on boot.
func shellOpts() session.Options {
	return session.Options{Shell: "/bin/bash", Cols: 80, Rows: 24}
}

// createShell makes a plain session in the default project.
func createShell(t *testing.T, r *Registry) *Entry {
	t.Helper()
	if _, err := r.EnsureDefaultProject(t.TempDir()); err != nil {
		t.Fatalf("EnsureDefaultProject: %v", err)
	}
	e, err := r.Create(context.Background(), wire.CreateSpec{Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return e
}

func TestKillWritesTombstone(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	id, name, color := e.ID, e.Name, e.Color

	if err := r.Kill(id, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	tomb, err := r.readTombstone(id)
	if err != nil {
		t.Fatalf("readTombstone after Kill: %v", err)
	}
	if tomb.Meta.ID != id || tomb.Meta.Name != name || tomb.Meta.Color != color {
		t.Errorf("tombstone meta = %+v, want id/name/color %q/%q/%q", tomb.Meta, id, name, color)
	}
	if tomb.ClosedAt.IsZero() {
		t.Error("tombstone ClosedAt is zero")
	}
}

func TestRestoreRebuildsEntry(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	id, name, color, created := e.ID, e.Name, e.Color, e.Created

	if err := r.Kill(id, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if r.Get(id) != nil {
		t.Fatal("entry still present after Kill")
	}

	got, res, err := r.Restore(id, shellOpts())
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	defer r.Kill(id, true)

	if got == nil {
		t.Fatal("Restore returned a nil entry")
	}
	if got.ID != id || got.Name != name || got.Color != color {
		t.Errorf("restored entry = %q/%q/%q, want %q/%q/%q", got.ID, got.Name, got.Color, id, name, color)
	}
	if !got.Created.Equal(created) {
		t.Errorf("Created = %v, want %v (the original creation time, not the restore time)", got.Created, created)
	}
	// A plain shell has no agent conversation, so nothing is degraded.
	if !res.Clean() {
		t.Errorf("restoring a plain shell should be a clean undo, got %+v", res)
	}
	// It must be back in the list, not merely in the map.
	var found bool
	for _, info := range r.List() {
		if info.ID == id {
			found = true
		}
	}
	if !found {
		t.Error("restored session missing from List()")
	}
}

func TestRestoreDropsTheTombstone(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	id := e.ID

	if err := r.Kill(id, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, _, err := r.Restore(id, shellOpts()); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	defer r.Kill(id, true)

	if _, err := r.readTombstone(id); !errors.Is(err, ErrNotFound) {
		t.Errorf("tombstone survived a successful restore (err=%v); a second undo would duplicate the session", err)
	}
}

func TestRestoreTwiceSecondFails(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	id := e.ID

	if err := r.Kill(id, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, _, err := r.Restore(id, shellOpts()); err != nil {
		t.Fatalf("first Restore: %v", err)
	}
	defer r.Kill(id, true)

	_, _, err := r.Restore(id, shellOpts())
	if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrExists) {
		t.Errorf("second Restore err = %v, want ErrNotFound or ErrExists", err)
	}
}

func TestRestoreMissingTombstoneNotFound(t *testing.T) {
	r := freshRegistry(t)
	if _, _, err := r.Restore("nope", shellOpts()); !errors.Is(err, ErrNotFound) {
		t.Errorf("Restore of unknown id = %v, want ErrNotFound", err)
	}
}

func TestRestoreAppendsToEndOfOrder(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	first := createShell(t, r)
	second := createShell(t, r)
	third := createShell(t, r)
	defer r.Kill(second.ID, true)
	defer r.Kill(third.ID, true)

	// Close the FIRST one, so a restore that used the remembered Order
	// would splice it back to the head.
	if err := r.Kill(first.ID, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, _, err := r.Restore(first.ID, shellOpts()); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	defer r.Kill(first.ID, true)

	list := r.List()
	if len(list) != 3 {
		t.Fatalf("List has %d sessions, want 3", len(list))
	}
	if list[len(list)-1].ID != first.ID {
		t.Errorf("restored session is at position %v, want last; order = %v", list, listIDs(list))
	}
	// Order values must be a dense 0..n-1 after the reindex.
	for i, info := range list {
		if info.Order != i {
			t.Errorf("session %d has Order %d, want %d (reindex did not run)", i, info.Order, i)
		}
	}
}

func listIDs(infos []wire.SessionInfo) []string {
	out := make([]string, 0, len(infos))
	for _, i := range infos {
		out = append(out, i.ID)
	}
	return out
}

func TestRestoreIntoDefaultProjectWhenProjectGone(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	if _, err := r.EnsureDefaultProject(t.TempDir()); err != nil {
		t.Fatalf("EnsureDefaultProject: %v", err)
	}
	doomed, err := r.CreateProject(wire.CreateProjectReq{Name: "doomed", Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	e, err := r.Create(context.Background(), wire.CreateSpec{ProjectID: doomed.ID, Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := e.ID

	if err := r.Kill(id, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if err := r.KillProject(doomed.ID, true); err != nil {
		t.Fatalf("KillProject: %v", err)
	}

	got, res, err := r.Restore(id, shellOpts())
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	defer r.Kill(id, true)

	if got.ProjectID == doomed.ID {
		t.Error("restored into the deleted project; that is a dangling reference")
	}
	if !res.ProjectReassigned {
		t.Error("ProjectReassigned not reported; the UI would claim a clean undo")
	}
}

func TestRestoreReattachesSurvivingWorktree(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id, wtPath, wtBranch := e.ID, e.WorktreePath, e.WorktreeBranch
	if wtPath == "" {
		t.Skip("worktree creation unavailable in this environment")
	}
	// Dirty it, so the ordinary close keeps the directory.
	mustWriteFile(t, filepath.Join(wtPath, "scratch.txt"), "work in progress\n")

	if err := r.Kill(id, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, statErr := os.Stat(wtPath); statErr != nil {
		t.Fatalf("closing a session with a dirty worktree deleted it: %v", statErr)
	}

	got, res, err := r.Restore(id, shellOpts())
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	defer r.Kill(id, true)

	if got.WorktreePath != wtPath || got.WorktreeBranch != wtBranch {
		t.Errorf("worktree binding = %q/%q, want %q/%q", got.WorktreePath, got.WorktreeBranch, wtPath, wtBranch)
	}
	if res.WorktreeRecreated || res.WorktreeLost {
		t.Errorf("an intact worktree should be adopted as-is, got %+v", res)
	}
	// The uncommitted file is still there — nothing deleted it.
	if _, err := os.Stat(filepath.Join(wtPath, "scratch.txt")); err != nil {
		t.Errorf("uncommitted work vanished across close+restore: %v", err)
	}
}

func TestRestoreRecreatesWorktreeFromBranch(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id, wtPath, wtBranch := e.ID, e.WorktreePath, e.WorktreeBranch
	if wtPath == "" {
		t.Skip("worktree creation unavailable in this environment")
	}
	// Commit something so there is work to prove came back.
	mustWriteFile(t, filepath.Join(wtPath, "committed.txt"), "kept\n")
	runGit(t, wtPath, "add", "committed.txt")
	runGit(t, wtPath, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "work")

	// Explicitly destructive close: deletes the worktree directory.
	if err := r.KillAndRemoveWorktree(id, true); err != nil {
		t.Fatalf("KillAndRemoveWorktree: %v", err)
	}
	if _, statErr := os.Stat(wtPath); statErr == nil {
		t.Fatal("worktree still on disk after an explicit delete")
	}

	got, res, err := r.Restore(id, shellOpts())
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	defer r.Kill(id, true)

	if got.WorktreePath != wtPath {
		t.Fatalf("worktree not recreated at %q, got %q", wtPath, got.WorktreePath)
	}
	if !res.WorktreeRecreated {
		t.Error("WorktreeRecreated not reported; the UI would not warn about lost uncommitted work")
	}
	if got.WorktreeBranch != wtBranch {
		t.Errorf("branch = %q, want %q", got.WorktreeBranch, wtBranch)
	}
	// Committed work is back; that is the whole point of recreating
	// from the branch rather than starting fresh.
	if _, err := os.Stat(filepath.Join(wtPath, "committed.txt")); err != nil {
		t.Errorf("committed work did not come back: %v", err)
	}
}

func TestRestoreWithoutWorktreeWhenBranchGone(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id, wtPath, wtBranch := e.ID, e.WorktreePath, e.WorktreeBranch
	if wtPath == "" {
		t.Skip("worktree creation unavailable in this environment")
	}
	if err := r.KillAndRemoveWorktree(id, true); err != nil {
		t.Fatalf("KillAndRemoveWorktree: %v", err)
	}
	runGit(t, p.Cwd, "branch", "-D", wtBranch)

	got, res, err := r.Restore(id, shellOpts())
	if err != nil {
		t.Fatalf("Restore should degrade, not fail: %v", err)
	}
	defer r.Kill(id, true)

	if got.WorktreePath != "" {
		t.Errorf("WorktreePath = %q, want empty when the branch is gone", got.WorktreePath)
	}
	if !res.WorktreeLost {
		t.Error("WorktreeLost not reported")
	}
}

func TestRestoreRefusesUnmanagedWorktreePath(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e := createShellIn(t, r, p)
	id := e.ID

	// Point the tombstone at a path outside hive's managed directory
	// that does not exist. Restore must never materialize a directory
	// there — the path came off disk and is not trusted.
	outside := filepath.Join(t.TempDir(), "not-hive-managed")
	if err := r.Kill(id, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	tomb, err := r.readTombstone(id)
	if err != nil {
		t.Fatalf("readTombstone: %v", err)
	}
	tomb.Meta.WorktreePath = outside
	tomb.Meta.WorktreeBranch = "main"
	if err := writeJSON(r.tombstonePath(id), tomb); err != nil {
		t.Fatalf("rewrite tombstone: %v", err)
	}

	got, res, err := r.Restore(id, shellOpts())
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	defer r.Kill(id, true)

	if got.WorktreePath != "" {
		t.Errorf("adopted an unmanaged path %q", got.WorktreePath)
	}
	if _, statErr := os.Stat(outside); statErr == nil {
		t.Errorf("restore created a directory outside the managed namespace at %q", outside)
	}
	if !res.WorktreeLost {
		t.Error("WorktreeLost not reported")
	}
}

// createShellIn makes a plain (worktree-less) session in project p.
func createShellIn(t *testing.T, r *Registry, p *Project) *Entry {
	t.Helper()
	e, err := r.Create(context.Background(), wire.CreateSpec{ProjectID: p.ID, Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return e
}

func TestKillDumpsPatchBeforeWorktreeDelete(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id, wtPath := e.ID, e.WorktreePath
	if wtPath == "" {
		t.Skip("worktree creation unavailable in this environment")
	}
	// One tracked modification and one untracked file — the patch has
	// to carry both or it is not a recovery.
	runGit(t, wtPath, "-c", "user.email=t@t", "-c", "user.name=t",
		"commit", "--allow-empty", "-q", "-m", "base")
	mustWriteFile(t, filepath.Join(wtPath, "tracked.txt"), "v1\n")
	runGit(t, wtPath, "add", "tracked.txt")
	runGit(t, wtPath, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "add tracked")
	mustWriteFile(t, filepath.Join(wtPath, "tracked.txt"), "v2-uncommitted\n")
	mustWriteFile(t, filepath.Join(wtPath, "untracked.txt"), "brand new\n")

	if err := r.KillAndRemoveWorktree(id, true); err != nil {
		t.Fatalf("KillAndRemoveWorktree: %v", err)
	}

	tomb, err := r.readTombstone(id)
	if err != nil {
		t.Fatalf("readTombstone: %v", err)
	}
	if tomb.PatchPath == "" {
		t.Fatal("no recovery patch recorded; deleting a dirty worktree lost work with no trace")
	}
	body, err := os.ReadFile(tomb.PatchPath)
	if err != nil {
		t.Fatalf("read recovery patch: %v", err)
	}
	if !strings.Contains(string(body), "v2-uncommitted") {
		t.Error("patch is missing the tracked modification")
	}
	if !strings.Contains(string(body), "untracked.txt") {
		t.Error("patch is missing the untracked file")
	}

	// And the restore surfaces it rather than making the user hunt.
	_, res, err := r.Restore(id, shellOpts())
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	defer r.Kill(id, true)
	if res.PatchPath != tomb.PatchPath {
		t.Errorf("RestoreResult.PatchPath = %q, want %q", res.PatchPath, tomb.PatchPath)
	}
}

func TestKillWritesNoPatchWhenWorktreeIsClean(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := e.ID
	if e.WorktreePath == "" {
		t.Skip("worktree creation unavailable in this environment")
	}

	if err := r.KillAndRemoveWorktree(id, true); err != nil {
		t.Fatalf("KillAndRemoveWorktree: %v", err)
	}
	tomb, err := r.readTombstone(id)
	if err != nil {
		t.Fatalf("readTombstone: %v", err)
	}
	if tomb.PatchPath != "" {
		t.Errorf("wrote a recovery patch for a clean worktree (%q); it would recover nothing", tomb.PatchPath)
	}
	if tomb.PatchSkipped {
		t.Error("PatchSkipped set for a clean worktree; that claims work was at stake")
	}
}

func TestPatchSkippedAboveCap(t *testing.T) {
	skipNonPosix(t)
	repo := initGitRepo(t)
	// Two files whose combined diff comfortably exceeds a 1 KiB cap.
	mustWriteFile(t, filepath.Join(repo, "big.txt"), strings.Repeat("padding line\n", 500))
	out := filepath.Join(t.TempDir(), "recovery.patch")

	err := worktree.DumpPatch(repo, out, 1024)
	if !errors.Is(err, worktree.ErrPatchTooLarge) {
		t.Fatalf("DumpPatch err = %v, want ErrPatchTooLarge", err)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("an over-cap dump still wrote a file; a truncated patch is worse than none")
	}
}

func TestPatchDumpFailureDoesNotBlockClose(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	if _, err := r.EnsureDefaultProject(t.TempDir()); err != nil {
		t.Fatalf("EnsureDefaultProject: %v", err)
	}
	// A non-git project: the entry gets no worktree, so the dump path
	// is skipped entirely and the close must still complete.
	e := createShell(t, r)
	id := e.ID
	if err := r.KillAndRemoveWorktree(id, true); err != nil {
		t.Fatalf("close failed when no patch could be taken: %v", err)
	}
	if _, err := r.readTombstone(id); err != nil {
		t.Errorf("no tombstone written: %v", err)
	}
}

func TestTombstonePruneKeepsLastNAndSevenDays(t *testing.T) {
	r := freshRegistry(t)

	// One more than the count bound, all recent.
	for i := range maxTombstones + 5 {
		id := "recent-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		writeTestTombstone(t, r, id, time.Now().UTC().Add(-time.Duration(i)*time.Minute))
	}
	// One well inside the count bound but older than the age bound.
	writeTestTombstone(t, r, "ancient", time.Now().UTC().Add(-8*24*time.Hour))

	r.pruneTombstones()

	got := r.listTombstones()
	if len(got) > maxTombstones {
		t.Errorf("prune left %d tombstones, want at most %d", len(got), maxTombstones)
	}
	for _, tomb := range got {
		if tomb.Meta.ID == "ancient" {
			t.Error("prune kept a tombstone older than the age bound")
		}
	}
	// Newest survives — pruning must not evict the one thing undo needs.
	if len(got) == 0 || got[0].Meta.ID != "recent-aa" {
		t.Errorf("newest tombstone did not survive the prune; got %v", got)
	}
}

func TestPruneRemovesTheRecoveryPatchToo(t *testing.T) {
	r := freshRegistry(t)
	writeTestTombstone(t, r, "old", time.Now().UTC().Add(-8*24*time.Hour))
	patch := r.patchPath("old")
	if err := os.WriteFile(patch, []byte("diff --git a/x b/x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	r.pruneTombstones()

	if _, err := os.Stat(patch); err == nil {
		t.Error("prune dropped the tombstone but left its recovery patch behind, leaking disk forever")
	}
}

func writeTestTombstone(t *testing.T, r *Registry, id string, closedAt time.Time) {
	t.Helper()
	tomb := Tombstone{Meta: MetaFile{ID: id, Name: id}, ClosedAt: closedAt}
	if err := writeJSON(r.tombstonePath(id), tomb); err != nil {
		t.Fatalf("write tombstone %s: %v", id, err)
	}
}

func TestOrphanReclaimSkipsTombstonedWorktree(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id, wtPath := e.ID, e.WorktreePath
	if wtPath == "" {
		t.Skip("worktree creation unavailable in this environment")
	}
	// Dirty it so the ordinary close keeps the directory, leaving
	// exactly the shape boot-time reclaim would find: a worktree on
	// disk that no live session claims.
	mustWriteFile(t, filepath.Join(wtPath, "wip.txt"), "unfinished\n")
	if err := r.Kill(id, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	if !r.worktreeClaimed(wtPath) {
		t.Fatal("a tombstoned worktree reads as unclaimed; boot reclaim would delete what the user can still undo into")
	}

	// And the full scan+reclaim leaves it alone.
	r.ReclaimOrphanWorktrees(context.Background(), r.ScanOrphanWorktrees())
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("orphan reclaim deleted a tombstoned worktree: %v", err)
	}
}

func TestListClosedIsNewestFirst(t *testing.T) {
	r := freshRegistry(t)
	now := time.Now().UTC()
	writeTestTombstone(t, r, "older", now.Add(-time.Hour))
	writeTestTombstone(t, r, "newest", now)
	writeTestTombstone(t, r, "middle", now.Add(-time.Minute))

	got := r.ListClosed()
	if len(got) != 3 {
		t.Fatalf("ListClosed returned %d, want 3", len(got))
	}
	want := []string{"newest", "middle", "older"}
	for i, w := range want {
		if got[i].SessionID != w {
			t.Errorf("position %d = %q, want %q (reopen-last would pick the wrong session)", i, got[i].SessionID, w)
		}
	}
}
