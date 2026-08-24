package registry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

// ErrProjectNotFound is returned when a project ID isn't known.
var ErrProjectNotFound = errors.New("registry: project not found")

// ListProjects returns a snapshot of all projects in display order.
func (r *Registry) ListProjects() []wire.ProjectInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]wire.ProjectInfo, 0, len(r.projectOrder))
	for _, id := range r.projectOrder {
		if p := r.projects[id]; p != nil {
			out = append(out, p.Info())
		}
	}
	return out
}

// EnsureDefaultProject creates a project named "default" rooted at
// the given cwd if no projects exist on disk. Idempotent. Used by the
// daemon at startup so a fresh install always has a project to host
// new sessions.
func (r *Registry) EnsureDefaultProject(cwd string) (*Project, error) {
	r.mu.Lock()
	if len(r.projectOrder) > 0 {
		first := r.projects[r.projectOrder[0]]
		r.mu.Unlock()
		return first, nil
	}
	r.mu.Unlock()
	return r.CreateProject(wire.CreateProjectReq{Name: "default", Cwd: cwd})
}

// OrphanWorktreeCandidate names one directory the reclaim may
// consider. Candidates are collected once, at daemon start, and the
// expensive part (git status per candidate, removal) runs later —
// see ReclaimOrphanWorktrees.
type OrphanWorktreeCandidate struct {
	Root string // repository root the worktree belongs to
	Path string // absolute path of the worktree directory
}

// ScanOrphanWorktrees lists the worktree directories that exist right
// now, one ReadDir per project plus a `git rev-parse` to find each
// repo root. Cheap enough to run on the daemon's boot path, which is
// the point: the candidate set must be "what was on disk when this
// daemon started".
//
// Anything created afterwards is somebody's live work — a worktree the
// user made by hand, or one hive's own create path is still filling in
// — and a background sweep that could delete it would be a data-loss
// bug, not a leak cleanup.
func (r *Registry) ScanOrphanWorktrees() []OrphanWorktreeCandidate {
	r.mu.Lock()
	projects := make([]*Project, 0, len(r.projects))
	for _, p := range r.projects {
		projects = append(projects, p)
	}
	r.mu.Unlock()

	var out []OrphanWorktreeCandidate
	seen := make(map[string]bool)
	for _, p := range projects {
		if p.Cwd == "" {
			continue
		}
		root, err := r.gitRoot(p.Cwd)
		if err != nil {
			continue
		}
		wtDir := filepath.Join(root, ".worktrees")
		entries, err := os.ReadDir(wtDir)
		if err != nil {
			continue
		}
		for _, d := range entries {
			if !d.IsDir() {
				continue
			}
			path := filepath.Join(wtDir, d.Name())
			if seen[path] {
				continue // two projects under one repo root
			}
			seen[path] = true
			out = append(out, OrphanWorktreeCandidate{Root: root, Path: path})
		}
	}
	return out
}

// ReclaimOrphanWorktrees runs worktree.Cleanup on any candidate that
// is both unclaimed by a live registry entry AND pristine (no
// uncommitted changes, no unpushed commits). Idempotent. Run once per
// daemon run, off the accept path, so a SIGKILL'd previous process
// doesn't leak without holding up boot — the git status calls here
// are several subprocesses per candidate.
//
// Candidates come from ScanOrphanWorktrees, called on the boot path
// before any client can connect. That split is what makes a
// background sweep safe: this function can only ever delete a
// directory that predates the daemon.
//
// It still runs concurrently with client-driven create/kill, so it
// takes r.gitMu around each git step rather than for the whole scan —
// a whole-scan hold would move the boot stall onto the first worktree
// create or kill after boot, which waits on that same mutex before it
// can even report a phase. Deleting is guarded instead by re-checking
// the claim and re-inspecting the worktree inside that step's lock: a
// worktree adopted, or dirtied, since the scan must not be deleted.
//
// ctx cuts the sweep short when the daemon is shutting down; the git
// steps already in flight are bounded by their own timeouts.
//
// The pristine condition is what makes detached worktrees possible:
// a worktree whose session was closed is unclaimed forever, so
// reclaiming unconditionally would delete the user's kept work at the
// next boot.
//
// Caller contract: only the canonical daemon may call this. The
// on-disk <project>/.worktrees/ namespace is shared across daemon
// instances (it lives under the project cwd, not under the state
// dir), so an isolated dev daemon — HIVE_STATE_DIR set — must not
// reap: every prod-owned worktree would look unclaimed in its
// registry and get force-removed. Daemon startup gates this call on
// registry.StateDirOverridden().
func (r *Registry) ReclaimOrphanWorktrees(ctx context.Context, candidates []OrphanWorktreeCandidate) {
	for _, c := range candidates {
		if ctx.Err() != nil {
			return
		}
		if r.worktreeClaimed(c.Path) {
			continue
		}
		// Same rule as Kill: reclaim only what holds no work.
		// A worktree the user detached on purpose (closed its
		// session, kept the branch) is unclaimed by definition,
		// so an unconditional reclaim here would delete it on
		// the next daemon start — silently, at boot, with no
		// confirm. Pristine-only keeps the crash-leak cleanup
		// this exists for while making detaching safe.
		st, ierr := r.inspectWorktree(c.Root, c.Path)
		switch {
		case ierr != nil:
			log.Printf("registry: cannot inspect orphan worktree %s (%v); keeping it", c.Path, ierr)
		case !st.Pristine():
			log.Printf("registry: keeping orphan worktree %s (uncommitted=%v unpushed=%d unknown=%v)",
				c.Path, st.Uncommitted, st.Unpushed, st.Unknown)
		default:
			r.reclaimOne(c.Root, c.Path)
		}
	}
}

// gitRoot resolves a project cwd to its repository root under gitMu,
// so the reclaim's git subprocesses stay serialized against the
// create/kill worktree steps without holding the mutex for the whole
// scan. A non-git cwd surfaces as an error, which the scan skips.
func (r *Registry) gitRoot(cwd string) (string, error) {
	r.gitMu.Lock()
	defer r.gitMu.Unlock()
	return worktree.Root(cwd)
}

// inspectWorktree is worktree.Inspect under gitMu — several git
// subprocesses per call, and the create path is entitled to interleave
// between candidates.
func (r *Registry) inspectWorktree(root, path string) (worktree.Status, error) {
	r.gitMu.Lock()
	defer r.gitMu.Unlock()
	return worktree.Inspect(root, path)
}

// reclaimOne removes a single orphan worktree, re-checking under the
// same lock that no session claimed it while Inspect was shelling out
// to git. The claim check and the removal must share gitMu: adoption
// (create.go's adoptDetachedWorktree) takes gitMu too, so a create
// cannot slip a claim in between them.
func (r *Registry) reclaimOne(root, path string) {
	r.gitMu.Lock()
	defer r.gitMu.Unlock()
	if r.worktreeClaimed(path) {
		return
	}
	// Re-inspect too: the first Inspect ran without the lock, and the
	// user may have started work in the directory since.
	if st, err := worktree.Inspect(root, path); err != nil || !st.Pristine() {
		log.Printf("registry: orphan worktree %s is no longer pristine (err=%v); keeping it", path, err)
		return
	}
	log.Printf("registry: reclaiming orphan worktree %s", path)
	if err := worktree.Cleanup(root, path); err != nil {
		log.Printf("registry: orphan cleanup failed for %s: %v", path, err)
	}
}

// worktreeClaimed reports whether any registry entry currently owns
// the worktree directory at path.
func (r *Registry) worktreeClaimed(path string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.WorktreePath == path {
			return true
		}
	}
	return false
}

// MigrateOrphanSessions assigns any session without a ProjectID to
// the default (first) project. Run on startup so users coming from
// pre-Phase-4 state don't see their sessions vanish.
func (r *Registry) MigrateOrphanSessions() {
	r.mu.Lock()
	defer r.mu.Unlock()
	defID := r.defaultProjectIDLocked()
	if defID == "" {
		return
	}
	for _, e := range r.entries {
		if e.ProjectID == "" {
			e.ProjectID = defID
			r.persistEntryLoggedLocked(e, "migrate orphan sessions")
		}
	}
}

// defaultProjectIDLocked must be called with r.mu held. Returns the
// first project's ID, or "" if none exist (caller should ensure a
// default exists first).
func (r *Registry) defaultProjectIDLocked() string {
	if len(r.projectOrder) == 0 {
		return ""
	}
	return r.projectOrder[0]
}

// CreateProject adds a new project and persists it.
func (r *Registry) CreateProject(req wire.CreateProjectReq) (*Project, error) {
	r.mu.Lock()
	id := uuid.NewString()
	name := req.Name
	if name == "" {
		name = fmt.Sprintf("project %d", len(r.projectOrder)+1)
	}
	color := req.Color
	if color == "" {
		color = pickColor(r.lastProjectColor)
		r.lastProjectColor = color
	}
	p := &Project{
		ID: id, Name: name, Color: color, Cwd: req.Cwd,
		Created: time.Now().UTC(),
	}
	r.projects[id] = p
	r.projectOrder = append(r.projectOrder, id)
	// Order comes from reindex, never from a literal here — one place
	// assigns it, so the append can't disagree with the slice.
	r.reindexProjectsLocked()
	if err := r.persistProjectLocked(p); err != nil {
		delete(r.projects, id)
		r.projectOrder = r.projectOrder[:len(r.projectOrder)-1]
		r.mu.Unlock()
		return nil, err
	}
	if err := r.persistProjectIndexLocked(); err != nil {
		delete(r.projects, id)
		r.projectOrder = r.projectOrder[:len(r.projectOrder)-1]
		r.mu.Unlock()
		return nil, err
	}
	info := p.Info()
	r.mu.Unlock()
	r.broadcastProject(wire.ProjectEventAdded, info)
	return p, nil
}

// KillProject removes a project. If killSessions is true the project's
// sessions are terminated; otherwise they're reassigned to the
// default project (which is never the project being killed unless
// it's the only one — in that case we refuse).
func (r *Registry) KillProject(id string, killSessions bool) error {
	r.mu.Lock()
	p, ok := r.projects[id]
	if !ok {
		r.mu.Unlock()
		return ErrProjectNotFound
	}
	if len(r.projectOrder) == 1 {
		r.mu.Unlock()
		return errors.New("registry: refusing to remove the only project")
	}

	// Pick the target for reassignment: the first project that isn't
	// the one being killed.
	var targetID string
	for _, oid := range r.projectOrder {
		if oid != id {
			targetID = oid
			break
		}
	}

	// Collect sessions in this project.
	affected := make([]*Entry, 0)
	for _, sid := range r.order {
		if e := r.entries[sid]; e != nil && e.ProjectID == id {
			affected = append(affected, e)
		}
	}

	// Reassign or kill each affected session. The reassigned ones are
	// snapshotted here, under the lock: `affected` holds live entries
	// that stay in r.entries, so calling Info() on them after the
	// unlock would race every concurrent Update/setPhase.
	var reassigned []wire.SessionInfo
	if !killSessions {
		for _, e := range affected {
			e.ProjectID = targetID
			r.persistEntryLoggedLocked(e, "delete project (reassign)")
			reassigned = append(reassigned, e.Info())
		}
	}

	delete(r.projects, id)
	for i, pid := range r.projectOrder {
		if pid == id {
			r.projectOrder = append(r.projectOrder[:i], r.projectOrder[i+1:]...)
			break
		}
	}
	// Every project after the removed one shifted down a slot, so
	// Order follows and the survivors are broadcast at the tail —
	// same reasoning as Kill(). See reindexLocked in registry.go.
	r.reindexProjectsLocked()
	r.persistProjectIndexLoggedLocked("delete project")

	dir := filepath.Join(ProjectsDir(r.stateDir), id)
	info := p.Info()
	r.mu.Unlock()

	_ = os.RemoveAll(dir)

	if killSessions {
		for _, e := range affected {
			_ = r.Kill(e.ID, true)
		}
	} else {
		for _, si := range reassigned {
			r.broadcast(wire.SessionEventUpdated, si)
		}
	}
	r.broadcastProject(wire.ProjectEventRemoved, info)
	// Read the survivors HERE, not back under the lock above: the
	// RemoveAll and the N worktree teardowns in between can take
	// seconds, and a stale ProjectInfo would clobber whatever ran in
	// that window. Same hazard as Kill's fan-out.
	for _, pi := range r.ListProjects() {
		r.broadcastProject(wire.ProjectEventUpdated, pi)
	}
	return nil
}

// UpdateProject mutates project metadata. When Order is set, every
// project whose Order shifted is broadcast as updated so the GUI's
// state stays in sync — otherwise the other projects keep stale
// .order values and the relative sort flips on the next render.
func (r *Registry) UpdateProject(req wire.UpdateProjectReq) (*Project, error) {
	r.mu.Lock()
	p, ok := r.projects[req.ProjectID]
	if !ok {
		r.mu.Unlock()
		return nil, ErrProjectNotFound
	}
	if req.Name != nil {
		p.Name = *req.Name
	}
	if req.Color != nil {
		p.Color = *req.Color
	}
	if req.Cwd != nil {
		p.Cwd = *req.Cwd
	}
	orderChanged := req.Order != nil
	if orderChanged {
		r.moveProjectLocked(p.ID, *req.Order)
	}
	if err := r.persistProjectLocked(p); err != nil {
		r.mu.Unlock()
		return p, err
	}
	if err := r.persistProjectIndexLocked(); err != nil {
		r.mu.Unlock()
		return p, err
	}
	info := p.Info()
	var others []wire.ProjectInfo
	if orderChanged {
		for _, pid := range r.projectOrder {
			if other := r.projects[pid]; other != nil && other.ID != p.ID {
				others = append(others, other.Info())
			}
		}
	}
	r.mu.Unlock()
	for _, oi := range others {
		r.broadcastProject(wire.ProjectEventUpdated, oi)
	}
	r.broadcastProject(wire.ProjectEventUpdated, info)
	return p, nil
}

func (r *Registry) moveProjectLocked(id string, newOrder int) {
	if slices.Index(r.projectOrder, id) < 0 {
		return
	}
	r.projectOrder = moveInOrder(r.projectOrder, id, newOrder)
	r.reindexProjectsLocked()
}

// reindexProjectsLocked is reindexLocked for projects — same
// invariant, same reason. See registry.go.
func (r *Registry) reindexProjectsLocked() {
	for i, id := range r.projectOrder {
		if p := r.projects[id]; p != nil {
			p.Order = i
		}
	}
}

// SubscribeProjects returns a channel that receives ProjectEvent.
// Slow consumers are dropped — listeners must drain promptly.
func (r *Registry) SubscribeProjects() (ProjectListener, func()) {
	// 64 for the same reason as Subscribe: order changes broadcast one
	// event per project under lock.
	ch := make(ProjectListener, 64)
	r.mu.Lock()
	if r.projectListeners == nil {
		// Post-Close subscribe; see the matching guard in Subscribe.
		r.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	r.projectListeners[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		if _, ok := r.projectListeners[ch]; ok {
			delete(r.projectListeners, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
}

func (r *Registry) broadcastProject(kind string, info wire.ProjectInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ev := wire.ProjectEvent{Kind: kind, Project: info}
	for ch := range r.projectListeners {
		select {
		case ch <- ev:
		default:
			// Same contract as broadcastLocked: drops must be loud.
			log.Printf("registry: dropping slow project-event listener (buffer %d full, %d listeners); client is desynced until it resubscribes",
				cap(ch), len(r.projectListeners))
			delete(r.projectListeners, ch)
			close(ch)
		}
	}
}
