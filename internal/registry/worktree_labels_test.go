package registry

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

// openAt is freshRegistry without the temp dir, so a test can close a
// registry and reopen the SAME state dir to prove a write reached disk.
func openAt(t *testing.T, dir string) *Registry {
	t.Helper()
	r, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(%s): %v", dir, err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func labelOf(t *testing.T, r *Registry, projectID, path string) string {
	t.Helper()
	for _, pi := range r.ListProjects() {
		if pi.ID == projectID {
			return pi.WorktreeLabels[path]
		}
	}
	t.Fatalf("project %s not found", projectID)
	return ""
}

// The label is state, not a view: it has to survive the daemon that
// wrote it. Asserting on the in-memory map only would pass even if
// persistProjectLocked were never called.
func TestSetWorktreeLabel_PersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	r := openAt(t, dir)
	p, err := r.CreateProject(wire.CreateProjectReq{Name: "proj", Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	const path = "/repo/.worktrees/auth"
	if err := r.SetWorktreeLabel(p.ID, path, "auth refactor"); err != nil {
		t.Fatalf("SetWorktreeLabel: %v", err)
	}
	if got := labelOf(t, r, p.ID, path); got != "auth refactor" {
		t.Fatalf("in-memory label = %q, want %q", got, "auth refactor")
	}
	_ = r.Close()

	reopened := openAt(t, dir)
	if got := labelOf(t, reopened, p.ID, path); got != "auth refactor" {
		t.Errorf("label after reload = %q, want %q", got, "auth refactor")
	}
}

// Clearing deletes the key. Storing "" would round-trip through
// project.json forever as an entry that means nothing.
func TestSetWorktreeLabel_ClearsOnEmptyString(t *testing.T) {
	r := freshRegistry(t)
	p, err := r.CreateProject(wire.CreateProjectReq{Name: "proj", Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	const path = "/repo/.worktrees/auth"
	if err := r.SetWorktreeLabel(p.ID, path, "auth refactor"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := r.SetWorktreeLabel(p.ID, path, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	for _, pi := range r.ListProjects() {
		if pi.ID != p.ID {
			continue
		}
		if _, present := pi.WorktreeLabels[path]; present {
			t.Errorf("key still present after clear: %#v", pi.WorktreeLabels)
		}
	}
}

// Whitespace-only is a clear, not a label made of spaces.
func TestSetWorktreeLabel_TrimsAndTreatsBlankAsClear(t *testing.T) {
	r := freshRegistry(t)
	p, _ := r.CreateProject(wire.CreateProjectReq{Name: "proj", Cwd: t.TempDir()})
	const path = "/repo/.worktrees/auth"

	if err := r.SetWorktreeLabel(p.ID, path, "  spaced  "); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := labelOf(t, r, p.ID, path); got != "spaced" {
		t.Errorf("label = %q, want %q", got, "spaced")
	}
	if err := r.SetWorktreeLabel(p.ID, path, "   "); err != nil {
		t.Fatalf("blank: %v", err)
	}
	if got := labelOf(t, r, p.ID, path); got != "" {
		t.Errorf("blank label did not clear, got %q", got)
	}
}

// THE point of the feature. Every other worktree mutation refuses while
// a session lives in the worktree, because they move or delete the
// directory under a running shell. A label touches neither, and naming a
// group of RUNNING sessions is exactly what it is for — so this must
// succeed. Without this test, someone copying RenameWorktree's
// liveSessionsIn guard into SetWorktreeLabel would break the feature and
// no other test would notice.
func TestSetWorktreeLabel_AllowedWithLiveSession(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer r.Kill(e.ID, true)
	time.Sleep(80 * time.Millisecond)

	if err := r.SetWorktreeLabel(p.ID, e.WorktreePath, "live work"); err != nil {
		t.Fatalf("SetWorktreeLabel with a live session: %v", err)
	}
	if got := labelOf(t, r, p.ID, e.WorktreePath); got != "live work" {
		t.Errorf("label = %q, want %q", got, "live work")
	}
	// The session and its directory are untouched — a label renames
	// nothing.
	if _, err := os.Stat(e.WorktreePath); err != nil {
		t.Errorf("worktree moved: %v", err)
	}
	if e.Name == "live work" {
		t.Error("labelling the group renamed the session")
	}
}

func TestSetWorktreeLabel_UnknownProject(t *testing.T) {
	r := freshRegistry(t)
	if err := r.SetWorktreeLabel("nope", "/repo/.worktrees/a", "x"); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("got %v, want ErrProjectNotFound", err)
	}
}

func TestSetWorktreeLabel_EmptyPathRejected(t *testing.T) {
	r := freshRegistry(t)
	p, _ := r.CreateProject(wire.CreateProjectReq{Name: "proj", Cwd: t.TempDir()})
	if err := r.SetWorktreeLabel(p.ID, "", "x"); err == nil {
		t.Fatal("empty path accepted; a label with no worktree is unreachable")
	}
}

// Every project.json written before this feature existed lacks the key.
// It must load as an empty map, not as an error and not as a panic on
// first read.
func TestLoad_ToleratesProjectJSONWithoutLabels(t *testing.T) {
	dir := t.TempDir()
	r := openAt(t, dir)
	p, err := r.CreateProject(wire.CreateProjectReq{Name: "proj", Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	_ = r.Close()

	// Rewrite project.json the way an older hive would have: no
	// worktree_labels key at all.
	meta := filepath.Join(ProjectsDir(dir), p.ID, "project.json")
	raw, err := os.ReadFile(meta)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	delete(m, "worktree_labels")
	out, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(meta, out, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	reopened := openAt(t, dir)
	if got := labelOf(t, reopened, p.ID, "/anything"); got != "" {
		t.Errorf("label from a legacy project.json = %q, want empty", got)
	}
	// And a write onto the nil map must not panic.
	if err := reopened.SetWorktreeLabel(p.ID, "/repo/.worktrees/a", "x"); err != nil {
		t.Fatalf("SetWorktreeLabel onto a nil map: %v", err)
	}
}

// Info() hands its map to callers that read it after r.mu is released,
// so it must hand out a copy. Aliasing the live map is a data race that
// only shows up under -race with a concurrent write.
func TestProjectInfo_WorktreeLabelsAreCopied(t *testing.T) {
	r := freshRegistry(t)
	p, _ := r.CreateProject(wire.CreateProjectReq{Name: "proj", Cwd: t.TempDir()})
	const path = "/repo/.worktrees/a"
	if err := r.SetWorktreeLabel(p.ID, path, "first"); err != nil {
		t.Fatalf("set: %v", err)
	}
	info := r.ListProjects()
	var snapshot map[string]string
	for _, pi := range info {
		if pi.ID == p.ID {
			snapshot = pi.WorktreeLabels
		}
	}
	if err := r.SetWorktreeLabel(p.ID, path, "second"); err != nil {
		t.Fatalf("set again: %v", err)
	}
	if snapshot[path] != "first" {
		t.Errorf("earlier snapshot mutated to %q; Info() aliased the live map", snapshot[path])
	}
}

// A label outliving its worktree is not merely untidy. Worktree paths are
// derived from the branch (worktree.WorktreePath), so re-creating a
// worktree on the same branch lands on the SAME path — and an unpruned
// label means the new group silently comes up wearing the deleted one's
// name. That resurrection is the bug; the unbounded map growth is the
// lesser half.
func TestRemoveWorktree_PrunesTheLabel(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	repo := p.Cwd
	path := newWorktree(t, repo, "labelled")

	if err := r.SetWorktreeLabel(p.ID, path, "auth refactor"); err != nil {
		t.Fatalf("SetWorktreeLabel: %v", err)
	}
	if got := labelOf(t, r, p.ID, path); got != "auth refactor" {
		t.Fatalf("label = %q, want %q", got, "auth refactor")
	}

	if err := r.RemoveWorktree(p.ID, path, true, true, false); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if got := labelOf(t, r, p.ID, path); got != "" {
		t.Errorf("label survived removal: %q", got)
	}

	// And a worktree re-created at the same path does not inherit it.
	again := newWorktree(t, repo, "labelled")
	if again != path {
		t.Fatalf("re-created worktree landed at %q, want %q — test no longer exercises the collision", again, path)
	}
	if got := labelOf(t, r, p.ID, again); got != "" {
		t.Errorf("re-created worktree inherited the deleted group's name: %q", got)
	}
}

// The name follows the directory. Without the re-key the sidebar looks
// the label up under the new path and finds nothing, so the name the
// operator gave the group silently disappears on a branch rename.
func TestRenameWorktree_MovesTheLabel(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	repo := p.Cwd
	path := newWorktree(t, repo, "before")

	if err := r.SetWorktreeLabel(p.ID, path, "auth refactor"); err != nil {
		t.Fatalf("SetWorktreeLabel: %v", err)
	}
	if err := r.RenameWorktree(p.ID, path, "after"); err != nil {
		t.Fatalf("RenameWorktree: %v", err)
	}

	// MainRoot, not the raw cwd: the registry derives the destination from
	// the git-resolved root, and so does every session's WorktreePath
	// (create.go -> ResolveBranchAndPath). On macOS the two spellings
	// differ (/var vs /private/var), so computing the expectation from the
	// unresolved cwd would fail here while the real lookup — which uses
	// the resolved form on both sides — succeeds.
	root, err := worktree.MainRoot(repo)
	if err != nil {
		t.Fatalf("MainRoot: %v", err)
	}
	dest := worktree.WorktreePath(root, "after")
	if got := labelOf(t, r, p.ID, dest); got != "auth refactor" {
		t.Errorf("label at the new path = %q, want %q", got, "auth refactor")
	}
	if got := labelOf(t, r, p.ID, path); got != "" {
		t.Errorf("label still present at the old path: %q", got)
	}
}

// SetWorktreeLabel stores the client's verbatim spelling while the
// worktree mutations work in managedPath-resolved form. On macOS those
// differ (/var vs /private/var), so a prune that compared keys literally
// would miss every label set through the unresolved spelling — and would
// do so only on macOS, which is how it would escape notice.
func TestRemoveWorktree_PrunesALabelSetUnderAnUnresolvedPath(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	repo := p.Cwd
	path := newWorktree(t, repo, "spelling")

	// The spelling the GUI would send: whatever the session carries,
	// which is not run through ResolvePath.
	unresolved := filepath.Join(repo, ".worktrees", "spelling")
	if err := r.SetWorktreeLabel(p.ID, unresolved, "auth refactor"); err != nil {
		t.Fatalf("SetWorktreeLabel: %v", err)
	}

	if err := r.RemoveWorktree(p.ID, path, true, true, false); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if got := labelOf(t, r, p.ID, unresolved); got != "" {
		t.Errorf("label set under the unresolved spelling survived removal: %q", got)
	}
}

// Removing an unlabelled worktree must not touch the project or emit a
// spurious broadcast.
func TestRemoveWorktree_UnlabelledIsANoOp(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	repo := p.Cwd
	kept := newWorktree(t, repo, "kept")
	doomed := newWorktree(t, repo, "doomed")

	if err := r.SetWorktreeLabel(p.ID, kept, "keep me"); err != nil {
		t.Fatalf("SetWorktreeLabel: %v", err)
	}
	if err := r.RemoveWorktree(p.ID, doomed, true, true, false); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if got := labelOf(t, r, p.ID, kept); got != "keep me" {
		t.Errorf("unrelated label was disturbed: %q", got)
	}
}

// The label is operator-supplied and goes straight into project.json and
// a broadcast to every open window, so it needs a ceiling that is not
// "the wire's 1 MiB frame cap". Rejected rather than truncated, like an
// over-long idea: a name silently shortened is a name nobody chose.
func TestSetWorktreeLabel_RejectsAnOverlongLabel(t *testing.T) {
	r := freshRegistry(t)
	p, _ := r.CreateProject(wire.CreateProjectReq{Name: "proj", Cwd: t.TempDir()})
	const path = "/repo/.worktrees/auth"

	if err := r.SetWorktreeLabel(p.ID, path, "fits"); err != nil {
		t.Fatalf("baseline set: %v", err)
	}
	long := strings.Repeat("x", wire.MaxWorktreeLabel+1)
	err := r.SetWorktreeLabel(p.ID, path, long)
	if !errors.Is(err, ErrWorktreeLabelTooLong) {
		t.Fatalf("got %v, want ErrWorktreeLabelTooLong", err)
	}
	// Refused means unchanged — not cleared, and not half-applied.
	if got := labelOf(t, r, p.ID, path); got != "fits" {
		t.Errorf("a refused label disturbed the stored one: %q", got)
	}
}

// Exactly at the limit is allowed; the bound is inclusive.
func TestSetWorktreeLabel_AcceptsExactlyTheLimit(t *testing.T) {
	r := freshRegistry(t)
	p, _ := r.CreateProject(wire.CreateProjectReq{Name: "proj", Cwd: t.TempDir()})
	const path = "/repo/.worktrees/auth"

	exact := strings.Repeat("x", wire.MaxWorktreeLabel)
	if err := r.SetWorktreeLabel(p.ID, path, exact); err != nil {
		t.Fatalf("a label of exactly the limit was refused: %v", err)
	}
	if got := labelOf(t, r, p.ID, path); got != exact {
		t.Errorf("stored label is %d bytes, want %d", len(got), len(exact))
	}
}

// The key is stored verbatim and rides the same persist-and-broadcast
// path as the label, so bounding one without the other is not a bound.
func TestSetWorktreeLabel_RejectsAnOverlongPath(t *testing.T) {
	r := freshRegistry(t)
	p, _ := r.CreateProject(wire.CreateProjectReq{Name: "proj", Cwd: t.TempDir()})

	long := "/" + strings.Repeat("p", wire.MaxWorktreePath)
	if err := r.SetWorktreeLabel(p.ID, long, "x"); err == nil {
		t.Fatal("an over-long worktree path was accepted")
	}
	// Nothing was filed under it.
	if got := labelOf(t, r, p.ID, long); got != "" {
		t.Errorf("a refused path was stored anyway: %q", got)
	}
}

// The common teardown path, and the one the explicit-remove fix missed.
// Closing the LAST session in a named group disposes the worktree through
// Kill -> disposeWorktree, not through RemoveWorktree — so a prune wired
// only into the latter leaves the name behind on the path people actually
// take. Same resurrect consequence: a worktree re-created on the branch
// lands on the same path and inherits the dead group's name.
func TestKill_PrunesTheLabelOfTheWorktreeItDisposes(t *testing.T) {
	skipNonPosix(t)
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
	if err := r.SetWorktreeLabel(p.ID, wtPath, "auth refactor"); err != nil {
		t.Fatalf("SetWorktreeLabel: %v", err)
	}
	time.Sleep(80 * time.Millisecond)

	if err := r.Kill(sess.ID, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatalf("worktree survived the kill, so this test proves nothing: err=%v", err)
	}
	if got := labelOf(t, r, p.ID, wtPath); got != "" {
		t.Errorf("label survived the kill that disposed its worktree: %q", got)
	}
}

// A worktree kept because it holds work must KEEP its name — the session
// is gone but the group is not, and the worktree is still in the browser.
func TestKill_KeepsTheLabelWhenItKeepsTheWorktree(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	sess, err := r.Create(context.Background(), wire.CreateSpec{
		Name: "wt", ProjectID: p.ID, Cols: 80, Rows: 24,
		Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	wtPath := sess.WorktreePath
	if err := r.SetWorktreeLabel(p.ID, wtPath, "auth refactor"); err != nil {
		t.Fatalf("SetWorktreeLabel: %v", err)
	}
	// Uncommitted work makes the worktree non-pristine, so teardown
	// keeps it.
	mustWriteFile(t, filepath.Join(wtPath, "WIP.txt"), "unfinished")
	time.Sleep(80 * time.Millisecond)

	// force: the dirty worktree is what makes teardown KEEP it, and a
	// non-forced kill refuses outright on the same condition.
	if err := r.Kill(sess.ID, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("a dirty worktree was deleted, so this test proves nothing: %v", err)
	}
	if got := labelOf(t, r, p.ID, wtPath); got != "auth refactor" {
		t.Errorf("label = %q, want it kept alongside the kept worktree", got)
	}
}
