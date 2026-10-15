package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

// archivedWorktreeSession creates a worktree-backed session and returns
// it with its path; it skips when worktrees are unavailable.
func archivedWorktreeSession(t *testing.T, r *Registry, p *Project) *Entry {
	t.Helper()
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if e.WorktreePath == "" {
		t.Skip("worktree creation unavailable in this environment")
	}
	return e
}

func TestArchiveWritesRecordAndStopsSession(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	id, name, color := e.ID, e.Name, e.Color
	sess := e.sess

	if err := r.Archive(id); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if r.Get(id) != nil {
		t.Fatal("entry still live after Archive")
	}
	if sess != nil {
		select {
		case <-sess.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("process still running after Archive")
		}
	}
	rec, err := readRecord(ArchivedDir(r.stateDir), id)
	if err != nil {
		t.Fatalf("no archive record: %v", err)
	}
	if rec.Meta.Name != name || rec.Meta.Color != color || rec.ArchivedAt.IsZero() {
		t.Errorf("record = %+v, want name/color %q/%q and ArchivedAt set", rec, name, color)
	}
	// It is an archive, not a close: no tombstone, so Close's undo and
	// retention are untouched.
	if _, err := r.readTombstone(id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Archive left a tombstone (err=%v)", err)
	}
}

func TestArchiveRecordSurvivesReopen(t *testing.T) {
	skipNonPosix(t)
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := r.EnsureDefaultProject(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	e, err := r.Create(context.Background(), wire.CreateSpec{Shell: "/bin/bash"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Archive(e.ID); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()

	r2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer r2.Close()
	got := r2.ListArchived()
	if len(got) != 1 || got[0].SessionID != e.ID {
		t.Fatalf("ListArchived after reopen = %+v, want the archived session", got)
	}
}

func TestArchiveRecordWriteFailureLeavesSessionRunning(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	id := e.ID
	// A file where the directory should be makes MkdirAll fail on any
	// platform, root included.
	if err := os.WriteFile(ArchivedDir(r.stateDir), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := r.Archive(id); err == nil {
		t.Fatal("Archive succeeded although the record could not be written")
	}
	got := r.Get(id)
	if got == nil {
		t.Fatal("session vanished although the archive record was never written")
	}
	select {
	case <-got.sess.Done():
		t.Fatal("process was stopped although the archive record was never written")
	default:
	}
	if got.Phase != wire.PhaseReady {
		t.Errorf("phase = %q after a failed archive, want ready", got.Phase)
	}
	defer r.Kill(id, true)
}

func TestArchiveRefusesBusySession(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	defer r.Kill(e.ID, true)

	r.mu.Lock()
	e.Phase = wire.PhaseSpawning
	r.mu.Unlock()
	if err := r.Archive(e.ID); !errors.Is(err, ErrArchiveBusy) {
		t.Errorf("Archive of a spawning session = %v, want ErrArchiveBusy", err)
	}
	r.mu.Lock()
	e.Phase = wire.PhaseReady
	e.awaitingChoice = true
	r.mu.Unlock()
	if err := r.Archive(e.ID); !errors.Is(err, ErrArchiveBusy) {
		t.Errorf("Archive of a session parked on a choice = %v, want ErrArchiveBusy", err)
	}
	if r.Get(e.ID) == nil {
		t.Error("a refused archive removed the session")
	}
	if err := r.Archive("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Archive of an unknown id = %v, want ErrNotFound", err)
	}
}

func TestWriteDurableSyncsFileThenDirAfterRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	var calls []string
	origF, origD := syncFile, syncDir
	defer func() { syncFile, syncDir = origF, origD }()
	syncFile = func(f *os.File) error {
		if _, err := os.Stat(path); err == nil {
			t.Error("file synced after the rename; it must be synced before")
		}
		calls = append(calls, "file")
		return f.Sync()
	}
	syncDir = func(d string) error {
		if _, err := os.Stat(path); err != nil {
			t.Error("directory synced before the rename landed")
		}
		calls = append(calls, "dir")
		return nil
	}
	if err := writeDurable(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != "file" || calls[1] != "dir" {
		t.Errorf("sync order = %v, want [file dir]", calls)
	}
}

func TestArchiveNeverPruned(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	old := time.Now().UTC().Add(-90 * 24 * time.Hour)
	for i := 0; i < maxTombstones+5; i++ {
		id := "arch" + string(rune('a'+i))
		rec := Tombstone{Meta: MetaFile{ID: id, Name: id}, ArchivedAt: old}
		if err := writeJSON(recordPath(ArchivedDir(r.stateDir), id), rec); err != nil {
			t.Fatal(err)
		}
	}
	r.pruneTombstones()
	// A Close made after archiving runs the prune itself.
	e := createShell(t, r)
	if err := r.Kill(e.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := len(r.ListArchived()); got != maxTombstones+5 {
		t.Errorf("ListArchived = %d records, want %d: archived records must never be pruned", got, maxTombstones+5)
	}
}

func TestArchiveKeepsPristineWorktree(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e := archivedWorktreeSession(t, r, p)
	wt := e.WorktreePath

	if err := r.Archive(e.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	// A plain Kill removes a pristine worktree; archive must not.
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("a clean worktree was removed by Archive: %v", err)
	}
}

func TestArchiveKeepsDirtyWorktreeWithoutGuard(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e := archivedWorktreeSession(t, r, p)
	wip := filepath.Join(e.WorktreePath, "wip.txt")
	mustWriteFile(t, wip, "unfinished\n")

	if err := r.Archive(e.ID); err != nil {
		t.Fatalf("Archive of a dirty worktree = %v, want nil (no ErrWorktreeDirty)", err)
	}
	b, err := os.ReadFile(wip)
	if err != nil || string(b) != "unfinished\n" {
		t.Errorf("uncommitted file changed by Archive: %q, %v", b, err)
	}
}

func TestOrphanReclaimSkipsArchivedWorktree(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e := archivedWorktreeSession(t, r, p)
	wt := e.WorktreePath
	if err := r.Archive(e.ID); err != nil {
		t.Fatal(err)
	}
	if !r.worktreeClaimed(wt) {
		t.Fatal("an archived worktree reads as unclaimed; boot reclaim would delete it")
	}
	r.ReclaimOrphanWorktrees(context.Background(), r.ScanOrphanWorktrees())
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("orphan reclaim deleted an archived worktree: %v", err)
	}
}

func TestUnarchiveRebuildsEntry(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	id, name, color, created := e.ID, e.Name, e.Color, e.Created
	if err := r.Archive(id); err != nil {
		t.Fatal(err)
	}

	got, res, err := r.Unarchive(id, shellOpts())
	if err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	defer r.Kill(id, true)
	if got.ID != id || got.Name != name || got.Color != color || !got.Created.Equal(created) {
		t.Errorf("unarchived entry = %q/%q/%q created %v, want %q/%q/%q created %v",
			got.ID, got.Name, got.Color, got.Created, id, name, color, created)
	}
	if !res.Clean() {
		t.Errorf("unarchiving a plain shell should be clean, got %+v", res)
	}
	if _, err := readRecord(ArchivedDir(r.stateDir), id); !errors.Is(err, ErrNotFound) {
		t.Errorf("archive record survived Unarchive (err=%v)", err)
	}
	if len(r.ListArchived()) != 0 {
		t.Error("unarchived session still listed as archived")
	}
}

func TestUnarchiveTwiceSecondFails(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	if err := r.Archive(e.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Unarchive(e.ID, shellOpts()); err != nil {
		t.Fatal(err)
	}
	defer r.Kill(e.ID, true)
	if _, _, err := r.Unarchive(e.ID, shellOpts()); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Unarchive = %v, want ErrNotFound", err)
	}
}

func TestUnarchiveRejectsBadIds(t *testing.T) {
	r := freshRegistry(t)
	for _, id := range []string{"", "../x", "a/b", ".."} {
		if _, _, err := r.Unarchive(id, shellOpts()); !errors.Is(err, ErrNotFound) {
			t.Errorf("Unarchive(%q) = %v, want ErrNotFound", id, err)
		}
		if _, err := r.DeleteArchived(id, true, true); !errors.Is(err, ErrNotFound) {
			t.Errorf("DeleteArchived(%q) = %v, want ErrNotFound", id, err)
		}
	}
}

func TestUnarchiveWorktreeGoneRecreatesFromBranch(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e := archivedWorktreeSession(t, r, p)
	id, wt := e.ID, e.WorktreePath
	if err := r.Archive(id); err != nil {
		t.Fatal(err)
	}
	root, err := worktree.Root(p.Cwd)
	if err != nil {
		t.Fatal(err)
	}
	if err := worktree.Cleanup(root, wt); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	got, res, err := r.Unarchive(id, shellOpts())
	if err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	defer r.Kill(id, true)
	if !res.WorktreeRecreated || got.WorktreePath != wt {
		t.Errorf("res=%+v path=%q, want a recreated worktree at %q", res, got.WorktreePath, wt)
	}
}

func TestArchiveSnapshotsProjectNameAndUnarchiveSurvivesProjectDeletion(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	if _, err := r.EnsureDefaultProject(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	p, err := r.CreateProject(wire.CreateProjectReq{Name: "doomed", Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	e := createShellIn(t, r, p)
	if err := r.Archive(e.ID); err != nil {
		t.Fatal(err)
	}
	if got := r.ListArchived(); len(got) != 1 || got[0].ProjectName != "doomed" {
		t.Fatalf("ListArchived = %+v, want project name %q snapshotted", got, "doomed")
	}
	if err := r.KillProject(p.ID, true, true); err != nil {
		t.Fatalf("KillProject: %v", err)
	}
	if got := r.ListArchived(); len(got) != 1 || got[0].ProjectName != "doomed" {
		t.Errorf("project name lost after the project was deleted: %+v", got)
	}
	got, res, err := r.Unarchive(e.ID, shellOpts())
	if err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	defer r.Kill(e.ID, true)
	if !res.ProjectReassigned || got.ProjectID == p.ID {
		t.Errorf("res=%+v project=%q, want reassignment to the default project", res, got.ProjectID)
	}
}

func TestListArchivedNewestFirstAndSkipsLive(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	now := time.Now().UTC()
	for id, at := range map[string]time.Time{"old": now.Add(-time.Hour), "new": now} {
		rec := Tombstone{Meta: MetaFile{ID: id, Name: id}, ArchivedAt: at}
		if err := writeJSON(recordPath(ArchivedDir(r.stateDir), id), rec); err != nil {
			t.Fatal(err)
		}
	}
	got := r.ListArchived()
	if len(got) != 2 || got[0].SessionID != "new" || got[1].SessionID != "old" {
		t.Fatalf("ListArchived = %+v, want newest first", got)
	}
	// A record whose id is live (crash between persist and discard) is
	// not offered a second time.
	e := createShell(t, r)
	defer r.Kill(e.ID, true)
	rec := Tombstone{Meta: MetaFile{ID: e.ID, Name: "dup"}, ArchivedAt: now}
	if err := writeJSON(recordPath(ArchivedDir(r.stateDir), e.ID), rec); err != nil {
		t.Fatal(err)
	}
	for _, a := range r.ListArchived() {
		if a.SessionID == e.ID {
			t.Error("a live session is also listed as archived")
		}
	}
}

func TestDeleteArchivedDiscardsRecordKeepsWorktreeByDefault(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e := archivedWorktreeSession(t, r, p)
	wt := e.WorktreePath
	if err := r.Archive(e.ID); err != nil {
		t.Fatal(err)
	}
	kept, err := r.DeleteArchived(e.ID, false, false)
	if err != nil || !kept {
		t.Fatalf("DeleteArchived = kept %v, err %v; want kept, nil", kept, err)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree removed although removal was not requested: %v", err)
	}
	if len(r.ListArchived()) != 0 {
		t.Error("record still listed after delete")
	}
}

func TestDeleteArchivedRemovesManagedWorktreeWhenAsked(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e := archivedWorktreeSession(t, r, p)
	wt := e.WorktreePath
	if err := r.Archive(e.ID); err != nil {
		t.Fatal(err)
	}
	kept, err := r.DeleteArchived(e.ID, true, false)
	if err != nil || kept {
		t.Fatalf("DeleteArchived = kept %v, err %v; want removed, nil", kept, err)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Error("worktree still on disk after a requested removal")
	}
}

func TestDeleteArchivedRefusesDirtyWorktreeWithoutForce(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e := archivedWorktreeSession(t, r, p)
	wt := e.WorktreePath
	mustWriteFile(t, filepath.Join(wt, "wip.txt"), "unfinished\n")
	if err := r.Archive(e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.DeleteArchived(e.ID, true, false); !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("DeleteArchived of a dirty worktree = %v, want ErrWorktreeDirty", err)
	}
	if len(r.ListArchived()) != 1 {
		t.Error("a refused delete discarded the record")
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("a refused delete removed the worktree: %v", err)
	}
	if kept, err := r.DeleteArchived(e.ID, true, true); err != nil || kept {
		t.Fatalf("forced DeleteArchived = kept %v, err %v; want removed, nil", kept, err)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Error("forced delete left the worktree on disk")
	}
}

func TestDeleteArchivedKeepsWorktreeHeldByAnotherRecord(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e := archivedWorktreeSession(t, r, p)
	wt := e.WorktreePath
	if err := r.Archive(e.ID); err != nil {
		t.Fatal(err)
	}
	sib := Tombstone{Meta: MetaFile{ID: "sibling", Name: "sibling", WorktreePath: wt, ProjectID: p.ID}, ArchivedAt: time.Now().UTC()}
	if err := writeJSON(recordPath(ArchivedDir(r.stateDir), "sibling"), sib); err != nil {
		t.Fatal(err)
	}
	kept, err := r.DeleteArchived(e.ID, true, true)
	if err != nil || !kept {
		t.Fatalf("DeleteArchived = kept %v, err %v; want kept, nil", kept, err)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("a worktree another archived session holds was removed: %v", err)
	}
}

func TestRemoveWorktreeRefusedWhenArchivedRecordHoldsIt(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e := archivedWorktreeSession(t, r, p)
	wt := e.WorktreePath
	if err := r.Archive(e.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.RemoveWorktree(p.ID, wt, true, false, false); !errors.Is(err, ErrWorktreeInUse) {
		t.Errorf("RemoveWorktree = %v, want ErrWorktreeInUse", err)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("worktree removed from under an archived session: %v", err)
	}
}

func TestUnarchiveDeleteArchivedRace(t *testing.T) {
	skipNonPosix(t)
	for i := 0; i < 3; i++ {
		r, p := freshRegistryWithProject(t)
		e := archivedWorktreeSession(t, r, p)
		id, wt := e.ID, e.WorktreePath
		if err := r.Archive(id); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _, _, _ = r.Unarchive(id, shellOpts()) }()
		go func() { defer wg.Done(); _, _ = r.DeleteArchived(id, true, true) }()
		wg.Wait()
		if r.Get(id) != nil {
			if _, err := os.Stat(wt); err != nil {
				t.Fatalf("iteration %d: session is live but its worktree was deleted: %v", i, err)
			}
			_ = r.Kill(id, true)
		}
	}
}

func TestCheckArchivedConversation(t *testing.T) {
	orig := takeoverTranscriptExists
	defer func() { takeoverTranscriptExists = orig }()

	rec := Tombstone{Meta: MetaFile{ID: "x", Agent: string(agent.IDClaude), AgentSessionID: "sid"}}
	takeoverTranscriptExists = func(agent.Def, string, string) bool { return false }
	var res RestoreResult
	checkArchivedConversation(rec, "/cwd", &res)
	if def, ok := agent.Get(agent.IDClaude); ok && def.TranscriptPaths != nil && !res.ConversationLost {
		t.Error("a pruned transcript was not reported as a lost conversation")
	}

	takeoverTranscriptExists = func(agent.Def, string, string) bool { return true }
	res = RestoreResult{}
	checkArchivedConversation(rec, "/cwd", &res)
	if res.ConversationLost {
		t.Error("a present transcript was reported lost")
	}

	// No pinned id: left to Restore's own empty-id rule, not double-counted.
	takeoverTranscriptExists = func(agent.Def, string, string) bool { return false }
	res = RestoreResult{}
	checkArchivedConversation(Tombstone{Meta: MetaFile{Agent: string(agent.IDClaude)}}, "/cwd", &res)
	if res.ConversationLost {
		t.Error("an empty session id must be left to the shared restore rule")
	}
}

func TestKillStillWritesTombstoneNotArchive(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	if err := r.Kill(e.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.readTombstone(e.ID); err != nil {
		t.Errorf("Kill no longer writes a tombstone: %v", err)
	}
	if len(r.ListArchived()) != 0 {
		t.Error("Kill produced an archive record")
	}
}
