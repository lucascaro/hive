// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// dirtyWorktreeSession makes a worktree session whose worktree holds an
// uncommitted file, and returns the entry and that file's path.
func dirtyWorktreeSession(t *testing.T, r *Registry, p *Project) (*Entry, string) {
	t.Helper()
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if e.WorktreePath == "" {
		t.Fatal("no worktree created")
	}
	time.Sleep(80 * time.Millisecond)
	f := filepath.Join(e.WorktreePath, "precious.txt")
	mustWriteFile(t, f, "uncommitted work\n")
	return e, f
}

// setProjectCwd re-points a project, standing in for a project dir
// that was moved, deleted or stopped being a git repo.
func setProjectCwd(r *Registry, id, cwd string) {
	r.mu.Lock()
	r.projects[id].Cwd = cwd
	r.mu.Unlock()
}

// When worktree.Root fails (project dir not a repo, deleted, git
// missing or timing out) hive can neither verify it manages the
// worktree nor that it holds nothing. Closing a session must never be
// what deletes it — with or without the explicit remove request.
func TestKill_RootFailureKeepsDirtyWorktree(t *testing.T) {
	cases := []struct {
		name           string
		cwd            func(t *testing.T) string
		removeWorktree bool
	}{
		{"not a repo", func(t *testing.T) string { return t.TempDir() }, false},
		{"not a repo, remove requested", func(t *testing.T) string { return t.TempDir() }, true},
		{"project dir gone", func(t *testing.T) string { return filepath.Join(t.TempDir(), "gone") }, false},
		{"project dir gone, remove requested", func(t *testing.T) string { return filepath.Join(t.TempDir(), "gone") }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			skipNonPosix(t)
			r, p := freshRegistryWithProject(t)
			e, file := dirtyWorktreeSession(t, r, p)
			wt := e.WorktreePath
			if err := r.SetWorktreeLabel(p.ID, wt, "keep me"); err != nil {
				t.Fatalf("SetWorktreeLabel: %v", err)
			}
			setProjectCwd(r, p.ID, tc.cwd(t))

			var err error
			if tc.removeWorktree {
				err = r.KillAndRemoveWorktree(e.ID, true)
			} else {
				err = r.Kill(e.ID, true)
			}
			if err != nil {
				t.Fatalf("kill: %v", err)
			}
			if _, err := os.Stat(file); err != nil {
				t.Fatalf("uncommitted file destroyed when the project root could not be resolved: %v", err)
			}
			if got := labelOf(t, r, p.ID, wt); got != "keep me" {
				t.Errorf("label = %q, want it kept alongside the kept worktree", got)
			}
		})
	}
}

// corruptWorktree makes `git status` fail inside wt while leaving the
// directory and its files in place.
func corruptWorktree(t *testing.T, wt string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /nonexistent/hive-test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// If `git status` itself fails, hive does not know the worktree is
// clean. The unforced kill must refuse, not read the error as clean.
func TestKill_StatusFailureRefusesUnforcedKill(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e, file := dirtyWorktreeSession(t, r, p)
	defer r.Kill(e.ID, true)
	corruptWorktree(t, e.WorktreePath)

	err := r.Kill(e.ID, false)
	if !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("Kill with a failing git status = %v, want ErrWorktreeDirty", err)
	}
	if r.Get(e.ID) == nil {
		t.Error("entry removed by a refused kill")
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("file destroyed: %v", err)
	}
}

// DeleteArchived's Root handling was already fail-closed; this pins it.
// Even with force and an explicit removal, an unresolvable project
// keeps the worktree.
func TestDeleteArchived_RootFailureKeepsDirtyWorktree(t *testing.T) {
	for name, cwd := range map[string]func(*testing.T) string{
		"not a repo":      func(t *testing.T) string { return t.TempDir() },
		"project removed": func(*testing.T) string { return "" },
	} {
		t.Run(name, func(t *testing.T) {
			skipNonPosix(t)
			r, p := freshRegistryWithProject(t)
			e := archivedWorktreeSession(t, r, p)
			file := filepath.Join(e.WorktreePath, "wip.txt")
			mustWriteFile(t, file, "unfinished\n")
			if err := r.Archive(e.ID); err != nil {
				t.Fatal(err)
			}
			repo := p.Cwd
			setProjectCwd(r, p.ID, cwd(t))
			if name == "project removed" {
				// The daemon happens to run inside a repo: with an
				// empty cwd a git lookup would resolve THAT repo.
				t.Chdir(repo)
			}
			kept, err := r.DeleteArchived(e.ID, true, true)
			if err != nil || !kept {
				t.Fatalf("DeleteArchived = kept %v, err %v; want kept, nil", kept, err)
			}
			if _, err := os.Stat(file); err != nil {
				t.Errorf("file destroyed: %v", err)
			}
		})
	}
}

// KillProject(killSessions) drops the project BEFORE killing its
// sessions, so disposeWorktree sees an empty project cwd, and running
// git with -C "" resolves the daemon's own directory. An empty cwd must
// count as a Root failure, not a lookup against whatever repo the
// daemon happens to run in.
func TestKillProject_KillSessionsKeepsDirtyWorktree(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	if _, err := r.CreateProject(wire.CreateProjectReq{Name: "other", Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	_, file := dirtyWorktreeSession(t, r, p)
	t.Chdir(t.TempDir()) // a daemon running outside any repo
	if err := r.KillProject(p.ID, true, true); err != nil {
		t.Fatalf("KillProject: %v", err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("removing the project destroyed a dirty worktree: %v", err)
	}
}

func TestDeleteArchived_StatusFailureRefusesWithoutForce(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e := archivedWorktreeSession(t, r, p)
	wt := e.WorktreePath
	mustWriteFile(t, filepath.Join(wt, "wip.txt"), "unfinished\n")
	if err := r.Archive(e.ID); err != nil {
		t.Fatal(err)
	}
	corruptWorktree(t, wt)
	if _, err := r.DeleteArchived(e.ID, true, false); !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("DeleteArchived with a failing git status = %v, want ErrWorktreeDirty", err)
	}
	if _, err := os.Stat(filepath.Join(wt, "wip.txt")); err != nil {
		t.Errorf("file destroyed: %v", err)
	}
}
