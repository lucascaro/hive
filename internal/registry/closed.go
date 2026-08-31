package registry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

// ErrExists is returned by Restore when the id it was asked to bring
// back is already live. Defensive: session ids are unique, so this
// only fires if something upstream reused one.
var ErrExists = errors.New("registry: session already exists")

// Tombstone retention. Both bounds apply and whichever prunes first
// wins: the count keeps the state dir from growing without limit on a
// heavy day, the age keeps a week-old close from surfacing as "the
// last thing you closed" long after the context is gone.
const (
	maxTombstones   = 20
	maxTombstoneAge = 7 * 24 * time.Hour
)

// maxPatchBytes caps the recovery patch written before a worktree is
// deleted. Above this the patch is skipped and the fact recorded, so
// the UI can say "too large to save" instead of implying a recovery
// that does not exist.
const maxPatchBytes = 10 << 20

// Tombstone is what a close leaves behind: enough to rebuild the
// registry entry, plus what the close itself did to the worktree.
//
// Metadata only — well under 1 KB. Scrollback is deliberately not
// captured here; it lives in the session's in-memory ring and there is
// no disk-backed scrollback to copy from.
type Tombstone struct {
	Meta     MetaFile  `json:"meta"`
	ClosedAt time.Time `json:"closed_at"`
	// WorktreeRemoveRequested records that the close asked for the
	// worktree to be deleted, not that the deletion succeeded —
	// disposeWorktree can still refuse (an unmanaged path). Restore
	// probes the filesystem rather than trusting this, so it is a
	// diagnostic, not a decision input.
	WorktreeRemoveRequested bool `json:"worktree_remove_requested,omitempty"`
	// WorktreeShared records that a sibling session still lived in the
	// worktree at close time, so this close never touched it.
	WorktreeShared bool `json:"worktree_shared,omitempty"`
	// PatchPath is the recovery patch written before a requested
	// worktree deletion; "" when none was written (nothing uncommitted,
	// no deletion requested, or the dump failed).
	PatchPath string `json:"patch_path,omitempty"`
	// PatchSkipped is true when there WAS uncommitted work but the
	// patch exceeded maxPatchBytes. The distinction matters: "" plus
	// false means nothing was at stake, "" plus true means something
	// was and we could not save it.
	PatchSkipped bool `json:"patch_skipped,omitempty"`
}

// RestoreResult reports everything a restore could NOT bring back
// cleanly. Every field is a degradation, so the zero value is a clean
// undo and the GUI can render "restored" versus "restored, but…"
// without a second round trip.
type RestoreResult struct {
	ProjectReassigned bool
	WorktreeRecreated bool
	WorktreeLost      bool
	ConversationLost  bool
	AgentFellBack     bool
	PatchPath         string
	PatchSkipped      bool
}

// Clean reports whether the restore lost nothing worth telling the
// user about.
func (rr RestoreResult) Clean() bool {
	return !rr.ProjectReassigned && !rr.WorktreeRecreated && !rr.WorktreeLost &&
		!rr.ConversationLost && !rr.AgentFellBack && !rr.PatchSkipped
}

// ClosedDir is the directory holding one tombstone (and any recovery
// patch) per recently closed session.
func ClosedDir(stateDir string) string {
	return filepath.Join(stateDir, "closed")
}

func (r *Registry) tombstonePath(id string) string {
	return filepath.Join(ClosedDir(r.stateDir), id+".json")
}

func (r *Registry) patchPath(id string) string {
	return filepath.Join(ClosedDir(r.stateDir), id+".patch")
}

// writeTombstoneLocked records the entry as closed. Called under r.mu
// immediately before kill removes it from the map, so the record is on
// disk before anything — in memory or on disk — is destroyed.
//
// Failure is logged, never returned: a close the user asked for must
// not be blocked because undo bookkeeping failed.
func (r *Registry) writeTombstoneLocked(e *Entry, t Tombstone) {
	t.Meta = MetaFile{
		ID: e.ID, Name: e.Name, Color: e.Color,
		Order: e.Order, Created: e.Created, Agent: e.Agent,
		ProjectID:      e.ProjectID,
		WorktreePath:   e.WorktreePath,
		WorktreeBranch: e.WorktreeBranch,
		AgentSessionID: e.AgentSessionID,
	}
	t.ClosedAt = time.Now().UTC()
	if err := writeJSON(r.tombstonePath(e.ID), t); err != nil {
		log.Printf("registry: kill %s: could not record tombstone (undo unavailable for this close): %v", e.ID, err)
	}
}

// dumpRecoveryPatch captures the uncommitted state of a worktree that
// this close has been told to delete. Runs before the tombstone write
// and outside r.mu — it shells out to git several times.
//
// Best-effort by design: every failure path returns a Tombstone with
// no patch rather than an error, because the user asked to close the
// session and a failed backup must not veto that.
func (r *Registry) dumpRecoveryPatch(id, wtPath string) (patchPath string, skipped bool) {
	r.gitMu.Lock()
	defer r.gitMu.Unlock()
	out := r.patchPath(id)
	err := worktree.DumpPatch(wtPath, out, maxPatchBytes)
	switch {
	case errors.Is(err, worktree.ErrPatchTooLarge):
		log.Printf("registry: kill %s: uncommitted work in %s exceeds the %d-byte recovery-patch cap; not saved",
			id, wtPath, maxPatchBytes)
		return "", true
	case err != nil:
		log.Printf("registry: kill %s: could not save a recovery patch for %s: %v", id, wtPath, err)
		return "", false
	}
	// DumpPatch writes nothing when there is nothing uncommitted.
	if _, statErr := os.Stat(out); statErr != nil {
		return "", false
	}
	return out, false
}

// readTombstone loads one record. A malformed file is treated as
// missing — an unreadable tombstone is not something the user can act
// on, and failing the restore with a JSON error helps nobody.
func (r *Registry) readTombstone(id string) (Tombstone, error) {
	var t Tombstone
	// The id arrives off the wire and is about to be joined into a
	// path that discardTombstone will later os.Remove. Kill() is
	// protected incidentally — it looks its id up in r.entries first,
	// so only known ids ever reach a path — but Restore builds the
	// path from the raw string, so the guard has to be explicit here.
	// The socket is local and user-owned, which bounds the impact; it
	// does not make an unvalidated path join at a trust boundary OK.
	if !validSessionID(id) {
		return t, ErrNotFound
	}
	if err := readJSON(r.tombstonePath(id), &t); err != nil {
		return t, ErrNotFound
	}
	if t.Meta.ID == "" {
		return t, ErrNotFound
	}
	// The file's own id must match the one we were asked for. A
	// mismatch means the store is inconsistent, and restoring under
	// the wrong id would then delete the wrong tombstone.
	if t.Meta.ID != id {
		return t, ErrNotFound
	}
	return t, nil
}

// validSessionID reports whether id is safe to use as a path segment.
// Session ids are generated UUIDs, so this is deliberately stricter
// than "no separators": anything outside the UUID alphabet is rejected
// rather than escaped, because there is no legitimate caller that
// needs it.
func validSessionID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// listTombstones returns every readable tombstone, newest close first.
func (r *Registry) listTombstones() []Tombstone {
	ents, err := os.ReadDir(ClosedDir(r.stateDir))
	if err != nil {
		return nil
	}
	var out []Tombstone
	for _, de := range ents {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		var t Tombstone
		if err := readJSON(filepath.Join(ClosedDir(r.stateDir), de.Name()), &t); err != nil {
			continue
		}
		if t.Meta.ID == "" {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClosedAt.After(out[j].ClosedAt) })
	return out
}

// ListClosed returns the recently closed sessions, newest first, for
// the "reopen last closed" affordance.
func (r *Registry) ListClosed() []wire.ClosedSessionInfo {
	ts := r.listTombstones()
	out := make([]wire.ClosedSessionInfo, 0, len(ts))
	for _, t := range ts {
		out = append(out, wire.ClosedSessionInfo{
			SessionID:      t.Meta.ID,
			Name:           t.Meta.Name,
			Color:          t.Meta.Color,
			Agent:          t.Meta.Agent,
			ProjectID:      t.Meta.ProjectID,
			WorktreeBranch: t.Meta.WorktreeBranch,
			ClosedAt:       t.ClosedAt.UTC().Format(time.RFC3339),
			HasPatch:       t.PatchPath != "",
		})
	}
	return out
}

// pruneTombstones enforces both retention bounds. Called after each
// write, outside every lock.
func (r *Registry) pruneTombstones() {
	ts := r.listTombstones() // newest first
	cutoff := time.Now().UTC().Add(-maxTombstoneAge)
	for i, t := range ts {
		if i < maxTombstones && t.ClosedAt.After(cutoff) {
			continue
		}
		r.discardTombstone(t.Meta.ID)
	}
}

// discardTombstone removes a record and any recovery patch beside it.
// Callers reach this only after readTombstone or listTombstones has
// vouched for the id, but it re-checks: this is the function that
// removes files, and it is one line to make that independent of who
// calls it.
func (r *Registry) discardTombstone(id string) {
	if !validSessionID(id) {
		return
	}
	if err := os.Remove(r.tombstonePath(id)); err != nil && !os.IsNotExist(err) {
		log.Printf("registry: could not remove tombstone %s: %v", id, err)
	}
	if err := os.Remove(r.patchPath(id)); err != nil && !os.IsNotExist(err) {
		log.Printf("registry: could not remove recovery patch %s: %v", id, err)
	}
}

// tombstoneClaimsWorktree reports whether a live tombstone still
// refers to this worktree path.
//
// Boot-time orphan reclaim deletes pristine worktrees that no session
// claims — and a tombstoned worktree is unclaimed by definition. The
// window is narrow (a pristine worktree is normally removed by the
// same kill that wrote the tombstone) but it is a silent delete of
// something the user can still undo into, at boot, with no confirm.
// Cheap to close, so close it.
func (r *Registry) tombstoneClaimsWorktree(path string) bool {
	if path == "" {
		return false
	}
	for _, t := range r.listTombstones() {
		if t.Meta.WorktreePath == path {
			return true
		}
	}
	return false
}

// Restore brings back a session closed earlier in this state dir,
// rebuilding its entry from the tombstone and reviving the agent.
//
// The returned RestoreResult names everything that could not be
// restored cleanly. It is not an error channel: a restore that comes
// back without its worktree still succeeded — the tile, the name, the
// project and (usually) the conversation are back, and the caller's
// job is to say what was lost, not to treat it as a failure.
//
// Never auto-applies the recovery patch. Applying a patch into a
// checkout whose HEAD may have moved since the close turns one bad
// close into a merge conflict; the path is surfaced and `git apply`
// stays the user's decision.
func (r *Registry) Restore(id string, opts session.Options) (*Entry, RestoreResult, error) {
	var res RestoreResult

	t, err := r.readTombstone(id)
	if err != nil {
		return nil, res, err
	}
	res.PatchPath = t.PatchPath
	res.PatchSkipped = t.PatchSkipped

	r.mu.Lock()
	if _, live := r.entries[id]; live {
		r.mu.Unlock()
		return nil, res, ErrExists
	}
	// Resolve the project now: it may have been killed since the close.
	projectID := t.Meta.ProjectID
	if _, ok := r.projects[projectID]; !ok {
		projectID = r.defaultProjectIDLocked()
		res.ProjectReassigned = projectID != t.Meta.ProjectID
	}
	projectCwd := ""
	if p, ok := r.projects[projectID]; ok {
		projectCwd = p.Cwd
	}
	r.mu.Unlock()

	// Worktree resolution runs outside r.mu (it shells out to git) and
	// under gitMu, the same discipline create and kill follow.
	wtPath, wtBranch := r.resolveRestoreWorktree(id, projectCwd, t, &res)

	if t.Meta.Agent != "" {
		if _, ok := agent.Get(agent.ID(t.Meta.Agent)); !ok {
			// A custom agent deleted since the close. Revive already
			// falls back to a generic shell; flag it so the UI can say
			// the session came back as a shell rather than as the tool
			// the user remembers.
			res.AgentFellBack = true
		}
		res.ConversationLost = t.Meta.AgentSessionID == ""
	}

	e := &Entry{
		ID: t.Meta.ID, Name: t.Meta.Name, Color: t.Meta.Color,
		Created:        t.Meta.Created,
		Agent:          t.Meta.Agent,
		ProjectID:      projectID,
		WorktreePath:   wtPath,
		WorktreeBranch: wtBranch,
		AgentSessionID: t.Meta.AgentSessionID,
	}

	r.mu.Lock()
	// Re-check liveness: the worktree work above ran unlocked and can
	// take seconds, easily long enough for a second undo to land.
	if _, live := r.entries[id]; live {
		r.mu.Unlock()
		return nil, res, ErrExists
	}
	r.entries[id] = e
	// Append rather than splice back into the original slot. Every
	// sibling's Order shifted down when this entry was removed, so the
	// remembered index no longer means what it meant; appending is
	// both simpler and never lands the tile somewhere surprising.
	r.order = append(r.order, id)
	r.reindexLocked()
	if perr := r.persistEntryLocked(e); perr != nil {
		delete(r.entries, id)
		r.order = slices.Delete(r.order, len(r.order)-1, len(r.order))
		r.reindexLocked()
		r.mu.Unlock()
		return nil, res, fmt.Errorf("restore %s: persist entry: %w", id, perr)
	}
	r.persistIndexLoggedLocked("restore")
	info := e.Info()
	r.mu.Unlock()

	// The entry exists and is visible before the PTY forks, so a client
	// sees the tile come back immediately rather than after the spawn.
	r.broadcast(wire.SessionEventAdded, info)

	// The tombstone has done its job. Drop it before the revive so a
	// spawn failure can't leave a record that would restore a second
	// copy of a session that is already in the map.
	r.discardTombstone(id)

	if rerr := r.Revive(id, opts); rerr != nil {
		// The entry stays: a session whose agent failed to spawn is an
		// ordinary dead tile the user can restart, which is strictly
		// better than silently undoing the undo.
		log.Printf("registry: restore %s: revive failed: %v", id, rerr)
	}

	r.mu.Lock()
	e = r.entries[id]
	r.mu.Unlock()
	return e, res, nil
}

// resolveRestoreWorktree decides what worktree, if any, the restored
// entry gets. Returns the path and branch to bind, both "" when the
// session comes back without one.
//
// Mirrors disposeWorktree's paranoia in reverse: it will re-adopt a
// directory that is still there, and it will recreate one from its
// surviving branch, but it never creates a worktree at a path outside
// hive's managed directory.
func (r *Registry) resolveRestoreWorktree(id, projectCwd string, t Tombstone, res *RestoreResult) (string, string) {
	wtPath, wtBranch := t.Meta.WorktreePath, t.Meta.WorktreeBranch
	if wtPath == "" {
		return "", ""
	}

	// Still on disk — whether because the close kept it (dirty, or
	// shared with a sibling) or because the delete was refused. Adopt
	// it as-is. A sibling already living there is fine and expected:
	// duplicated sessions legitimately share one worktree.
	if _, err := os.Stat(wtPath); err == nil {
		return wtPath, wtBranch
	}

	r.gitMu.Lock()
	defer r.gitMu.Unlock()

	root, err := worktree.Root(projectCwd)
	if err != nil {
		log.Printf("registry: restore %s: project cwd %q is not a git repo; restoring without a worktree", id, projectCwd)
		res.WorktreeLost = true
		return "", ""
	}
	// Never materialize a directory outside the managed namespace. The
	// path came off disk and this is the last guard before a mkdir.
	if !worktree.IsManaged(root, wtPath) {
		log.Printf("registry: restore %s: %s is not a hive-managed worktree of %s; restoring without a worktree", id, wtPath, root)
		res.WorktreeLost = true
		return "", ""
	}
	if wtBranch == "" || !worktree.BranchExists(root, wtBranch) {
		log.Printf("registry: restore %s: branch %q is gone; restoring without a worktree", id, wtBranch)
		res.WorktreeLost = true
		return "", ""
	}
	if err := worktree.CreateWorktree(context.Background(), root, wtBranch, wtPath); err != nil {
		log.Printf("registry: restore %s: could not recreate worktree %s on %s: %v", id, wtPath, wtBranch, err)
		res.WorktreeLost = true
		return "", ""
	}
	worktree.LinkAgentConfig(root, wtPath)
	// Committed work is back; anything uncommitted at close time is
	// not, and only the recovery patch (if one fit) can return it.
	res.WorktreeRecreated = true
	return wtPath, wtBranch
}
