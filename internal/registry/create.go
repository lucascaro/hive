package registry

import (
	"context"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

// createPlan carries values resolved by one step of Create into the
// next. Create's lock/unlock boundaries are load-bearing (a concurrent
// KillProject can invalidate projectID across one; the PTY fork and
// `git worktree add` must not run under r.mu), so every step below
// either takes r.mu for its whole body or never takes it at all.
type createPlan struct {
	id        string
	projectID string
	color     string
	cwd       string

	// adoptedPath/adoptedBranch are set when cwd already lives in a
	// worktree owned by a sibling session in the same project (e.g. ⌘P
	// duplicate): the new entry joins that worktree instead of creating
	// one.
	adoptedPath   string
	adoptedBranch string

	// wtPath/wtBranch are the worktree this session should end up in,
	// pre-resolved before naming and cleared if `git worktree add`
	// later fails.
	wtPath   string
	wtBranch string

	name string
	// nameFromBranch records whether name was derived from wtBranch. If
	// `git worktree add` later fails we rename to a random label so the
	// persisted name doesn't claim a worktree that doesn't exist.
	nameFromBranch bool
}

// Create adds a new session and starts it. Metadata persists before
// the session starts so a crash mid-Create still surfaces the entry.
//
// ctx bounds the git and post-spawn-capture work; it must be
// daemon-scoped, not per-connection (see
// startAgentSessionIDCaptureLocked).
func (r *Registry) Create(ctx context.Context, spec wire.CreateSpec) (*Entry, error) {
	e, p, err := r.beginCreate(spec)
	if err != nil {
		return nil, err
	}
	return e, r.finishCreate(ctx, e, spec, p)
}

// beginCreate is Create's synchronous prefix: it resolves the plan,
// registers and persists the entry, and broadcasts SESSION_EVENT(added)
// with PhaseStarting so the client can paint a tile immediately — long
// before the worktree and PTY exist.
//
// The whole prefix runs under createMu so concurrent creates (the
// daemon runs them off its read loop) can't interleave their order
// splicing and land in a surprising sidebar order.
func (r *Registry) beginCreate(spec wire.CreateSpec) (*Entry, createPlan, error) {
	r.createMu.Lock()
	defer r.createMu.Unlock()

	p := r.resolveCreateTarget(spec)
	r.planWorktreeAndName(spec, &p)

	e, err := r.insertEntry(spec, p)
	if err != nil {
		return nil, p, err
	}
	r.mu.Lock()
	e.Phase = wire.PhaseStarting
	info := e.Info()
	r.broadcastLocked(wire.SessionEventAdded, info)
	r.mu.Unlock()
	return e, p, nil
}

// finishCreate is Create's slow tail: `git worktree add` and the PTY
// fork, neither of which may run under r.mu. It reports progress via
// setPhase and ends on PhaseReady — the edge at which the session
// becomes attachable. The daemon runs this off the control read loop
// (see internal/daemon), so a slow git can no longer stall every other
// client request.
func (r *Registry) finishCreate(ctx context.Context, e *Entry, spec wire.CreateSpec, p createPlan) error {
	cmd := resolveAgentCmd(spec, p.id)
	r.materializeWorktree(ctx, &p)
	if p.nameFromBranch && p.wtBranch == "" {
		r.renameAfterWorktreeFailure(e, spec)
	}

	r.setPhase(p.id, wire.PhaseSpawning)
	sess, err := spawn(session.Options{
		Shell: spec.Shell,
		Cmd:   cmd,
		Cwd:   p.cwd,
		Cols:  spec.Cols,
		Rows:  spec.Rows,
	})
	if err != nil {
		log.Printf("registry: session.Start failed for %s (agent=%q cmd=%v): %v",
			e.ID, spec.Agent, cmd, err)
		r.mu.Lock()
		if _, ok := r.entries[p.id]; !ok {
			// Killed while the spawn was failing. Same tombstone rule
			// as attachSession below: broadcasting here would emit an
			// `updated` for an id the clients already saw `removed`,
			// which a client tracking liveness reads as a session
			// dying (the GUI pops a "Session ended" notification for a
			// session the user just closed). The worktree is ours to
			// clean up too — the entry never carried its path, so Kill
			// could not have removed it.
			r.mu.Unlock()
			log.Printf("registry: create %s: entry removed mid-create; discarding the failed session", p.id)
			r.discardWorktree(p)
			return ErrNotFound
		}
		// Strand the metadata as a dead entry. The user can recreate
		// or kill it. Store the error so the GUI can surface it. The
		// event is `updated`, not `added` — beginCreate already
		// announced this entry.
		e.LastError = err.Error()
		e.Phase = wire.PhaseReady
		info := e.Info()
		r.broadcastLocked(wire.SessionEventUpdated, info)
		r.mu.Unlock()
		return err
	}

	// Snapshot under the lock: attachSession starts the capture
	// goroutine, which mutates AgentSessionID on this same entry.
	info, live := r.attachSession(ctx, e, sess, spec, p)
	if !live {
		// Killed mid-create. The kill saw e.sess == nil and could not
		// close this PTY, and the entry carried no worktree path yet,
		// so cleaning both up is our job. Best-effort: a worktree that
		// survives is reclaimed by ReclaimOrphanWorktrees on the next
		// daemon start.
		log.Printf("registry: create %s: entry removed mid-create; discarding the spawned session", p.id)
		_ = sess.Close()
		r.discardWorktree(p)
		return ErrNotFound
	}
	r.broadcast(wire.SessionEventUpdated, info)
	go r.watchSessionExit(p.id, sess)
	return nil
}

// discardWorktree removes a worktree that finishCreate materialized
// for an entry that no longer exists. Never runs for an adopted
// worktree — that directory belongs to a sibling session.
func (r *Registry) discardWorktree(p createPlan) {
	if p.wtPath == "" || p.adoptedPath != "" {
		return
	}
	root, err := worktree.Root(p.wtPath)
	if err != nil {
		return
	}
	r.gitMu.Lock()
	defer r.gitMu.Unlock()
	if err := worktree.Cleanup(root, p.wtPath); err != nil {
		log.Printf("registry: discarding worktree %s after mid-create kill: %v", p.wtPath, err)
	}
}

// resolveCreateTarget picks the id, owning project, color and cwd for
// a new session, and detects an adoptable sibling worktree. Takes r.mu.
func (r *Registry) resolveCreateTarget(spec wire.CreateSpec) createPlan {
	r.mu.Lock()
	defer r.mu.Unlock()

	p := createPlan{id: uuid.NewString()}
	// Resolve owning project first so we can avoid its color when
	// auto-picking the session color (otherwise the gradient could
	// collapse to a flat hue).
	p.projectID = spec.ProjectID
	if p.projectID == "" {
		p.projectID = r.defaultProjectIDLocked()
	}
	var projectColor string
	if proj, ok := r.projects[p.projectID]; ok {
		projectColor = proj.Color
	}
	// Color is reserved for project/session identity; agent identity
	// is conveyed by the badge/icon. So skip the agent-default tier
	// and pick a random palette color when the caller didn't choose.
	p.color = spec.Color
	if p.color == "" {
		p.color = pickColor(r.lastSessionColor, projectColor)
		r.lastSessionColor = p.color
	}
	// Resolve cwd up front (under the same lock) so we can decide on a
	// worktree branch BEFORE naming the session — the session name is
	// derived from the worktree branch when one is in play, so the
	// user can find the worktree directory from the session label.
	p.cwd = spec.Cwd
	if p.cwd == "" {
		if proj, ok := r.projects[p.projectID]; ok && proj.Cwd != "" {
			p.cwd = proj.Cwd
		}
	}
	// An explicit worktree path is the "resume this work" path from
	// the worktree browser: run in a worktree that already exists on
	// disk, whether or not any session currently occupies it. It wins
	// over cwd, and it turns off worktree creation.
	if spec.WorktreePath != "" {
		p.cwd = spec.WorktreePath
	}
	// Detect when cwd already lives in a worktree owned by another
	// session in the same project (e.g. ⌘P duplicate). The new entry
	// adopts that worktree's path+branch so the sidebar shows the
	// worktree badge and Kill can keep the worktree alive until the
	// last session in it goes away.
	if !spec.UseWorktree && p.cwd != "" {
		for _, other := range r.entries {
			if other.ProjectID == p.projectID && other.WorktreePath != "" && other.WorktreePath == p.cwd {
				p.adoptedPath = other.WorktreePath
				p.adoptedBranch = other.WorktreeBranch
				break
			}
		}
	}
	return p
}

// adoptDetachedWorktree claims a worktree that exists on disk but has
// no session in it — the case resolveCreateTarget's sibling scan can't
// see. Without this the entry's WorktreePath stays empty, the worktree
// looks unclaimed to ReclaimOrphanWorktrees, and the work the user
// just resumed is deleted at the next daemon start.
//
// Does not take r.mu (it shells out to git); called from
// planWorktreeAndName, which also runs lock-free.
func (r *Registry) adoptDetachedWorktree(p *createPlan) {
	if p.adoptedPath != "" || p.cwd == "" || !worktree.IsGitRepo(p.cwd) {
		return
	}
	// MainRoot, not Root: p.cwd is typically a linked worktree here,
	// and Root would report that worktree's own top level — which
	// would then look like the main checkout and be skipped below.
	root, err := worktree.MainRoot(p.cwd)
	if err != nil {
		return
	}
	r.gitMu.Lock()
	trees, lerr := worktree.List(root)
	r.gitMu.Unlock()
	if lerr != nil {
		log.Printf("registry: worktree.List while adopting %s: %v", p.cwd, lerr)
		return
	}
	cwd := worktree.ResolvePath(p.cwd)
	for _, t := range trees {
		// The repo root is a worktree too, but a session in the main
		// checkout is a plain session, not a worktree-backed one.
		if t.Path != cwd || t.Path == worktree.ResolvePath(root) {
			continue
		}
		p.adoptedPath, p.adoptedBranch = t.Path, t.Branch
		return
	}
}

// planWorktreeAndName pre-resolves the worktree branch+path so the
// session name can match the worktree directory, then picks the name.
// ResolveBranchAndPath only picks a free name; the actual `git worktree
// add` happens in materializeWorktree. Does not take r.mu.
func (r *Registry) planWorktreeAndName(spec wire.CreateSpec, p *createPlan) {
	if !spec.UseWorktree {
		r.adoptDetachedWorktree(p)
	}
	if p.adoptedPath != "" {
		p.wtPath, p.wtBranch = p.adoptedPath, p.adoptedBranch
	}
	if spec.UseWorktree && p.cwd != "" && worktree.IsGitRepo(p.cwd) {
		if root, err := worktree.Root(p.cwd); err == nil {
			if b, path, rerr := worktree.ResolveBranchAndPath(root, spec.Branch); rerr == nil {
				p.wtBranch, p.wtPath = b, path
			} else {
				log.Printf("registry: worktree.ResolveBranchAndPath: %v", rerr)
			}
		}
	}

	p.name = spec.Name
	if p.name != "" {
		return
	}
	if p.wtBranch == "" {
		p.name = agent.RandomName(agent.ID(spec.Agent))
		return
	}
	// Tie the session name to the worktree directory so the user can
	// find the worktree from the session label. Slashes (e.g.
	// `feature/foo`) get folded to `-` so the name is safe to use in
	// paths and shell-quoted contexts.
	suffix := spec.Agent
	if suffix == "" {
		suffix = "shell"
	}
	p.name = strings.ReplaceAll(p.wtBranch, "/", "-") + " " + suffix
	p.nameFromBranch = true
}

// insertEntry registers the entry and persists it before the session
// starts, rolling both back if either write fails. Takes r.mu.
func (r *Registry) insertEntry(spec wire.CreateSpec, p createPlan) (*Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Re-validate projectID after the unlock window above: a concurrent
	// KillProject could have removed it. Fall back to the default
	// project rather than persisting a dangling reference.
	projectID := p.projectID
	if _, ok := r.projects[projectID]; !ok {
		projectID = r.defaultProjectIDLocked()
	}
	e := &Entry{
		ID: p.id, Name: p.name, Color: p.color,
		Created: time.Now().UTC(),
		Agent:   spec.Agent, ProjectID: projectID,
	}
	r.entries[p.id] = e
	// Place the new session right after its anchor when the anchor is a
	// live sibling in the same project; otherwise append. r.order is one
	// global list across all projects, so a cross-project anchor would
	// land the entry inside another project's index range.
	pos := len(r.order)
	if a := r.entries[spec.InsertAfterSessionID]; a != nil && a.ProjectID == projectID {
		if i := slices.Index(r.order, a.ID); i >= 0 {
			pos = i + 1
		}
	}
	r.order = slices.Insert(r.order, pos, p.id)
	if pos == len(r.order)-1 {
		// Plain append: nobody else's Order moved, so skip renumbering —
		// renumberLocked re-persists every entry, and doing that under
		// r.mu on every session create is O(n) disk writes for nothing.
		e.Order = pos
	} else {
		r.renumberLocked()
	}
	rollback := func() {
		delete(r.entries, p.id)
		r.order = slices.Delete(r.order, pos, pos+1)
		r.renumberLocked()
	}
	if err := r.persistEntryLocked(e); err != nil {
		rollback()
		return nil, err
	}
	if err := r.persistIndexLocked(); err != nil {
		rollback()
		return nil, err
	}
	// A mid-list splice shifted every later entry's Order, so the
	// clients' cached values are stale — same fan-out Update does. A
	// plain append shifts nothing and stays quiet.
	if pos != len(r.order)-1 {
		for _, sid := range r.order {
			if other := r.entries[sid]; other != nil && other.ID != e.ID {
				r.broadcastLocked(wire.SessionEventUpdated, other.Info())
			}
		}
	}
	return e, nil
}

// resolveAgentCmd returns the argv to spawn. If the spec names an
// agent (and no explicit Cmd), look up its default command and use it.
func resolveAgentCmd(spec wire.CreateSpec, id string) []string {
	cmd := spec.Cmd
	if len(cmd) > 0 || spec.Agent == "" {
		return cmd
	}
	def, ok := agent.Get(agent.ID(spec.Agent))
	if !ok || len(def.Cmd) == 0 {
		return cmd
	}
	cmd = def.Cmd
	// Pin the agent's conversation to our entry id so Restart can
	// resume by id even when sibling sessions share this cwd. Skipped
	// when the caller passed an explicit spec.Cmd (we don't mutate
	// user-supplied argv).
	if def.SessionIDFlag != "" {
		cmd = append(append([]string(nil), cmd...), def.SessionIDFlag, id)
	}
	return cmd
}

// materializeWorktree runs the heavy `git worktree add` and promotes
// the plan's cwd into the new worktree. Failure is non-fatal — the
// session falls back to the plain project cwd. Aborting create on
// worktree failure would block users on marginal repos (shallow
// clones, sandbox restrictions, slow filesystems). Does not take r.mu.
func (r *Registry) materializeWorktree(ctx context.Context, p *createPlan) {
	// Skip the create when we're adopting an existing worktree from a
	// sibling session — the directory is already on disk and `git
	// worktree add` would fail.
	if p.wtBranch == "" || p.adoptedPath != "" {
		return
	}
	root, err := worktree.Root(p.cwd)
	if err != nil {
		p.wtPath, p.wtBranch = "", ""
		return
	}
	// gitMu serializes the worktree subprocesses across concurrent
	// creates/kills. Taken before setPhase so the phase the client
	// sees is "we are actually working", not "we are queued" — and
	// never while holding r.mu (see Registry.gitMu).
	r.gitMu.Lock()
	defer r.gitMu.Unlock()
	r.setPhase(p.id, wire.PhaseFetching)
	if cerr := worktree.CreateWorktree(ctx, root, p.wtBranch, p.wtPath); cerr != nil {
		log.Printf("registry: worktree create failed (falling back to plain session): %v", cerr)
		p.wtPath, p.wtBranch = "", ""
		return
	}
	p.cwd = p.wtPath
	r.setPhase(p.id, wire.PhaseWorktree)
	worktree.EnsureGitignore(root)
	worktree.LinkAgentConfig(root, p.wtPath)
	log.Printf("registry: created worktree %s on branch %s", p.wtPath, p.wtBranch)
}

// renameAfterWorktreeFailure relabels an entry whose name was derived
// from a worktree branch that failed to materialize — the persisted
// name would otherwise lie about reality ("feature-foo claude" with no
// worktree). Takes r.mu.
func (r *Registry) renameAfterWorktreeFailure(e *Entry, spec wire.CreateSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// The entry can have been killed while the worktree attempt ran.
	// Persisting now would write its metadata back out after Kill
	// removed it, resurrecting a ghost session on the next daemon boot.
	if _, ok := r.entries[e.ID]; !ok {
		return
	}
	e.Name = agent.RandomName(agent.ID(spec.Agent))
	r.persistEntryLoggedLocked(e, "create (rename fallback)")
	r.broadcastLocked(wire.SessionEventUpdated, e.Info())
}

// attachSession binds the freshly spawned PTY to the entry, records
// the worktree and agent-session ids, kicks off the post-spawn
// capture, moves the entry to PhaseReady, and returns the info
// snapshot to broadcast. Takes r.mu.
//
// Returns live=false when the entry was killed while the tail was
// running. The check has to happen inside this critical section, not
// before it: Kill deletes the entry and reads e.sess to close the PTY,
// so a check that released r.mu before binding e.sess would let a kill
// slip through the gap and leak the process.
func (r *Registry) attachSession(ctx context.Context, e *Entry, sess *session.Session, spec wire.CreateSpec, p createPlan) (wire.SessionInfo, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.entries[p.id]; !ok {
		return wire.SessionInfo{}, false
	}

	// The session.Session uses its own UUID; we override with the
	// registry id so the registry id is the public identity.
	sess.ID = p.id
	e.sess = sess
	if p.wtPath != "" {
		e.WorktreePath = p.wtPath
		e.WorktreeBranch = p.wtBranch
	}
	// Pin the agent session id when the agent's first-launch flag let
	// us choose it (Claude). Persist alongside the worktree fields
	// above so the entry on disk matches what we just spawned. Skip
	// when the caller passed an explicit spec.Cmd — we never injected
	// SessionIDFlag in that branch (we don't mutate user-supplied
	// argv), so the agent did NOT record its conversation under our
	// id. Pretending otherwise would make Restart resume the wrong
	// conversation (or fail to find one).
	if len(spec.Cmd) == 0 {
		if def, ok := agent.Get(agent.ID(spec.Agent)); ok && def.SessionIDFlag != "" {
			e.AgentSessionID = p.id
		}
	}
	if p.wtPath != "" || e.AgentSessionID != "" {
		r.persistEntryLoggedLocked(e, "create")
	}
	// Kick off the post-spawn capture for agents that don't support
	// caller-chosen ids (Codex). The cancel func is stored so
	// watchSessionExit can stop the poll if the session dies first.
	r.startAgentSessionIDCaptureLocked(ctx, e, p.cwd)
	e.Phase = wire.PhaseReady
	return e.Info(), true
}

// startAgentSessionIDCaptureLocked launches the per-agent capture
// goroutine when the agent's Def opts into post-spawn id capture.
// Caller must hold r.mu so the cancel func is wired before
// watchSessionExit can race with it.
//
// ctx must be daemon-scoped: this goroutine outlives Create, so a
// per-connection ctx would silently kill Codex id capture whenever a
// create-mode client disconnects within the timeout.
func (r *Registry) startAgentSessionIDCaptureLocked(ctx context.Context, e *Entry, cwd string) {
	def, ok := agent.Get(agent.ID(e.Agent))
	if !ok || def.CaptureSessionIDFn == nil {
		return
	}
	// 30s is a generous upper bound. Codex writes the rollout file
	// well within a second of spawn in practice; the long tail is
	// only for sandboxed/cold-start scenarios.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	e.captureCancel = cancel
	id := e.ID
	go func() {
		defer cancel()
		captured, err := def.CaptureSessionIDFn(ctx, cwd, time.Now())
		if err != nil || captured == "" {
			return
		}
		r.mu.Lock()
		e2, ok := r.entries[id]
		if !ok {
			r.mu.Unlock()
			return
		}
		// If the session already exited and was reaped, or another
		// path has already populated AgentSessionID, leave it alone.
		if e2.AgentSessionID != "" {
			r.mu.Unlock()
			return
		}
		e2.AgentSessionID = captured
		r.persistEntryLoggedLocked(e2, "agent-session-id capture")
		info := e2.Info()
		r.mu.Unlock()
		r.broadcast(wire.SessionEventUpdated, info)
	}()
}
