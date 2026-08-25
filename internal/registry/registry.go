// Package registry tracks the daemon's open sessions and their
// user-facing metadata (name, color, order). It owns persistence so
// the daemon's main loop can stay focused on transport.
package registry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

// ErrNotFound is returned when a session ID isn't known.
var ErrNotFound = errors.New("registry: session not found")

// startSession is the package-level seam used to spawn the underlying
// PTY. Tests swap this to capture the resolved session.Options
// without forking real agent binaries (e.g. to inspect agent argv);
// the test seam still execs a benign stub like `sleep` so the
// returned *session.Session behaves normally.
var startSession = session.Start

// startSessionMu guards the seam itself. Creates now run concurrently
// (the daemon dispatches them off its control read loop), so a test
// swapping the seam races the in-flight create that is reading it.
var startSessionMu sync.RWMutex

// spawn calls the current seam. Every production call site goes
// through here.
func spawn(opts session.Options) (*session.Session, error) {
	startSessionMu.RLock()
	fn := startSession
	startSessionMu.RUnlock()
	return fn(opts)
}

// SetStartSessionForTest swaps the PTY spawn seam and returns a
// restore func. Test-only, and exported because the daemon's own
// tests (a different package) need to hold a create open long enough
// to prove the control loop keeps serving other clients. Mirrors the
// existing Daemon.Registry() "exposed for tests" escape hatch.
func SetStartSessionForTest(fn func(session.Options) (*session.Session, error)) func() {
	startSessionMu.Lock()
	prev := startSession
	startSession = fn
	startSessionMu.Unlock()
	return func() {
		startSessionMu.Lock()
		startSession = prev
		startSessionMu.Unlock()
	}
}

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
	// AgentSessionID is the id the agent CLI uses to identify this
	// conversation. For agents that accept a caller-chosen id at first
	// launch (Claude: --session-id) this equals Entry.ID. For agents
	// whose id is auto-generated (Codex) it's captured post-spawn from
	// the agent's session-rollout file. Empty ⇔ not yet captured /
	// agent does not support per-id resume; Restart then falls back to
	// the agent's generic ResumeCmd. Daemon-internal — not on the wire.
	AgentSessionID string
	LastError      string           // human-readable error from last failed Start/Revive; cleared on success
	sess           *session.Session // nil ⇔ not running this lifetime

	// Phase is the lifecycle phase surfaced to clients (see the
	// wire.Phase* constants). In-memory only: never persisted, so a
	// daemon restart can't strand an entry mid-create. The zero value
	// is wire.PhaseReady.
	Phase string

	// captureCancel cancels the post-spawn AgentSessionID capture
	// goroutine when the session exits before capture completes.
	// nil when no capture is in flight.
	captureCancel context.CancelFunc
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
		Phase:          e.Phase,
		Title:          e.title(),
	}
}

// title returns the window title of the live session, truncated to the
// wire cap. Read through to the session rather than mirrored into a
// field on Entry: that makes "no live process ⇒ no title" fall out for
// free, so death, restart and daemon boot need no clearing code (which
// is exactly the discipline the mirrored Phase field does require, at
// four separate `e.sess = …` sites).
func (e *Entry) title() string {
	if e.sess == nil {
		return ""
	}
	return truncateTitle(e.sess.Title())
}

// truncateTitle caps a window title at wire.MaxTitleLen bytes. The title
// is whatever the child process chose to emit, so it is bounded at the
// boundary rather than trusted — it is rebroadcast to every connected
// client each time it changes.
func truncateTitle(t string) string {
	if len(t) <= wire.MaxTitleLen {
		return t
	}
	// Byte-slice, then drop any partial rune left at the tail so the
	// field stays valid UTF-8 for JSON encoding.
	return strings.ToValidUTF8(t[:wire.MaxTitleLen], "")
}

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

	// createMu serializes the synchronous prefix of Create (id/color
	// resolution, name planning, order splicing). The daemon now runs
	// Create off its control read loop, so without this two rapid ⌘N
	// presses could interleave and land in a surprising order.
	createMu sync.Mutex

	// gitMu serializes the git subprocesses that create and remove
	// worktrees. Concurrent `git worktree add`/`remove` in one repo
	// collide on index.lock.
	//
	// Lock ordering rule: NEVER acquire gitMu while holding r.mu. The
	// create tail takes gitMu and then setPhase takes r.mu, so the
	// reverse order anywhere is an ABBA deadlock.
	//
	// ponytail: one global git lock; key it per repo root if
	// multi-repo create throughput ever matters.
	gitMu sync.Mutex
}

// Phase reports the entry's current lifecycle phase (wire.Phase*), or
// wire.PhaseReady when the id is unknown. Race-free accessor for
// readers outside the package — Entry.Phase itself is written under
// r.mu by the create/kill/restart paths.
func (r *Registry) Phase(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[id]; ok {
		return e.Phase
	}
	return wire.PhaseReady
}

// setPhase records a lifecycle phase and broadcasts it. Takes r.mu, so
// callers must not already hold it (and must not hold gitMu ordering
// backwards — see Registry.gitMu). No-op when the entry is gone or
// already in that phase, so a redundant transition costs no event.
func (r *Registry) setPhase(id, phase string) {
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok || e.Phase == phase {
		r.mu.Unlock()
		return
	}
	e.Phase = phase
	info := e.Info()
	// broadcastLocked, not broadcast: broadcast takes r.mu and
	// sync.Mutex is not reentrant.
	r.broadcastLocked(wire.SessionEventUpdated, info)
	r.mu.Unlock()
}

// attachTitleHook wires a freshly-assigned session's window-title
// reports back to this registry. Called at each site that assigns
// Entry.sess, so a session created, restarted or revived all report
// alike.
//
// Safe to call while holding r.mu: SetTitleHook takes only the session's
// own lock, and the hook it installs takes only r.mu (never both at
// once, since the session releases its lock before invoking it). The
// resulting order is one-way, r.mu → session.mu, matching every other
// registry→session call.
func (r *Registry) attachTitleHook(id string, sess *session.Session) {
	sess.SetTitleHook(func(string) { r.noteTitleChange(id) })
}

// noteTitleChange announces an entry after its session reported a new
// window title. Emitted as SessionEventTitle, not SessionEventUpdated —
// see the constant's comment for why the two are kept apart. Installed on the session as a hook at the two sites that
// assign Entry.sess, and invoked from the session's readLoop goroutine
// with no session lock held — which is what keeps the registry→session
// import direction from becoming a lock cycle.
//
// There is no unchanged-guard here: the session already coalesces and
// only calls the hook when the title actually changed. The title itself
// is re-read via Info() rather than taken from the argument, so what
// clients receive is always the entry's current state.
func (r *Registry) noteTitleChange(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok || e.sess == nil {
		return
	}
	r.broadcastLocked(wire.SessionEventTitle, e.Info())
}

// MarkPendingRevive puts every entry that has no live session into
// PhaseReviving. The daemon calls this on the boot path BEFORE it
// binds its socket, so the first snapshot any client can see already
// says "starting" instead of the alive:false + PhaseReady combination
// that every client reads as death — an entry loaded from disk has no
// session yet, and reviveAll forks its PTY later, sequentially.
// Phases are in-memory only, so this can never persist.
//
// The phase is PhaseReviving rather than PhaseSpawning precisely so it
// stays exclusive to this boot path: finishCreate parks an in-flight
// create in PhaseSpawning with no session either, and a claim that
// accepted that phase would let the boot revive fork a second PTY for
// a session someone is already creating.
func (r *Registry) MarkPendingRevive() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.sess == nil && e.Phase == wire.PhaseReady {
			e.Phase = wire.PhaseReviving
		}
	}
}

// setPhaseIf moves the entry from one phase to another only if it is
// still in `from`, reporting whether it did. The compare and the set
// share one critical section, which is what makes a phase bracket
// safe against a concurrent lifecycle op: a kill that moved the entry
// to PhaseChecking while we were spawning must not be flipped back to
// ready underneath the GUI.
func (r *Registry) setPhaseIf(id, from, to string) bool {
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok || e.Phase != from {
		r.mu.Unlock()
		return false
	}
	if from == to {
		r.mu.Unlock()
		return true
	}
	e.Phase = to
	info := e.Info()
	r.broadcastLocked(wire.SessionEventUpdated, info)
	r.mu.Unlock()
	return true
}

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
		// Index position wins over meta.Order — see the session
		// loader below for why the per-entity copy is advisory.
		r.projects[meta.ID] = &Project{
			ID: meta.ID, Name: meta.Name, Color: meta.Color, Cwd: meta.Cwd,
			Order: len(r.projectOrder), Created: meta.Created,
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
		// Order comes from the index position, never from meta.Order:
		// the per-session copy is advisory and goes stale whenever a
		// sibling is killed or moved. Deriving it here also heals any
		// state dir already skewed by an older build.
		r.entries[meta.ID] = &Entry{
			ID: meta.ID, Name: meta.Name, Color: meta.Color,
			Order: len(r.order), Created: meta.Created, Agent: meta.Agent,
			ProjectID:      meta.ProjectID,
			WorktreePath:   meta.WorktreePath,
			WorktreeBranch: meta.WorktreeBranch,
			AgentSessionID: meta.AgentSessionID,
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
				AgentSessionID: meta.AgentSessionID,
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

// ReviveWithPhase is Revive bracketed in wire.PhaseSpawning, for the
// daemon's background boot revive: a client that attaches while the
// PTY is still forking then gets "session is still starting" (and the
// GUI's phase spinner) instead of "session_dead".
//
// The bool reports whether this call owned the revive. False means
// another lifecycle op (create tail, restart, kill) held the entry
// and nothing was done — the caller decides whether to come back.
//
// Unlike Restart, ready is set AFTER Revive, not before: the whole
// point is that the phase covers the fork. Revive broadcasts
// alive:true from inside while the phase still reads "spawning", and
// the setPhase below is the later event that unsticks a client gating
// its attach on ready — Restart has no such trailing event, which is
// why it clears the phase first.
func (r *Registry) ReviveWithPhase(id string, opts session.Options) (bool, error) {
	// Claim the entry: the check and the set are one critical
	// section, so a kill or restart that got there first keeps it.
	// PhaseReviving is the boot pre-mark (see MarkPendingRevive) and
	// nothing else ever sets it, so accepting it here claims exactly
	// the entries this daemon restored from disk — and never an
	// in-flight create, which sits in PhaseSpawning with no session.
	if !r.setPhaseIf(id, wire.PhaseReady, wire.PhaseSpawning) &&
		!r.setPhaseIf(id, wire.PhaseReviving, wire.PhaseSpawning) {
		return false, nil
	}
	err := r.Revive(id, opts)
	// Compare-and-set on the way out too: a kill that started while
	// we were spawning has already moved the entry to PhaseChecking
	// or PhaseClosing, and clearing to ready there would show a
	// ready tile for a session being torn down.
	r.setPhaseIf(id, wire.PhaseSpawning, wire.PhaseReady)
	return true, err
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
// The cwd is taken from the entry's project (overridden by the
// worktree path when one exists). The caller's opts.Cwd is ignored
// when the entry has a project — daemon startup has no useful cwd
// to contribute, and using the daemon's launch dir leads to PATH
// resolution failures (e.g. project-local `node_modules/.bin`
// symlinks like `codex` won't be found if revive runs from `/`).
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
	agentSessionID := e.AgentSessionID
	projectCwd := ""
	if p, ok := r.projects[e.ProjectID]; ok {
		projectCwd = p.Cwd
	}
	r.mu.Unlock()

	if projectCwd != "" {
		opts.Cwd = projectCwd
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
			r.persistEntryLoggedLocked(e, "revive (worktree missing)")
			info := e.Info()
			r.mu.Unlock()
			r.broadcast(wire.SessionEventUpdated, info)
		}
	}

	if agentID != "" && len(opts.Cmd) == 0 {
		if def, ok := agent.Get(agent.ID(agentID)); ok && len(def.Cmd) > 0 {
			// If we previously pinned this entry to an agent
			// conversation id (Claude --session-id at first launch,
			// or codex post-spawn capture), resume that exact
			// conversation. Otherwise the daemon-startup respawn
			// runs a bare agent in the cwd and re-introduces the
			// path-scoped ambiguity #165 fixed.
			if agentSessionID != "" && def.ResumeArgs != nil {
				opts.Cmd = def.ResumeArgs(agentSessionID, opts.Cwd)
			} else {
				opts.Cmd = def.Cmd
			}
		}
	}

	sess, err := spawn(opts)
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
	// Re-resolve before binding: spawn happens outside the lock, and
	// a Kill in that window deletes the entry and snapshots a nil
	// sess to close — so assigning here would attach a live shell to
	// an entry nobody owns. watchSessionExit would then find no entry
	// and return, leaving the process running with its cwd inside a
	// worktree kill has already removed.
	if cur, ok := r.entries[id]; !ok || cur != e {
		r.mu.Unlock()
		_ = sess.Close()
		return ErrNotFound
	}
	e.sess = sess
	r.attachTitleHook(id, sess)
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
	wtPath := e.WorktreePath
	projectCwd := ""
	if p, ok := r.projects[e.ProjectID]; ok {
		projectCwd = p.Cwd
	}
	e.Phase = wire.PhaseRestarting
	r.broadcastLocked(wire.SessionEventUpdated, e.Info())
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

	r.mu.Lock()
	resumeID := ""
	if e, ok := r.entries[id]; ok {
		resumeID = e.AgentSessionID
	}
	r.mu.Unlock()

	// Resolve the cwd we expect the resume to spawn under, so
	// ResumeArgs (Claude) can stat the agent's session file at the
	// right project hash. Revive will redo this same promotion; we
	// only compute it here to drive the resume-argv decision.
	resumeCwd := projectCwd
	if wtPath != "" {
		if _, err := os.Stat(wtPath); err == nil {
			resumeCwd = wtPath
		}
	}

	var opts session.Options
	if def, ok := agent.Get(agent.ID(agentID)); ok {
		switch {
		case def.ResumeArgs != nil && resumeID != "":
			// Resume the specific conversation by the agent CLI's
			// session id. Disambiguates when multiple sessions share
			// a cwd. resumeID is the Hive entry id for Claude
			// (pre-pinned via --session-id) and the codex-generated
			// UUID captured post-spawn for Codex.
			opts.Cmd = def.ResumeArgs(resumeID, resumeCwd)
		case len(def.ResumeCmd) > 0:
			opts.Cmd = def.ResumeCmd
		default:
			opts.Cmd = def.Cmd
		}
	}
	// Pass the project cwd as the fallback. Revive promotes opts.Cwd to
	// wtPath when the worktree directory still exists; if the user removed
	// it out-of-band, Revive's self-heal clears the worktree fields but
	// leaves opts.Cwd alone — projectCwd is what session.Start should use.
	opts.Cwd = projectCwd

	// Back to ready BEFORE Revive: Revive broadcasts alive:true from
	// inside, and a client gating attach on ready would otherwise see
	// alive:true + phase:"restarting" with no later event to unstick
	// it. Clearing here means ready and alive:true ride the same event.
	r.setPhase(id, wire.PhaseReady)
	return r.Revive(id, opts)
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
	// Stop any post-spawn AgentSessionID capture that's still
	// polling — the session is gone, no point waiting for codex's
	// rollout file. The capture goroutine will return ctx.Canceled
	// and exit without persisting.
	if e.captureCancel != nil {
		e.captureCancel()
		e.captureCancel = nil
	}
	info := e.Info()
	r.mu.Unlock()
	r.broadcast(wire.SessionEventUpdated, info)
}

// Kill terminates the session and removes its entry from the registry.
// The on-disk metadata directory is also removed.
//
// A backing git worktree is removed ONLY when it is pristine — no
// uncommitted changes and no unpushed commits (worktree.Status.
// Pristine). A worktree holding work survives the session and stays
// visible in the worktree browser, where RemoveWorktree can delete it
// deliberately. force therefore skips the confirm, not the work check:
// it lets the session close, it never destroys the worktree.
//
// When force is false and the worktree has uncommitted changes,
// returns ErrWorktreeDirty without modifying any state. Callers can
// retry with force=true after confirming with the user.
func (r *Registry) Kill(id string, force bool) error {
	return r.kill(id, force, false)
}

// KillAndRemoveWorktree closes the session AND deletes its worktree,
// whether or not the worktree is pristine. This is the one path where
// closing a session is allowed to destroy work, and it exists because
// the user asked for exactly that in the close dialog — the ordinary
// Kill deliberately keeps anything holding work.
func (r *Registry) KillAndRemoveWorktree(id string, force bool) error {
	return r.kill(id, force, true)
}

func (r *Registry) kill(id string, force, removeWorktree bool) error {
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
	//
	// `git status` on a large worktree is slow enough to look like a
	// hang, so the client is told we're checking; a refusal puts the
	// entry back to ready so a cancelled confirm leaves no stuck tile.
	if wtPath != "" && !worktreeShared && !force {
		r.setPhase(id, wire.PhaseChecking)
		r.gitMu.Lock()
		dirty, _ := worktree.HasUncommitted(wtPath)
		r.gitMu.Unlock()
		if dirty {
			r.setPhase(id, wire.PhaseReady)
			return ErrWorktreeDirty
		}
	}
	// Announce the teardown while the entry still exists: after the
	// delete below there is nothing left for List() to return, so a
	// client connecting mid-kill would otherwise see nothing at all.
	r.setPhase(id, wire.PhaseClosing)

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
	// Removing an entry shifts every later one down a slot, so Order
	// has to follow — it IS the index into r.order, and both GUI
	// reorder paths hand that index straight back to moveInOrder. The
	// survivors are broadcast at the tail of this function so clients
	// don't keep the stale values and misplace the next move.
	r.reindexLocked()
	r.persistIndexLoggedLocked("kill")
	dir := filepath.Join(SessionsDir(r.stateDir), id)
	// Snapshot the PTY under the lock. Reading e.sess after the unlock
	// races the create tail, which binds it under r.mu.
	sess := e.sess
	r.mu.Unlock()

	// Order: PTY first (releases any FD/cwd handles into the
	// worktree), worktree second (now safe to git worktree remove),
	// metadata last (so a crash mid-cleanup leaves a recoverable
	// orphan that the next daemon-startup scan reclaims).
	if sess != nil {
		_ = sess.Close()
	}
	if wtPath != "" && !worktreeShared {
		r.disposeWorktree(id, projectCwd, wtPath, wtBranch, removeWorktree)
	}
	_ = os.RemoveAll(dir)
	r.broadcast(wire.SessionEventRemoved, e.Info())
	// Re-read the survivors HERE rather than snapshotting them back
	// under the lock above: the worktree teardown between the two can
	// take seconds, and anything that ran meanwhile — a create
	// splicing mid-list, a rename, a phase change — would be clobbered
	// by a stale SessionInfo. Duplicate .order on the client is the
	// exact failure this function exists to prevent.
	for _, info := range r.List() {
		r.broadcast(wire.SessionEventUpdated, info)
	}
	return nil
}

// disposeWorktree decides what happens to a killed session's worktree
// and carries it out. Extracted from kill, which was 164 lines: this
// is the half that can delete a directory on someone's disk, so it is
// worth reading — and testing — on its own.
//
// The caller has already established that this session was the last
// one living in wtPath. Every branch below is a refusal except two:
// an explicit remove-the-worktree request, and a worktree that holds
// nothing (no uncommitted changes, no unpushed commits). Closing a
// session must never be the thing that destroys work.
func (r *Registry) disposeWorktree(id, projectCwd, wtPath, wtBranch string, removeWorktree bool) {
	r.gitMu.Lock()
	defer r.gitMu.Unlock()
	root, err := worktree.Root(projectCwd)
	switch {
	case err != nil:
		log.Printf("registry: kill %s: project cwd %q is not (or no longer) a git repo; falling back to RemoveAll on %s", id, projectCwd, wtPath)
		_ = os.RemoveAll(wtPath)
	case !worktree.IsManaged(root, wtPath):
		// Second guard, independent of whatever set WorktreePath:
		// only ever delete a worktree hive owns. An entry pointing
		// at the user's own checkout — their main clone, or a
		// worktree they created themselves — is left strictly
		// alone. Teardown is the last place a bug upstream can
		// still cost someone their working directory, so it does
		// not trust the field it was handed.
		log.Printf("registry: kill %s: worktree %s is not a hive-managed worktree of %s; leaving it on disk", id, wtPath, root)
	default:
		// Prune only a pristine worktree. Anything holding
		// uncommitted changes or unpushed commits outlives the
		// session and shows up in the worktree browser, where
		// removing it is an explicit, confirmed act. Closing a
		// session must never be the thing that destroys work.
		st, ierr := worktree.Inspect(root, wtPath)
		switch {
		case removeWorktree:
			// Asked for explicitly: delete regardless of what it
			// holds. The confirmation that produced this named the
			// work at stake.
			if err := worktree.Cleanup(root, wtPath); err != nil {
				log.Printf("registry: worktree cleanup failed for %s: %v (branch=%s)", id, err, wtBranch)
			}
		case ierr != nil:
			log.Printf("registry: kill %s: cannot inspect worktree %s (%v); keeping it", id, wtPath, ierr)
		case !st.Pristine():
			log.Printf("registry: kill %s: keeping worktree %s (branch=%s, uncommitted=%v unpushed=%d unknown=%v)",
				id, wtPath, wtBranch, st.Uncommitted, st.Unpushed, st.Unknown)
		default:
			if err := worktree.Cleanup(root, wtPath); err != nil {
				log.Printf("registry: worktree cleanup failed for %s: %v (branch=%s)", id, err, wtBranch)
			}
		}
	}
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
		// Notify clients of every session, since reindexLocked
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

// moveInOrder relocates id within order to newOrder, clamping to
// [0, len(order)] after removal. No-op if id isn't present. Shared by
// Registry's session-order and project-order slices, which use
// identical reorder semantics.
func moveInOrder(order []string, id string, newOrder int) []string {
	cur := slices.Index(order, id)
	if cur < 0 {
		return order
	}
	order = slices.Delete(order, cur, cur+1)
	newOrder = min(max(newOrder, 0), len(order))
	return slices.Insert(order, newOrder, id)
}

func (r *Registry) moveLocked(id string, newOrder int) {
	if slices.Index(r.order, id) < 0 {
		return
	}
	r.order = moveInOrder(r.order, id, newOrder)
	r.reindexLocked()
}

// reindexLocked re-derives every entry's Order from its position in
// r.order. Call it after ANY mutation of r.order — insert, delete, or
// move. Order is not independent state: both GUI reorder paths
// (frontend lib/reorder.ts and app/sidebar.ts) convert a display
// position into a global index by reading a sibling's .order and
// handing it back as UpdateSessionReq.Order, which moveInOrder splices
// at positionally. The moment Order stops equalling the index, every
// move lands in the wrong slot (or clamps to the end), and an append
// can even hand out an Order another entry already holds, which makes
// the frontend's sort-by-order ambiguous.
//
// In-memory only. index.json's id slice is the persisted authority and
// is what load() re-derives Order from, so a stale Order in a
// session.json nobody rewrote is harmless.
func (r *Registry) reindexLocked() {
	for i, id := range r.order {
		if e := r.entries[id]; e != nil {
			e.Order = i
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
		AgentSessionID: e.AgentSessionID,
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
	return writeJSON(filepath.Join(SessionsDir(r.stateDir), "index.json"), idx)
}

// persistEntryLoggedLocked persists e, logging (rather than returning)
// any failure. For mutation paths that must proceed regardless: the
// in-memory registry stays authoritative, but a failed write means the
// on-disk copy is stale until the next successful persist, so the
// failure must at least leave a trace in the log.
func (r *Registry) persistEntryLoggedLocked(e *Entry, op string) {
	if err := r.persistEntryLocked(e); err != nil {
		log.Printf("registry: %s: persist session %s failed (on-disk metadata now stale): %v", op, e.ID, err)
	}
}

func (r *Registry) persistIndexLoggedLocked(op string) {
	if err := r.persistIndexLocked(); err != nil {
		log.Printf("registry: %s: persist session index failed (on-disk order now stale): %v", op, err)
	}
}

func (r *Registry) persistProjectLoggedLocked(p *Project, op string) {
	if err := r.persistProjectLocked(p); err != nil {
		log.Printf("registry: %s: persist project %s failed (on-disk metadata now stale): %v", op, p.ID, err)
	}
}

func (r *Registry) persistProjectIndexLoggedLocked(op string) {
	if err := r.persistProjectIndexLocked(); err != nil {
		log.Printf("registry: %s: persist project index failed (on-disk order now stale): %v", op, err)
	}
}
