// Package registry tracks the daemon's open sessions and their
// user-facing metadata (name, color, order). It owns persistence so
// the daemon's main loop can stay focused on transport.
package registry

import (
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

// ErrNotFound is returned when a session ID isn't known.
var ErrNotFound = errors.New("registry: session not found")

// ErrWorktreeDirty is returned by Kill when the session is backed by
// a worktree with uncommitted changes and force=false. Callers (the
// daemon) translate this into a wire.FrameError with code
// wire.ErrCodeWorktreeDirty so the GUI can confirm with the user.
var ErrWorktreeDirty = errors.New("registry: worktree has uncommitted changes")

// Entry pairs persisted metadata with the live session. The session is
// nil for entries loaded from disk that haven't been started this run.
type Entry struct {
	ID             string
	Name           string
	Color          string
	Order          int
	Created        time.Time
	Agent          string // canonical agent ID; "" = generic shell
	ProjectID      string // owning project; "" = default project
	WorktreePath   string // absolute path of the git worktree backing this session; "" = none
	WorktreeBranch string // branch backing the worktree (informational; e.g. for sidebar tooltip)
	LastError      string // human-readable error from last failed Start/Revive; cleared on success
	sess           *session.Session // nil ⇔ not running this lifetime
}

// Project is the registry-side representation of a project.
type Project struct {
	ID      string
	Name    string
	Color   string
	Cwd     string
	Order   int
	Created time.Time
}

// Info renders the project as a wire.ProjectInfo.
func (p *Project) Info() wire.ProjectInfo {
	return wire.ProjectInfo{
		ID:      p.ID,
		Name:    p.Name,
		Color:   p.Color,
		Cwd:     p.Cwd,
		Order:   p.Order,
		Created: p.Created.UTC().Format(time.RFC3339),
	}
}

// Alive reports whether this entry has a live session attached.
func (e *Entry) Alive() bool { return e.sess != nil }

// Session returns the live session, or nil.
func (e *Entry) Session() *session.Session { return e.sess }

// Info renders the entry as a wire.SessionInfo for the protocol.
func (e *Entry) Info() wire.SessionInfo {
	return wire.SessionInfo{
		ID:             e.ID,
		Name:           e.Name,
		Color:          e.Color,
		Order:          e.Order,
		Created:        e.Created.UTC().Format(time.RFC3339),
		Alive:          e.Alive(),
		Agent:          e.Agent,
		ProjectID:      e.ProjectID,
		WorktreePath:   e.WorktreePath,
		WorktreeBranch: e.WorktreeBranch,
		LastError:      e.LastError,
	}
}

// Listener is a channel that receives SessionEvent notifications.
type Listener chan wire.SessionEvent

// Registry is the daemon-side authoritative store of sessions and
// the projects they belong to.
type Registry struct {
	mu       sync.Mutex
	entries  map[string]*Entry
	order    []string
	stateDir string

	projects     map[string]*Project
	projectOrder []string

	// lastProjectColor / lastSessionColor remember the most recent
	// auto-assigned palette color so consecutive creates pick a
	// different one. Empty string = no bias.
	lastProjectColor string
	lastSessionColor string

	// Listeners are notified of every change. Slow listeners are dropped.
	listeners map[Listener]struct{}

	// projectListeners receive project events specifically. Kept
	// separate from listeners so a sidebar can subscribe to both
	// streams without filtering.
	projectListeners map[ProjectListener]struct{}
}

// ProjectListener is a channel that receives ProjectEvent.
type ProjectListener chan wire.ProjectEvent

// Open creates or loads a Registry rooted at stateDir. Existing
// metadata on disk is loaded; live sessions are not auto-started.
func Open(stateDir string) (*Registry, error) {
	if stateDir == "" {
		stateDir = StateDir()
	}
	r := &Registry{
		entries:          make(map[string]*Entry),
		stateDir:         stateDir,
		projects:         make(map[string]*Project),
		listeners:        make(map[Listener]struct{}),
		projectListeners: make(map[ProjectListener]struct{}),
	}
	if err := r.load(); err != nil {
		return nil, fmt.Errorf("registry: load: %w", err)
	}
	return r, nil
}

// load reads index.json + every session.json under sessions/, plus
// the parallel projects/ tree. Missing files are tolerated; corrupt
// files are skipped with a best-effort recovery.
func (r *Registry) load() error {
	dir := SessionsDir(r.stateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	pdir := ProjectsDir(r.stateDir)
	if err := os.MkdirAll(pdir, 0o700); err != nil {
		return err
	}

	// Load projects first, so session->project lookups during
	// migration succeed.
	var pidx ProjectIndexFile
	_ = readJSON(filepath.Join(pdir, "index.json"), &pidx)
	pseen := make(map[string]bool)
	for _, id := range pidx.Order {
		var meta ProjectMetaFile
		if err := readJSON(filepath.Join(pdir, id, "project.json"), &meta); err != nil {
			continue
		}
		r.projects[meta.ID] = &Project{
			ID: meta.ID, Name: meta.Name, Color: meta.Color, Cwd: meta.Cwd,
			Order: meta.Order, Created: meta.Created,
		}
		r.projectOrder = append(r.projectOrder, meta.ID)
		pseen[meta.ID] = true
	}
	if dirs, err := os.ReadDir(pdir); err == nil {
		for _, d := range dirs {
			if !d.IsDir() || pseen[d.Name()] {
				continue
			}
			var meta ProjectMetaFile
			if err := readJSON(filepath.Join(pdir, d.Name(), "project.json"), &meta); err != nil {
				continue
			}
			meta.Order = len(r.projectOrder)
			r.projects[meta.ID] = &Project{
				ID: meta.ID, Name: meta.Name, Color: meta.Color, Cwd: meta.Cwd,
				Order: meta.Order, Created: meta.Created,
			}
			r.projectOrder = append(r.projectOrder, meta.ID)
		}
	}

	var idx IndexFile
	_ = readJSON(filepath.Join(dir, "index.json"), &idx) // OK if missing

	// Build entries from per-session metadata files. The index gives
	// order; any sessions present on disk but missing from the index
	// are appended to the end.
	seen := make(map[string]bool)
	for _, id := range idx.Order {
		var meta MetaFile
		if err := readJSON(filepath.Join(dir, id, "session.json"), &meta); err != nil {
			continue
		}
		r.entries[meta.ID] = &Entry{
			ID: meta.ID, Name: meta.Name, Color: meta.Color,
			Order: meta.Order, Created: meta.Created, Agent: meta.Agent,
			ProjectID:      meta.ProjectID,
			WorktreePath:   meta.WorktreePath,
			WorktreeBranch: meta.WorktreeBranch,
		}
		r.order = append(r.order, meta.ID)
		seen[meta.ID] = true
	}
	// Catch sessions on disk not in the index.
	if dirs, err := os.ReadDir(dir); err == nil {
		for _, d := range dirs {
			if !d.IsDir() || seen[d.Name()] {
				continue
			}
			var meta MetaFile
			if err := readJSON(filepath.Join(dir, d.Name(), "session.json"), &meta); err != nil {
				continue
			}
			meta.Order = len(r.order)
			r.entries[meta.ID] = &Entry{
				ID: meta.ID, Name: meta.Name, Color: meta.Color,
				Order: meta.Order, Created: meta.Created, Agent: meta.Agent,
				ProjectID:      meta.ProjectID,
				WorktreePath:   meta.WorktreePath,
				WorktreeBranch: meta.WorktreeBranch,
			}
			r.order = append(r.order, meta.ID)
		}
	}
	return nil
}

// List returns a snapshot of all entries in display order.
func (r *Registry) List() []wire.SessionInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]wire.SessionInfo, 0, len(r.order))
	for _, id := range r.order {
		if e := r.entries[id]; e != nil {
			out = append(out, e.Info())
		}
	}
	return out
}

// Get returns the entry for id, or nil.
func (r *Registry) Get(id string) *Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.entries[id]
}

// Create adds a new session and starts it. Metadata persists before
// the session starts so a crash mid-Create still surfaces the entry.
func (r *Registry) Create(spec wire.CreateSpec) (*Entry, error) {
	r.mu.Lock()
	id := uuid.NewString()
	// Resolve owning project first so we can avoid its color when
	// auto-picking the session color (otherwise the gradient could
	// collapse to a flat hue).
	projectID := spec.ProjectID
	if projectID == "" {
		projectID = r.defaultProjectIDLocked()
	}
	var projectColor string
	if p, ok := r.projects[projectID]; ok {
		projectColor = p.Color
	}
	// Color is reserved for project/session identity; agent identity
	// is conveyed by the badge/icon. So skip the agent-default tier
	// and pick a random palette color when the caller didn't choose.
	color := spec.Color
	if color == "" {
		color = pickColor(r.lastSessionColor, projectColor)
		r.lastSessionColor = color
	}
	// Resolve cwd up front (under the same lock) so we can decide on a
	// worktree branch BEFORE naming the session — the session name is
	// derived from the worktree branch when one is in play, so the
	// user can find the worktree directory from the session label.
	cwd := spec.Cwd
	if cwd == "" {
		if p, ok := r.projects[projectID]; ok && p.Cwd != "" {
			cwd = p.Cwd
		}
	}
	// Detect when cwd already lives in a worktree owned by another
	// session in the same project (e.g. ⌘P duplicate). The new entry
	// adopts that worktree's path+branch so the sidebar shows the
	// worktree badge and Kill can keep the worktree alive until the
	// last session in it goes away.
	var adoptedPath, adoptedBranch string
	if !spec.UseWorktree && cwd != "" {
		for _, other := range r.entries {
			if other.ProjectID == projectID && other.WorktreePath != "" && other.WorktreePath == cwd {
				adoptedPath = other.WorktreePath
				adoptedBranch = other.WorktreeBranch
				break
			}
		}
	}
	r.mu.Unlock()

	// Pre-resolve the worktree branch+path so the session name can
	// match the worktree directory. ResolveBranchAndPath only picks a
	// free name; the actual `git worktree add` happens further down.
	var wtPath, wtBranch string
	if adoptedPath != "" {
		wtPath, wtBranch = adoptedPath, adoptedBranch
	}
	if spec.UseWorktree && cwd != "" && worktree.IsGitRepo(cwd) {
		if root, err := worktree.Root(cwd); err == nil {
			if b, p, rerr := worktree.ResolveBranchAndPath(root, spec.Branch); rerr == nil {
				wtBranch, wtPath = b, p
			} else {
				log.Printf("registry: worktree.ResolveBranchAndPath: %v", rerr)
			}
		}
	}

	name := spec.Name
	// nameFromBranch records whether the name was derived from the
	// pre-resolved worktree branch. If `git worktree add` later fails
	// we'll rename to a random label so the persisted name doesn't
	// claim a worktree that doesn't exist.
	nameFromBranch := false
	if name == "" {
		if wtBranch != "" {
			// Tie the session name to the worktree directory so the
			// user can find the worktree from the session label.
			// Slashes (e.g. `feature/foo`) get folded to `-` so the
			// name is safe to use in paths and shell-quoted contexts.
			suffix := spec.Agent
			if suffix == "" {
				suffix = "shell"
			}
			name = strings.ReplaceAll(wtBranch, "/", "-") + " " + suffix
			nameFromBranch = true
		} else {
			name = agent.RandomName(agent.ID(spec.Agent))
		}
	}

	r.mu.Lock()
	// Re-validate projectID after the unlock window above: a concurrent
	// KillProject could have removed it. Fall back to the default
	// project rather than persisting a dangling reference.
	if _, ok := r.projects[projectID]; !ok {
		projectID = r.defaultProjectIDLocked()
	}
	e := &Entry{
		ID: id, Name: name, Color: color,
		Order: len(r.order), Created: time.Now().UTC(),
		Agent: spec.Agent, ProjectID: projectID,
	}
	r.entries[id] = e
	r.order = append(r.order, id)
	if err := r.persistEntryLocked(e); err != nil {
		delete(r.entries, id)
		r.order = r.order[:len(r.order)-1]
		r.mu.Unlock()
		return nil, err
	}
	if err := r.persistIndexLocked(); err != nil {
		delete(r.entries, id)
		r.order = r.order[:len(r.order)-1]
		r.mu.Unlock()
		return nil, err
	}
	r.mu.Unlock()

	// Start the session outside the lock so the PTY fork doesn't block
	// the registry. If the spec names an agent (and no explicit Cmd),
	// look up its default command and use it.
	cmd := spec.Cmd
	if len(cmd) == 0 && spec.Agent != "" {
		if def, ok := agent.Get(agent.ID(spec.Agent)); ok && len(def.Cmd) > 0 {
			cmd = def.Cmd
		}
	}

	// Now actually create the worktree (heavy `git worktree add`).
	// Failure here is non-fatal — the session falls back to the plain
	// project cwd. Aborting create on worktree failure would block
	// users on marginal repos (shallow clones, sandbox restrictions,
	// slow filesystems).
	// Skip the create when we're adopting an existing worktree from a
	// sibling session — the directory is already on disk and `git
	// worktree add` would fail.
	if wtBranch != "" && adoptedPath == "" {
		if root, err := worktree.Root(cwd); err == nil {
			if cerr := worktree.CreateWorktree(root, wtBranch, wtPath); cerr != nil {
				log.Printf("registry: worktree create failed (falling back to plain session): %v", cerr)
				wtPath, wtBranch = "", ""
			} else {
				cwd = wtPath
				worktree.EnsureGitignore(root)
				log.Printf("registry: created worktree %s on branch %s", wtPath, wtBranch)
			}
		} else {
			wtPath, wtBranch = "", ""
		}
	}
	// If the name was derived from a worktree branch but the worktree
	// failed to materialize, the persisted name would lie about reality
	// ("feature-foo claude" with no worktree). Rename to a random label
	// and re-persist so the session label matches what actually exists.
	if nameFromBranch && wtBranch == "" {
		r.mu.Lock()
		e.Name = agent.RandomName(agent.ID(spec.Agent))
		_ = r.persistEntryLocked(e)
		r.mu.Unlock()
	}

	sess, err := session.Start(session.Options{
		Shell: spec.Shell,
		Cmd:   cmd,
		Cwd:   cwd,
		Cols:  spec.Cols,
		Rows:  spec.Rows,
	})
	if err != nil {
		log.Printf("registry: session.Start failed for %s (agent=%q cmd=%v): %v",
			e.ID, spec.Agent, cmd, err)
		// Strand the metadata as a dead entry. The user can recreate
		// or kill it. Store the error so the GUI can surface it.
		r.mu.Lock()
		e.LastError = err.Error()
		r.mu.Unlock()
		r.broadcast(wire.SessionEventAdded, e.Info())
		return e, err
	}
	r.mu.Lock()
	// The session.Session uses its own UUID; we override with the
	// registry id so the registry id is the public identity.
	sess.ID = id
	e.sess = sess
	if wtPath != "" {
		e.WorktreePath = wtPath
		e.WorktreeBranch = wtBranch
		_ = r.persistEntryLocked(e)
	}
	r.mu.Unlock()
	r.broadcast(wire.SessionEventAdded, e.Info())
	go r.watchSessionExit(id, sess)
	return e, nil
}

// Revive starts a fresh process on the existing entry. No-op if the
// entry already has a live session. Used on daemon startup to bring
// previously-persisted sessions back to a usable state.
//
// If the entry's Agent is set, we re-resolve the agent's command via
// the agent package — this means an agent binary moved on disk
// between runs (e.g. nvm switch) is picked up automatically. If the
// agent ID is unknown (e.g. a future agent rolled back), we fall back
// to a generic shell.
//
// Note: Phase 1.7 (disk-backed scrollback) will replay prior content
// on revive. Today the slot is preserved but starts blank.
func (r *Registry) Revive(id string, opts session.Options) error {
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	if e.sess != nil {
		r.mu.Unlock()
		return nil
	}
	agentID := e.Agent
	wtPath := e.WorktreePath
	r.mu.Unlock()

	if agentID != "" && len(opts.Cmd) == 0 {
		if def, ok := agent.Get(agent.ID(agentID)); ok && len(def.Cmd) > 0 {
			opts.Cmd = def.Cmd
		}
	}

	// If the entry is supposed to live in a worktree, prefer the
	// worktree path as cwd. If the dir vanished out-from-under us
	// (e.g. user removed it manually), self-heal: clear the worktree
	// fields and broadcast an updated event so the GUI drops the
	// worktree badge. The session falls back to the project cwd.
	if wtPath != "" {
		if _, err := os.Stat(wtPath); err == nil {
			opts.Cwd = wtPath
		} else {
			log.Printf("registry: revive %s: worktree %s missing; clearing", id, wtPath)
			r.mu.Lock()
			e.WorktreePath = ""
			e.WorktreeBranch = ""
			_ = r.persistEntryLocked(e)
			info := e.Info()
			r.mu.Unlock()
			r.broadcast(wire.SessionEventUpdated, info)
		}
	}

	sess, err := session.Start(opts)
	if err != nil {
		r.mu.Lock()
		e.LastError = err.Error()
		info := e.Info()
		r.mu.Unlock()
		r.broadcast(wire.SessionEventUpdated, info)
		return err
	}
	sess.ID = id

	r.mu.Lock()
	e.sess = sess
	e.LastError = ""
	info := e.Info()
	r.mu.Unlock()
	r.broadcast(wire.SessionEventUpdated, info)
	go r.watchSessionExit(id, sess)
	return nil
}

// Restart terminates the agent process running in the session and
// respawns it in place. The Entry (its ID, Name, Color, Order, and
// worktree binding) is preserved — only the underlying PTY/process
// is recycled. If the agent has a ResumeCmd defined we use it so
// the new process picks up the prior conversation; otherwise we
// fall back to the agent's normal Cmd. If the session is already
// dead (e.sess == nil) we just respawn.
//
// Use cases:
//   - User updated agent skills/config and wants the agent to
//     re-read them without losing the conversation.
//   - (Future) recovering after RestartDaemon.
func (r *Registry) Restart(id string) error {
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	sess := e.sess
	agentID := e.Agent
	projectCwd := ""
	if p, ok := r.projects[e.ProjectID]; ok {
		projectCwd = p.Cwd
	}
	r.mu.Unlock()

	// Tear down the current PTY. watchSessionExit also observes the
	// close, but it races with our Revive call below — Revive's
	// "already alive" guard short-circuits if e.sess hasn't been
	// cleared yet. Wait for the readLoop to drain, then clear e.sess
	// ourselves under the lock so Revive sees a vacant slot.
	// watchSessionExit will then no-op (it checks e.sess == sess).
	if sess != nil {
		_ = sess.Close()
		<-sess.Done()
		r.mu.Lock()
		if e2, ok := r.entries[id]; ok && e2.sess == sess {
			e2.sess = nil
		}
		r.mu.Unlock()
	}

	var opts session.Options
	if def, ok := agent.Get(agent.ID(agentID)); ok {
		if len(def.ResumeCmd) > 0 {
			opts.Cmd = def.ResumeCmd
		} else {
			opts.Cmd = def.Cmd
		}
	}
	// Pass the project cwd as the fallback. Revive promotes opts.Cwd to
	// wtPath when the worktree directory still exists; if the user removed
	// it out-of-band, Revive's self-heal clears the worktree fields but
	// leaves opts.Cwd alone — projectCwd is what session.Start should use.
	opts.Cwd = projectCwd

	return r.Revive(id, opts)
}

// Adopt registers an externally-started session under the given
// metadata. Used by the daemon for its bootstrap session in Phase 2
// transitional code (before the GUI calls CREATE_SESSION explicitly).
func (r *Registry) Adopt(s *session.Session, name, color string) (*Entry, error) {
	r.mu.Lock()
	id := s.ID
	if existing := r.entries[id]; existing != nil {
		existing.sess = s
		r.mu.Unlock()
		r.broadcast(wire.SessionEventUpdated, existing.Info())
		go r.watchSessionExit(id, s)
		return existing, nil
	}
	if name == "" {
		name = agent.RandomName("")
	}
	if color == "" {
		color = pickColor(r.lastSessionColor)
		r.lastSessionColor = color
	}
	e := &Entry{
		ID: id, Name: name, Color: color,
		Order: len(r.order), Created: time.Now().UTC(), sess: s,
	}
	r.entries[id] = e
	r.order = append(r.order, id)
	_ = r.persistEntryLocked(e)
	_ = r.persistIndexLocked()
	r.mu.Unlock()
	r.broadcast(wire.SessionEventAdded, e.Info())
	go r.watchSessionExit(id, s)
	return e, nil
}

// watchSessionExit waits for sess to exit, then — if the entry is
// still attached to *this* session (not already replaced by a Revive
// or removed by Kill) — clears e.sess and broadcasts an Updated event
// so clients see Alive: false. The PTY's own resources are released
// by readLoop's defer.
func (r *Registry) watchSessionExit(id string, sess *session.Session) {
	<-sess.Done()
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok || e.sess != sess {
		r.mu.Unlock()
		return
	}
	e.sess = nil
	info := e.Info()
	r.mu.Unlock()
	r.broadcast(wire.SessionEventUpdated, info)
}

// Kill terminates the session and removes its entry from the registry.
// The on-disk metadata directory is also removed. If the session is
// backed by a git worktree, the worktree is also cleaned up
// (`git worktree remove --force`, `os.RemoveAll`, `git worktree prune`).
//
// When force is false and the worktree has uncommitted changes,
// returns ErrWorktreeDirty without modifying any state. Callers can
// retry with force=true after confirming with the user.
func (r *Registry) Kill(id string, force bool) error {
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok {
		r.mu.Unlock()
		return ErrNotFound
	}

	// Capture worktree state and resolved repo root BEFORE we remove
	// the entry from the map. Kill happens outside the lock; we'd
	// lose the data otherwise.
	wtPath, wtBranch := e.WorktreePath, e.WorktreeBranch
	var projectCwd string
	if p, ok := r.projects[e.ProjectID]; ok {
		projectCwd = p.Cwd
	}
	// Count siblings sharing this worktree. The worktree is only
	// cleaned up when the LAST session in it is killed — duplicating
	// (⌘P) creates extra entries that share the same worktree dir.
	worktreeShared := false
	if wtPath != "" {
		for sid, other := range r.entries {
			if sid != id && other.WorktreePath == wtPath {
				worktreeShared = true
				break
			}
		}
	}
	r.mu.Unlock()

	// Pre-flight safety check on the worktree. Returning here leaves
	// everything intact so the user can retry with force=true. Skip
	// the check when other sessions still live in the worktree —
	// killing this one won't remove the directory, so dirtiness is
	// irrelevant.
	if wtPath != "" && !worktreeShared && !force {
		dirty, _ := worktree.HasUncommitted(wtPath)
		if dirty {
			return ErrWorktreeDirty
		}
	}

	r.mu.Lock()
	// Re-resolve the entry — the world may have changed while we were
	// running the dirty check.
	e, ok = r.entries[id]
	if !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	delete(r.entries, id)
	for i, sid := range r.order {
		if sid == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	// Re-check sibling count after removing this entry, in case the
	// world changed during the dirty check. Cleanup only runs when
	// nobody else lives in the worktree.
	if wtPath != "" {
		worktreeShared = false
		for _, other := range r.entries {
			if other.WorktreePath == wtPath {
				worktreeShared = true
				break
			}
		}
	}
	// Intentionally NOT renumbering remaining entries here. Orders
	// were assigned at create time and only change on an explicit
	// user move. Kill leaves a hole in the sequence (e.g. 0,1,3,4)
	// — the sort-by-Order render handles holes fine, and not
	// touching the field means we never have to broadcast spurious
	// SessionEventUpdated for sessions the user didn't touch.
	_ = r.persistIndexLocked()
	dir := filepath.Join(SessionsDir(r.stateDir), id)
	r.mu.Unlock()

	// Order: PTY first (releases any FD/cwd handles into the
	// worktree), worktree second (now safe to git worktree remove),
	// metadata last (so a crash mid-cleanup leaves a recoverable
	// orphan that the next daemon-startup scan reclaims).
	if e.sess != nil {
		_ = e.sess.Close()
	}
	if wtPath != "" && !worktreeShared {
		root, err := worktree.Root(projectCwd)
		switch {
		case err != nil:
			log.Printf("registry: kill %s: project cwd %q is not (or no longer) a git repo; falling back to RemoveAll on %s", id, projectCwd, wtPath)
			_ = os.RemoveAll(wtPath)
		case !strings.HasPrefix(wtPath, root):
			// The worktree path lives outside the current project
			// repo (project cwd was changed). Don't run `git worktree
			// remove` against an unrelated repo; just rm -rf.
			log.Printf("registry: kill %s: worktree %s lives outside current project repo %s; using RemoveAll only", id, wtPath, root)
			_ = os.RemoveAll(wtPath)
		default:
			if err := worktree.Cleanup(root, wtPath); err != nil {
				log.Printf("registry: worktree cleanup failed for %s: %v (branch=%s)", id, err, wtBranch)
			}
		}
	}
	_ = os.RemoveAll(dir)
	r.broadcast(wire.SessionEventRemoved, e.Info())
	return nil
}

// Update mutates name / color / order. Pointer fields opt in. When
// Order is set, ALL sessions whose Order shifted are broadcast as
// updated events so the GUI's state stays in sync (otherwise the
// other sessions keep stale .order values, and the relative sort
// can flip on the next render).
func (r *Registry) Update(req wire.UpdateSessionReq) (*Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[req.SessionID]
	if !ok {
		return nil, ErrNotFound
	}
	if req.Name != nil {
		e.Name = *req.Name
	}
	if req.Color != nil {
		e.Color = *req.Color
	}
	orderChanged := req.Order != nil
	if orderChanged {
		r.moveLocked(e.ID, *req.Order)
	}
	if err := r.persistEntryLocked(e); err != nil {
		return e, err
	}
	if err := r.persistIndexLocked(); err != nil {
		return e, err
	}
	if orderChanged {
		// Notify clients of every session, since renumberLocked
		// touched all of them. Cheap: a few entries times one
		// channel send each.
		for _, sid := range r.order {
			if other := r.entries[sid]; other != nil && other.ID != e.ID {
				r.broadcastLocked(wire.SessionEventUpdated, other.Info())
			}
		}
	}
	defer r.broadcastLocked(wire.SessionEventUpdated, e.Info())
	return e, nil
}

// Subscribe returns a channel that receives every SessionEvent. The
// returned cleanup function unsubscribes and closes the channel.
// Slow consumers are dropped — listeners must drain promptly.
func (r *Registry) Subscribe() (Listener, func()) {
	ch := make(Listener, 16)
	r.mu.Lock()
	r.listeners[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		if _, ok := r.listeners[ch]; ok {
			delete(r.listeners, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
}

// Close terminates every live session and clears listeners. The on-disk
// metadata is preserved.
func (r *Registry) Close() error {
	r.mu.Lock()
	for ch := range r.listeners {
		close(ch)
	}
	for ch := range r.projectListeners {
		close(ch)
	}
	r.listeners = nil
	r.projectListeners = nil
	entries := r.entries
	r.entries = nil
	r.order = nil
	r.projects = nil
	r.projectOrder = nil
	r.mu.Unlock()
	for _, e := range entries {
		if e.sess != nil {
			_ = e.sess.Close()
		}
	}
	return nil
}

// --- internal helpers below ---

func (r *Registry) moveLocked(id string, newOrder int) {
	cur := -1
	for i, s := range r.order {
		if s == id {
			cur = i
			break
		}
	}
	if cur < 0 {
		return
	}
	r.order = append(r.order[:cur], r.order[cur+1:]...)
	if newOrder < 0 {
		newOrder = 0
	}
	if newOrder > len(r.order) {
		newOrder = len(r.order)
	}
	r.order = append(r.order[:newOrder], append([]string{id}, r.order[newOrder:]...)...)
	r.renumberLocked()
}

func (r *Registry) renumberLocked() {
	for i, id := range r.order {
		if e := r.entries[id]; e != nil {
			e.Order = i
		}
	}
	// Re-persist any entries whose Order changed. We re-write all of
	// them rather than diff: the volume is small.
	for _, id := range r.order {
		if e := r.entries[id]; e != nil {
			_ = r.persistEntryLocked(e)
		}
	}
}

func (r *Registry) persistEntryLocked(e *Entry) error {
	path := filepath.Join(SessionsDir(r.stateDir), e.ID, "session.json")
	return writeJSON(path, MetaFile{
		ID: e.ID, Name: e.Name, Color: e.Color,
		Order: e.Order, Created: e.Created, Agent: e.Agent,
		ProjectID:      e.ProjectID,
		WorktreePath:   e.WorktreePath,
		WorktreeBranch: e.WorktreeBranch,
	})
}

func (r *Registry) persistProjectLocked(p *Project) error {
	path := filepath.Join(ProjectsDir(r.stateDir), p.ID, "project.json")
	return writeJSON(path, ProjectMetaFile{
		ID: p.ID, Name: p.Name, Color: p.Color, Cwd: p.Cwd,
		Order: p.Order, Created: p.Created,
	})
}

func (r *Registry) persistProjectIndexLocked() error {
	idx := ProjectIndexFile{Order: append([]string(nil), r.projectOrder...)}
	return writeJSON(filepath.Join(ProjectsDir(r.stateDir), "index.json"), idx)
}

func (r *Registry) persistIndexLocked() error {
	idx := IndexFile{Order: append([]string(nil), r.order...)}
	sort.SliceStable(idx.Order, func(i, j int) bool { return false }) // preserve order
	return writeJSON(filepath.Join(SessionsDir(r.stateDir), "index.json"), idx)
}

func (r *Registry) broadcast(kind string, info wire.SessionInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.broadcastLocked(kind, info)
}

func (r *Registry) broadcastLocked(kind string, info wire.SessionInfo) {
	ev := wire.SessionEvent{Kind: kind, Session: info}
	for ch := range r.listeners {
		select {
		case ch <- ev:
		default:
			// drop slow listener
			delete(r.listeners, ch)
			close(ch)
		}
	}
}

// colorPalette is the curated set of "good" hues used for auto-
// assigned project and session colors. All are readable as text on
// the dark sidebar. Users can override via Update.
var colorPalette = []string{
	"#f59e0b", // amber
	"#f97316", // orange
	"#ef4444", // red
	"#ec4899", // pink
	"#d946ef", // fuchsia
	"#a855f7", // purple
	"#8b5cf6", // violet
	"#6366f1", // indigo
	"#3b82f6", // sky
	"#06b6d4", // cyan
	"#14b8a6", // teal
	"#10b981", // emerald
	"#84cc16", // lime
	"#eab308", // yellow
}

// pickColor returns a random palette color, excluding any entry
// listed in avoid. Uses math/rand/v2 top-level helpers so it's
// goroutine-safe without needing a lock at the call site.
func pickColor(avoid ...string) string {
	n := len(colorPalette)
	idx := rand.IntN(n)
	for i := range n {
		c := colorPalette[(idx+i)%n]
		if !slices.Contains(avoid, c) {
			return c
		}
	}
	// Every palette entry is in avoid — fall back to a uniform pick.
	return colorPalette[idx]
}
