package registry

import (
	"context"
	"log"
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
// daemon-scoped, not per-connection (see attachSessionLocked).
func (r *Registry) Create(ctx context.Context, spec wire.CreateSpec) (*Entry, error) {
	p := r.resolveCreateTarget(spec)
	r.planWorktreeAndName(spec, &p)

	e, err := r.insertEntry(spec, p)
	if err != nil {
		return nil, err
	}

	// Everything below runs outside the lock so neither `git worktree
	// add` nor the PTY fork blocks the registry.
	cmd := resolveAgentCmd(spec, p.id)
	r.materializeWorktree(ctx, &p)
	if p.nameFromBranch && p.wtBranch == "" {
		r.renameAfterWorktreeFailure(e, spec)
	}

	sess, err := startSession(session.Options{
		Shell: spec.Shell,
		Cmd:   cmd,
		Cwd:   p.cwd,
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

	r.attachSession(ctx, e, sess, spec, p)
	r.broadcast(wire.SessionEventAdded, e.Info())
	go r.watchSessionExit(p.id, sess)
	return e, nil
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

// planWorktreeAndName pre-resolves the worktree branch+path so the
// session name can match the worktree directory, then picks the name.
// ResolveBranchAndPath only picks a free name; the actual `git worktree
// add` happens in materializeWorktree. Does not take r.mu.
func (r *Registry) planWorktreeAndName(spec wire.CreateSpec, p *createPlan) {
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
		Order: len(r.order), Created: time.Now().UTC(),
		Agent: spec.Agent, ProjectID: projectID,
	}
	r.entries[p.id] = e
	r.order = append(r.order, p.id)
	rollback := func() {
		delete(r.entries, p.id)
		r.order = r.order[:len(r.order)-1]
	}
	if err := r.persistEntryLocked(e); err != nil {
		rollback()
		return nil, err
	}
	if err := r.persistIndexLocked(); err != nil {
		rollback()
		return nil, err
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
	if cerr := worktree.CreateWorktree(ctx, root, p.wtBranch, p.wtPath); cerr != nil {
		log.Printf("registry: worktree create failed (falling back to plain session): %v", cerr)
		p.wtPath, p.wtBranch = "", ""
		return
	}
	p.cwd = p.wtPath
	worktree.EnsureGitignore(root)
	log.Printf("registry: created worktree %s on branch %s", p.wtPath, p.wtBranch)
}

// renameAfterWorktreeFailure relabels an entry whose name was derived
// from a worktree branch that failed to materialize — the persisted
// name would otherwise lie about reality ("feature-foo claude" with no
// worktree). Takes r.mu.
func (r *Registry) renameAfterWorktreeFailure(e *Entry, spec wire.CreateSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e.Name = agent.RandomName(agent.ID(spec.Agent))
	r.persistEntryLoggedLocked(e, "create (rename fallback)")
}

// attachSession binds the freshly spawned PTY to the entry, records
// the worktree and agent-session ids, and kicks off the post-spawn
// capture. Takes r.mu.
func (r *Registry) attachSession(ctx context.Context, e *Entry, sess *session.Session, spec wire.CreateSpec, p createPlan) {
	r.mu.Lock()
	defer r.mu.Unlock()

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
