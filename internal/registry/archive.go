package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

// ErrArchiveBusy is returned by Archive for a session that is mid
// create, parked on a worktree question, or already being torn down.
// Archiving it would have to abandon that work half-done.
var ErrArchiveBusy = errors.New("registry: session is busy and cannot be archived yet")

// ArchivedDir holds one durable record per archived session. It is a
// separate directory from ClosedDir on purpose: pruneTombstones lists
// only closed/, so an archived record is never expired by age or count,
// and Close's retention stays exactly as it was.
func ArchivedDir(stateDir string) string {
	return filepath.Join(stateDir, "archived")
}

// Seams so a test can observe the durability order and inject failures.
var (
	syncFile = func(f *os.File) error { return f.Sync() }
	syncDir  = func(dir string) error {
		d, err := os.Open(dir)
		if err != nil {
			return err
		}
		defer d.Close()
		return d.Sync()
	}
)

// writeDurable writes data to path so that, once it returns nil, the
// file survives a crash: temp file, fsync, rename, fsync of the
// directory. writeAtomic does not fsync, which is fine for an undo
// record but not for the only copy of a session the user put away.
func writeDurable(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := syncFile(f); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return syncDir(dir)
}

// writeArchiveLocked records the entry as archived. Called under r.mu
// before anything is torn down. Unlike writeTombstoneLocked the error
// is returned: a failed archive must leave the session running, because
// the record is the only copy of it.
func (r *Registry) writeArchiveLocked(e *Entry) error {
	t := Tombstone{Meta: recordMeta(e), ArchivedAt: time.Now().UTC()}
	if p, ok := r.projects[e.ProjectID]; ok {
		t.ProjectName = p.Name
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return writeDurable(recordPath(ArchivedDir(r.stateDir), e.ID), b)
}

// Archive stops a session, keeps its worktree where it is, and leaves a
// durable record Unarchive can rebuild it from.
//
// It is not a Close: no dirty-worktree check (the worktree is not
// touched), no tombstone, no retention. A session that is mid-create,
// parked or already closing is refused before any state changes.
func (r *Registry) Archive(id string) error {
	r.parkedMu.Lock()
	_, parked := r.parked[id]
	r.parkedMu.Unlock()

	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	busy := parked || e.pendingChoice != nil || e.awaitingChoice
	switch e.Phase {
	case wire.PhaseStarting, wire.PhaseFetching, wire.PhaseWorktree, wire.PhaseSpawning,
		wire.PhaseChecking, wire.PhaseClosing, wire.PhaseRestarting, wire.PhaseReviving:
		busy = true
	}
	r.mu.Unlock()
	if busy {
		return ErrArchiveBusy
	}
	return r.kill(id, true, false, true)
}

// ListArchived returns every archived session, newest first. A record
// whose id is live is skipped: a crash between Unarchive's index
// persist and its record discard leaves such a pair, and the session
// must not be offered twice.
func (r *Registry) ListArchived() []wire.ArchivedSessionInfo {
	recs := listRecords(ArchivedDir(r.stateDir))
	out := make([]wire.ArchivedSessionInfo, 0, len(recs))
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range recs {
		if _, live := r.entries[t.Meta.ID]; live {
			continue
		}
		out = append(out, wire.ArchivedSessionInfo{
			SessionID:      t.Meta.ID,
			Name:           t.Meta.Name,
			Color:          t.Meta.Color,
			Agent:          t.Meta.Agent,
			ProjectID:      t.Meta.ProjectID,
			ProjectName:    t.ProjectName,
			WorktreeBranch: t.Meta.WorktreeBranch,
			ArchivedAt:     t.stamp().UTC().Format(time.RFC3339),
		})
	}
	return out
}

// Unarchive brings an archived session back the way Restore brings back
// a closed one, and additionally reports a conversation the agent CLI
// has pruned from disk while the session sat archived.
//
// Lock order: archiveMu > gitMu > r.mu. archiveMu is released before
// Revive spawns the agent.
func (r *Registry) Unarchive(id string, opts session.Options) (*Entry, RestoreResult, error) {
	r.archiveMu.Lock()
	_, res, err := r.restoreEntry(ArchivedDir(r.stateDir), id, checkArchivedConversation)
	r.archiveMu.Unlock()
	if err != nil {
		return nil, res, err
	}
	return r.reviveRestored(id, opts, res)
}

// checkArchivedConversation flags a pinned conversation whose
// transcript is gone. Only for agents that define TranscriptPaths; a
// missing transcript is safe to spawn on (ResumeArgs falls back to a
// fresh session) but the user should be told.
func checkArchivedConversation(t Tombstone, cwd string, res *RestoreResult) {
	if res.ConversationLost || t.Meta.AgentSessionID == "" {
		return
	}
	def, ok := agent.Get(agent.ID(t.Meta.Agent))
	if !ok || def.TranscriptPaths == nil {
		return
	}
	if !takeoverTranscriptExists(def, t.Meta.AgentSessionID, cwd) {
		res.ConversationLost = true
	}
}

// DeleteArchived discards an archived record for good. With
// removeWorktree it also deletes the session's worktree, but only a
// hive-managed one that no live session, tombstone or other archived
// record holds; otherwise the worktree is kept and worktreeKept says so.
// A dirty worktree is refused (record untouched) unless force.
func (r *Registry) DeleteArchived(id string, removeWorktree, force bool) (worktreeKept bool, err error) {
	r.archiveMu.Lock()
	defer r.archiveMu.Unlock()

	dir := ArchivedDir(r.stateDir)
	t, err := readRecord(dir, id)
	if err != nil {
		return false, err
	}
	wt := t.Meta.WorktreePath
	worktreeKept = wt != ""
	if removeWorktree && wt != "" {
		removed, err := r.removeArchivedWorktree(t, force)
		if err != nil {
			return false, err
		}
		worktreeKept = !removed
	}
	discardRecord(dir, id)
	return worktreeKept, nil
}

// removeArchivedWorktree deletes the worktree of an archived record.
// Returns removed=true when the directory is gone afterwards.
func (r *Registry) removeArchivedWorktree(t Tombstone, force bool) (removed bool, err error) {
	wt := t.Meta.WorktreePath
	if _, statErr := os.Stat(wt); statErr != nil {
		return true, nil // already gone
	}
	if len(r.liveSessionsIn(wt)) > 0 || r.archivedClaimsWorktree(wt, t.Meta.ID) {
		return false, nil
	}
	for _, o := range r.listTombstones() {
		if o.Meta.WorktreePath == wt {
			return false, nil
		}
	}
	r.mu.Lock()
	cwd := ""
	if p, ok := r.projects[t.Meta.ProjectID]; ok {
		cwd = p.Cwd
	}
	r.mu.Unlock()

	r.gitMu.Lock()
	defer r.gitMu.Unlock()
	root, rerr := worktree.Root(cwd)
	if rerr != nil || !worktree.IsManaged(root, wt) {
		log.Printf("registry: delete archived %s: cannot establish a managed worktree for %s; keeping it", t.Meta.ID, wt)
		return false, nil
	}
	if !force {
		if dirty, _ := worktree.HasUncommitted(wt); dirty {
			return false, ErrWorktreeDirty
		}
	}
	if cerr := worktree.Cleanup(root, wt); cerr != nil {
		return false, fmt.Errorf("remove worktree: %w", cerr)
	}
	r.pruneWorktreeLabel(t.Meta.ProjectID, wt)
	return true, nil
}

// archivedClaimsWorktree reports whether an archived record other than
// exceptID refers to this worktree path (compared raw and symlink
// resolved). Boot-time orphan reclaim and RemoveWorktree both consult
// it: an archived session is a claim held indefinitely.
func (r *Registry) archivedClaimsWorktree(path, exceptID string) bool {
	_, ok := r.archivedHolder(path, exceptID)
	return ok
}

// archivedHolder returns the name of the archived session holding path.
func (r *Registry) archivedHolder(path, exceptID string) (string, bool) {
	if path == "" {
		return "", false
	}
	resolved := worktree.ResolvePath(path)
	for _, t := range listRecords(ArchivedDir(r.stateDir)) {
		if t.Meta.ID == exceptID || t.Meta.WorktreePath == "" {
			continue
		}
		if t.Meta.WorktreePath == path || worktree.ResolvePath(t.Meta.WorktreePath) == resolved {
			return t.Meta.Name, true
		}
	}
	return "", false
}
