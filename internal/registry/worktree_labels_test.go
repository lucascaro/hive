package registry

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
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
