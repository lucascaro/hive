package registry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

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

	// projectCwd is the owning project's own working directory. Kept
	// apart from cwd because a session may run elsewhere, and because
	// adoption must never claim it (see adoptDetachedWorktree).
	projectCwd string

	// wtPath/wtBranch are the worktree this session should end up in,
	// pre-resolved before naming and cleared if `git worktree add`
	// later fails.
	wtPath   string
	wtBranch string
	// wtCreated records that THIS plan's `git worktree add` actually
	// created wtPath: set when nothing occupied that path before the
	// add, whether the add then succeeded or left debris behind. Only
	// then may discardWorktree delete it.
	//
	// A planned-but-not-created path is not owned: a parked create
	// holds one for as long as the user takes to answer, and
	// ResolveBranchAndPath only avoids paths that exist ON DISK, so a
	// later session can resolve to the same path and materialize it
	// for real. Deleting on the strength of the plan alone would then
	// destroy that other session's worktree, uncommitted work and all.
	wtCreated bool

	// wtPlanErr records why a REQUESTED worktree could not even be
	// planned (branch/path resolution failed inside a real git repo).
	// Empty when none was requested, or when planning succeeded.
	// Carried so materializeWorktree can park on it instead of quietly
	// handing back a plain session in the project directory.
	wtPlanErr string

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
	// Both in-memory only (see Entry): a daemon restart between here
	// and delivery loses the prompt, which is exactly why the idea is
	// not flipped to `started` until the prompt has actually landed.
	e.ideaID = spec.IdeaID
	if deliveryFor(spec) == promptTyped {
		// May come back empty if the note was nothing but control
		// characters. That is NOT the prompt-less case: a prompt was
		// requested and nothing can be handed over, so finishCreate
		// leaves the idea in the inbox rather than claiming it.
		e.pendingPrompt = typedPrompt(spec.InitialPrompt)
	}
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
	parked, err := r.materializeWorktree(ctx, e, spec, &p)
	if err != nil {
		// Nothing can answer the question, so there is no session to
		// have. materializeWorktree has already removed the entry.
		return err
	}
	if parked {
		// The user is being asked what to do. finishCreateTail runs
		// from ResolveWorktreeChoice instead, whenever they answer —
		// this goroutine returns holding nothing.
		return nil
	}
	return r.finishCreateTail(ctx, e, spec, p)
}

// finishCreateTail is everything after worktree setup: the rename
// fallback, the PTY fork, and the attach. Split out of finishCreate so
// a create parked on a user decision can be resumed from
// ResolveWorktreeChoice without duplicating it.
func (r *Registry) finishCreateTail(ctx context.Context, e *Entry, spec wire.CreateSpec, p createPlan) error {
	cmd := r.resolveAgentCmd(spec, p.id)
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
		Env:   append(r.hiveEnv(p.id), r.resolveAgentEnv(spec)...),
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
		// No process ever existed, so there is nothing to paste into
		// and never will be. Left set, the offer would render on a dead
		// tile and refuse every click with ErrNoLiveSession.
		e.pendingPrompt = ""
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
	// The idea is claimed only when the work was actually handed over.
	// Three cases, and only the first two are that:
	//
	//   - no prompt was asked for at all: an ordinary Start session,
	//     nothing to deliver, so the link is immediate;
	//   - argv: the text is in the process's own command line, so it is
	//     delivered the moment the process exists;
	//   - typed: waiting for the user to paste it (ResolvePrompt),
	//     which is where that idea is claimed instead.
	//
	// Everything else is a prompt that was REQUESTED and cannot be
	// delivered — the shell agent, a custom agent, a note that
	// sanitized away to nothing. Those must not claim the idea: no
	// work was handed over, so the note stays in the inbox where the
	// user can start it again against an agent that can receive it.
	// (An earlier revision linked here, which marked a note as started
	// for a session that never got it.)
	r.mu.Lock()
	waiting := e.pendingPrompt != ""
	r.mu.Unlock()
	if !waiting && handedOverAtCreate(spec) {
		r.linkIdeaToSession(spec.IdeaID, p.id)
	}
	go r.watchSessionExit(p.id, sess)
	return nil
}

// handedOverAtCreate reports whether the session already has whatever
// prompt it was going to get by the time it is running — so the idea it
// came from can be claimed immediately.
//
// True in exactly two cases: nothing was asked for, or the argv path
// put real text on the command line. The typed path is false here and
// is claimed later, by ResolvePrompt. Everything else — the
// shell agent, a custom agent, a prompt that sanitizes away — is a
// prompt that was REQUESTED and cannot be delivered, and must not claim
// the note.
//
// A function rather than an expression inline because the argv arm was
// otherwise unreachable from any test: exercising it for real means
// spawning claude or pi.
func handedOverAtCreate(spec wire.CreateSpec) bool {
	if spec.InitialPrompt == "" {
		return true
	}
	// Both forms have to be non-empty, and they are not the same
	// function: a whitespace-only note survives sanitizing but collapses
	// on the typed path, and on Windows a note of nothing but `%` is
	// emptied by argvPrompt alone. Whatever the reason, if the argv
	// actually appended nothing then nothing was handed over.
	return deliveryFor(spec) == promptArgv &&
		argvPrompt(runtime.GOOS, spec.InitialPrompt) != "" &&
		typedPrompt(spec.InitialPrompt) != ""
}

// linkIdeaToSession flips the idea a session was started from to
// `started` and records which session serves it. No-op without an
// idea. Must NOT be called with r.mu held — UpdateIdea takes it.
//
// The entry is re-checked because every caller reaches here off the
// lock, and a Kill can land in between: linking then would leave the
// idea pointing at a session id no client can resolve, which renders
// as an inbox row saying "in <gone>" with no way back to open.
func (r *Registry) linkIdeaToSession(ideaID, sessionID string) {
	if ideaID == "" {
		return
	}
	r.mu.Lock()
	e, live := r.entries[sessionID]
	var sessionProject string
	if live {
		sessionProject = e.ProjectID
	}
	ideaProject := ""
	if f, ok := r.ideas[ideaID]; ok {
		ideaProject = f.ProjectID
	}
	r.mu.Unlock()
	if !live {
		log.Printf("registry: not linking idea %s: session %s is already gone", ideaID, sessionID)
		return
	}
	// An idea belongs to a project and the launcher pins that project,
	// so a mismatch means the client sent an idea_id that does not go
	// with this session. Refuse rather than file the link: it would put
	// one project's idea into another project's session, and the wire
	// field is reachable by any client.
	if ideaProject != "" && sessionProject != ideaProject {
		log.Printf("registry: not linking idea %s (project %s) to session %s (project %s): different projects",
			ideaID, ideaProject, sessionID, sessionProject)
		return
	}
	started := wire.IdeaStatusStarted
	if _, err := r.UpdateIdea(wire.UpdateIdeaReq{
		ID: ideaID, Status: &started, SessionID: &sessionID,
	}); err != nil {
		// Not fatal to the session: the user has a session with the
		// right prompt in it, and an idea still sitting in the inbox.
		log.Printf("registry: linking idea %s to session %s: %v", ideaID, sessionID, err)
	}
}

// discardWorktree removes a worktree THIS plan is responsible for, for
// an entry that no longer exists. Never runs for an adopted worktree —
// that directory belongs to a sibling session.
//
// Two ownership checks, because this is `git worktree remove --force`
// plus os.RemoveAll and a wrong call destroys uncommitted work:
//
//   - wtCreated: nothing may have occupied the path before this plan's
//     add. Parking made that distinction matter — a parked create
//     holds a planned path indefinitely while a later session can
//     legitimately resolve to the same path and create it for real.
//     Note it is set for a FAILED add too, when the path was free
//     beforehand: git is SIGKILLed on its deadline and leaves debris
//     that only this plan can be responsible for.
//   - no live entry may be living there. Mirrors the sibling check the
//     kill path already makes before it cleans up a worktree.
func (r *Registry) discardWorktree(p createPlan) {
	if p.wtPath == "" || p.adoptedPath != "" || !p.wtCreated {
		return
	}
	r.mu.Lock()
	for _, other := range r.entries {
		if other.WorktreePath == p.wtPath {
			r.mu.Unlock()
			log.Printf("registry: not discarding worktree %s: session %s lives there", p.wtPath, other.ID)
			return
		}
	}
	r.mu.Unlock()
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
		p.projectCwd = proj.Cwd
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
		// r.entries is a map, so "the first match" is whatever iteration
		// order hands back. Pick the lowest-Order occupant instead: it is
		// the group's anchor in the sidebar, and once a user recolours one
		// member the members disagree, at which point map order would make
		// the inherited colour differ run to run.
		var adopt *Entry
		for _, other := range r.entries {
			if other.ProjectID != p.projectID || other.WorktreePath == "" || other.WorktreePath != p.cwd {
				continue
			}
			if adopt == nil || other.Order < adopt.Order {
				adopt = other
			}
		}
		if adopt != nil {
			p.adoptedPath = adopt.WorktreePath
			p.adoptedBranch = adopt.WorktreeBranch
			// Sessions sharing one worktree share one colour: that is
			// the sidebar's link between them (spec 384). This reads
			// the "color is session identity" rule above as identity
			// of the WORK, not of the process — two sessions editing
			// the same files are one piece of work. An explicit
			// spec.Color still wins, and the inherited colour becomes
			// lastSessionColor so the next freshly-picked session
			// steers away from it rather than colliding with the
			// group.
			if spec.Color == "" && adopt.Color != "" {
				p.color = adopt.Color
				r.lastSessionColor = p.color
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
		if t.Path != cwd {
			continue
		}
		// Adoption means "Kill deletes this when the last session
		// leaves", so it is only ever correct for a worktree hive made
		// to hold sessions. Two things are therefore never adopted:
		//
		//  - anything outside <mainRoot>/.worktrees/ — the user's own
		//    checkout, or a worktree they created themselves;
		//  - the project's own working directory, even when that sits
		//    under .worktrees/. Running hived from inside a linked
		//    worktree makes project.Cwd that worktree, so without this
		//    every plain session would claim it and closing the last
		//    one would delete the directory the user works in.
		if !worktree.IsManaged(root, t.Path) {
			return
		}
		if p.projectCwd != "" &&
			worktree.ResolvePath(p.projectCwd) == t.Path {
			return
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
		// A worktree was asked for in a real git repo, so failing to
		// plan one is a failure to report, not a silent downgrade to a
		// plain session — same rule as the fetch and the add (#451).
		root, err := worktree.Root(p.cwd)
		if err != nil {
			p.wtPlanErr = err.Error()
			log.Printf("registry: worktree.Root: %v", err)
		} else if b, path, rerr := worktree.ResolveBranchAndPath(root, spec.Branch); rerr == nil {
			p.wtBranch, p.wtPath = b, path
		} else {
			p.wtPlanErr = rerr.Error()
			log.Printf("registry: worktree.ResolveBranchAndPath: %v", rerr)
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
	//
	// The agent id is deliberately NOT appended (see agent.RandomName):
	// the surfaces that show a name show the agent too. Two agents on one
	// branch therefore share a name — which is what the GUI's worktree
	// group panel is for: it states the branch once and leads each row
	// with what that session is actually doing.
	p.name = strings.ReplaceAll(p.wtBranch, "/", "-")
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
	r.reindexLocked()
	rollback := func() {
		delete(r.entries, p.id)
		r.order = slices.Delete(r.order, pos, pos+1)
		r.reindexLocked()
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
//
// SpawnArgs (the hook-tier wiring) is appended only in that same
// branch — an explicit spec.Cmd is the "raw Cmd from a client that
// doesn't speak agent IDs" case, and we don't mutate user-supplied
// argv there any more than we inject SessionIDFlag into it.
// agentDef is the built-in or custom agent whose OWN command a create
// will run, or ok=false when it runs an explicit spec.Cmd or no agent.
//
// It is the single gate for everything an agent adapter adds at spawn
// — argv (SpawnArgs) in resolveAgentCmd, environment (SpawnEnv) in
// resolveAgentEnv. Both must agree: an explicit Cmd gets no hooks, so it
// must not get the environment that only pays off through those hooks
// either.
func agentDef(spec wire.CreateSpec) (agent.Def, bool) {
	if len(spec.Cmd) > 0 || spec.Agent == "" {
		return agent.Def{}, false
	}
	def, ok := agent.Get(agent.ID(spec.Agent))
	if !ok || len(def.Cmd) == 0 {
		return agent.Def{}, false
	}
	return def, true
}

// resolveAgentEnv is the agent adapter's extra environment for a
// create, gated exactly like resolveAgentCmd's SpawnArgs.
func (r *Registry) resolveAgentEnv(spec wire.CreateSpec) []string {
	def, ok := agentDef(spec)
	if !ok || def.SpawnEnv == nil {
		return nil
	}
	return def.SpawnEnv(r.spawnInfo())
}

func (r *Registry) resolveAgentCmd(spec wire.CreateSpec, id string) []string {
	def, ok := agentDef(spec)
	if !ok {
		return spec.Cmd
	}
	cmd := def.Cmd
	// Carrying on in a worktree the user already worked in: use the
	// agent's path-scoped resume. It continues the most recent
	// conversation for this directory, which is the only handle we
	// have — the previous session's id died with its entry. Falls back
	// to a fresh launch for agents that define no resume argv.
	if spec.ContinueConversation && len(def.ResumeCmd) > 0 {
		cmd = def.ResumeCmd
	} else if def.SessionIDFlag != "" {
		// Pin the agent's conversation to our entry id so Restart can
		// resume by id even when sibling sessions share this cwd.
		cmd = append(append([]string(nil), cmd...), def.SessionIDFlag, id)
	}
	if def.SpawnArgs != nil {
		if extra := def.SpawnArgs(r.spawnInfo()); len(extra) > 0 {
			cmd = append(append([]string(nil), cmd...), extra...)
		}
	}
	// The opening prompt, last, as a bare positional. Quoted nothing:
	// this is argv, not a shell string. Deliberately here and NOT in
	// appendSpawnArgs — a restart or revive rebuilds argv from the same
	// helper, and re-sending the opening prompt would replay the first
	// turn every time the user restarted the session.
	if deliveryFor(spec) == promptArgv {
		if p := argvPrompt(runtime.GOOS, spec.InitialPrompt); p != "" {
			// "--" first: without it a prompt beginning with "-" is
			// parsed as a flag, and CreateSpec.InitialPrompt is a wire
			// field — our own prompts start with a word, but nothing
			// makes another client's do so. Verified against both
			// users before adding: `claude --print -- "…"` answers
			// normally, and `pi --help` documents `[--]` as "End option
			// parsing; treat remaining arguments as messages/files".
			cmd = append(append([]string(nil), cmd...), "--", p)
		}
	}
	return cmd
}

// maxPromptBytes bounds a delivered opening prompt. An idea's text is
// capped at wire.MaxIdeaText; the prompt built from it is that text
// plus ideaPrompt()'s preamble, so this has to be the larger of the
// two or a maximum-length note loses its ending.
const maxPromptBytes = wire.MaxIdeaText + 1024

// promptControlChars strips the C0 control characters (and DEL) that an
// opening prompt has no business carrying, leaving newline and tab.
//
// This is a trust boundary, not a formality. The text is user-authored
// AND agent-authored — `hive idea add` runs inside sessions, so an
// agent can file a note that another agent is later launched with —
// and on the typed path it is written straight into a PTY. A bare ESC
// in it is an ANSI sequence the receiving terminal executes, and a
// stray \r submits a half-formed turn.
func promptControlChars(r rune) rune {
	if r == '\n' || r == '\t' {
		return r
	}
	// C0 and DEL.
	if r < 0x20 || r == 0x7f {
		return -1
	}
	// C1 (U+0080–U+009F). Easy to forget because they are not ASCII,
	// and they are exactly as executable: U+009B IS the Control
	// Sequence Introducer, and xterm-family terminals decode the UTF-8
	// encoding of these back into control functions. They have no
	// legitimate use in prose, so there is nothing to weigh here.
	if r >= 0x80 && r <= 0x9f {
		return -1
	}
	return r
}

// sanitizePrompt is the argv form: control characters out, layout kept.
// argv is not a terminal, so a newline here is just a newline in the
// agent's own prompt string.
func sanitizePrompt(s string) string {
	s = strings.Map(promptControlChars, s)
	// Bounded, because CreateSpec.InitialPrompt is a separate entry
	// point: it arrives on the wire and never has to have been an idea
	// at all, so AddIdea's cap does not cover it. Truncated rather than
	// refused — unlike a captured note, nothing is lost the user cannot
	// see and retype, and failing the create over a long prompt is the
	// worse outcome.
	//
	// NOT wire.MaxIdeaText: what arrives here is ideaPrompt()'s
	// instruction preamble PLUS a note that may itself be exactly at
	// that cap, so bounding the sum by it silently ate the tail of
	// every maximum-length note. maxPromptBytes leaves room for the
	// preamble.
	if len(s) > maxPromptBytes {
		// By rune, so the cut cannot land mid-codepoint and hand the
		// agent an invalid UTF-8 tail.
		cut := maxPromptBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	return s
}

// argvPrompt is the argv form for a given platform.
//
// On Windows only, `%` goes too. internal/session spawns argv through
// `cmd.exe /S /C` there, and cmdExeEscape's own doc comment states the
// precondition: it does NOT escape `%`, because cmd.exe expands
// `%VAR%` even inside double quotes. A prompt is user- AND
// agent-authored (`hive idea add` runs inside sessions), so a note
// reading `%GITHUB_TOKEN%` would otherwise be expanded out of the
// daemon's environment and handed to the agent as its first turn.
//
// Stripped rather than escaped because cmd.exe has no quoting that
// neutralizes `%` on a /C line, and platform-conditional because on
// Unix argv reaches execve with no shell in between — mangling every
// "50% of the time" everywhere to fix a Windows-only hole would be the
// wrong trade.
func argvPrompt(goos, s string) string {
	s = sanitizePrompt(s)
	if goos == "windows" {
		// Both characters cmd.exe reinterprets, and for the same
		// reason: internal/session hands the escaped line to
		// `cmd.exe /S /C`, and cmd.exe is not the parser cmdExeEscape
		// quotes for.
		//
		//   %  — expands %VAR% even inside double quotes.
		//   "  — cmdExeEscape emits an embedded quote as \" per
		//        CommandLineToArgvW's rules, which cmd.exe does not
		//        honour: it COUNTS quote characters. One quote in the
		//        note flips the parity, so the tail of the line lands
		//        outside quotes where & | > are live again. A note
		//        reading `x" & calc` is command execution; a note
		//        reading `fix the "start" button` merely breaks the
		//        spawn.
		//
		// Stripped rather than escaped because there is no escape
		// cmd.exe honours on a /C line, and platform-conditional
		// because on Unix argv reaches execve with no shell at all.
		s = strings.NewReplacer("%", "", `"`, "").Replace(s)
	}
	return s
}

// typedPrompt is the PTY form. Same stripping, and then newlines and
// tabs collapse to spaces, because this is delivered as
// `prompt + "\r"` into whatever TUI the agent is running: an embedded
// newline submits the turn early and the rest of the note lands in the
// next one, in pieces.
//
// ponytail: collapsing loses the note's paragraph breaks. The real fix
// is bracketed paste (ESC[200~ … ESC[201~), which every modern TUI
// treats as literal text — do that when an agent actually needs
// multi-line, rather than now on the assumption they all support it.
func typedPrompt(s string) string {
	return strings.Join(strings.Fields(sanitizePrompt(s)), " ")
}

// promptDelivery says how an opening prompt can reach the agent this
// spec resolves to — or that it cannot, which is the default.
type promptDelivery int

const (
	// promptNone: nothing to deliver, or nowhere safe to put it.
	promptNone promptDelivery = iota
	// promptArgv: a bare positional on the spawn command line.
	promptArgv
	// promptTyped: offered to the user on SessionInfo.PendingPrompt
	// and written into the PTY only when they paste it.
	promptTyped
)

// deliveryFor is the ONE decision about where an opening prompt goes.
// resolveAgentCmd, beginCreate and finishCreate all route through it,
// so the argv branch, the typed branch and "when may the idea be
// linked" can never disagree about the same spec.
//
// Everything unrecognised lands on promptNone, deliberately:
//
//   - an explicit spec.Cmd is raw argv from a client that does not
//     speak agent IDs, and we no more append to it than resolveAgentCmd
//     injects SessionIDFlag into it;
//   - the shell agent has no Cmd, and typing into a shell is not
//     prompting an agent, it is running a command — see Def.TypedPrompt;
//   - a user-defined custom agent is an unknown program, and an unknown
//     program is exactly the case the default must be safe for.
func deliveryFor(spec wire.CreateSpec) promptDelivery {
	if spec.InitialPrompt == "" || len(spec.Cmd) > 0 || spec.Agent == "" {
		return promptNone
	}
	def, ok := agent.Get(agent.ID(spec.Agent))
	if !ok || len(def.Cmd) == 0 {
		return promptNone
	}
	switch {
	case def.PositionalPrompt:
		return promptArgv
	case def.TypedPrompt:
		return promptTyped
	}
	return promptNone
}

// materializeWorktree runs the heavy `git worktree add` and promotes
// the plan's cwd into the new worktree. Failure is non-fatal — the
// session falls back to the plain project cwd. Aborting create on
// worktree failure would block users on marginal repos (shallow
// clones, sandbox restrictions, slow filesystems). Does not take r.mu.
func (r *Registry) materializeWorktree(ctx context.Context, e *Entry, spec wire.CreateSpec, p *createPlan) (bool, error) {
	// A worktree was requested but could not even be planned (branch /
	// path resolution failed). Park rather than start a plain session
	// the user never agreed to.
	if p.wtBranch == "" && p.wtPlanErr != "" && p.adoptedPath == "" {
		return r.parkWorktreeChoice(e, spec, *p, &wire.PendingWorktreeChoice{
			Kind:    wire.WorktreeChoiceCreateFailed,
			Message: p.wtPlanErr,
		}, "")
	}
	// Skip the create when we're adopting an existing worktree from a
	// sibling session — the directory is already on disk and `git
	// worktree add` would fail.
	if p.wtBranch == "" || p.adoptedPath != "" {
		return false, nil
	}
	root, err := worktree.Root(p.cwd)
	if err != nil {
		// Near-unreachable: planWorktreeAndName already gated on
		// IsGitRepo. Parked rather than swallowed so the "no silent
		// fallback to the project directory" rule holds on every path.
		return r.parkWorktreeChoice(e, spec, *p, &wire.PendingWorktreeChoice{
			Kind:    wire.WorktreeChoiceCreateFailed,
			Message: err.Error(),
			Branch:  p.wtBranch,
		}, "")
	}

	// Checking out a branch that ALREADY exists never consults
	// upstream, so it must not fetch: it would pay up to 10s of
	// latency for a ref it ignores, and offline it would park behind a
	// dialog whose text ("the new branch would be based on…") is not
	// even true for a checkout — or fail outright with no client
	// attached. Probed first for exactly that reason, which is also
	// what spec 451's non-goals require.
	if worktree.BranchExists(root, p.wtBranch) {
		return r.addWorktree(ctx, e, spec, p, root, "")
	}

	// Step 1: the fetch. gitMu is held for the subprocess only — never
	// across the park below, or one user staring at a dialog would
	// block every other create and kill.
	r.gitMu.Lock()
	r.setPhase(p.id, wire.PhaseFetching)
	base, ferr := worktree.PrepareBase(ctx, root)
	r.gitMu.Unlock()

	if ferr != nil {
		var fe *worktree.FetchError
		if !errors.As(ferr, &fe) {
			return r.parkWorktreeChoice(e, spec, *p, &wire.PendingWorktreeChoice{
				Kind:    wire.WorktreeChoiceCreateFailed,
				Message: ferr.Error(),
				Branch:  p.wtBranch,
			}, "")
		}
		// The cached ref is offered as an explicit choice, never taken
		// on the user's behalf — branching from a stale ref silently is
		// the bug (#451).
		return r.parkWorktreeChoice(e, spec, *p, &wire.PendingWorktreeChoice{
			Kind:             wire.WorktreeChoiceFetchFailed,
			Message:          fe.Stderr,
			Branch:           p.wtBranch,
			CachedRef:        fe.BaseRef,
			CachedTip:        fe.CachedTip,
			CachedTipAgeSecs: int64(fe.TipAge.Seconds()),
		}, fe.BaseRef)
	}

	return r.addWorktree(ctx, e, spec, p, root, base)
}

// addWorktree is step 2: the `git worktree add` itself, with the base
// ref already decided (by PrepareBase, or by the user answering a
// parked fetch failure). Parks on failure rather than falling back to
// a plain session in the project directory.
func (r *Registry) addWorktree(ctx context.Context, e *Entry, spec wire.CreateSpec, p *createPlan, root, base string) (bool, error) {
	// Did anything already occupy this path before we tried? If not,
	// whatever is there afterwards is OURS — including the debris of a
	// failed add. `git worktree add` runs under a 30s deadline and is
	// SIGKILLed on expiry, so git's own junk-cleanup never runs and a
	// half-made directory (plus its .git/worktrees admin entry) can
	// survive a failure. Claiming it only on SUCCESS would leave that
	// debris unowned: every later discard would skip it, Cancel would
	// leave a worktree behind against the spec, and Retry would fail
	// with "already exists" forever.
	//
	// A path that existed BEFORE our add is never claimed, which is
	// what keeps the ownership guard honest — that directory may
	// belong to another session.
	// Inside gitMu with the add itself: gitMu serializes every worktree
	// subprocess, so stat-then-add as one critical section closes the
	// window where a concurrent create could add at this path between
	// our stat and our add — we would otherwise see "free", fail the
	// add, claim ownership of their directory, and delete it on cancel.
	r.gitMu.Lock()
	_, statErr := os.Stat(p.wtPath)
	existedBefore := statErr == nil
	cerr := worktree.CreateWorktreeAt(ctx, root, p.wtBranch, p.wtPath, base)
	r.gitMu.Unlock()
	if !existedBefore {
		p.wtCreated = true
	}
	if cerr != nil {
		return r.parkWorktreeChoice(e, spec, *p, &wire.PendingWorktreeChoice{
			Kind:    wire.WorktreeChoiceCreateFailed,
			Message: cerr.Error(),
			Branch:  p.wtBranch,
		}, base)
	}
	p.cwd = p.wtPath
	r.setPhase(p.id, wire.PhaseWorktree)
	worktree.EnsureGitignore(root)
	worktree.LinkAgentConfig(root, p.wtPath)
	log.Printf("registry: created worktree %s on branch %s (base %q)", p.wtPath, p.wtBranch, base)
	return false, nil
}

// parkedCreate is the state a create needs to resume after the user
// answers a worktree-setup question. Everything in it is plain data:
// that is what lets the create goroutine return instead of blocking,
// so an indefinite wait costs nothing (#451).
type parkedCreate struct {
	entry *Entry
	spec  wire.CreateSpec
	plan  createPlan
	// kind is the failure being asked about (wire.WorktreeChoice*).
	kind string
	// question is this park's own copy of what the user was asked.
	// Held here rather than read back off the entry: the entry's copy
	// is cleared the moment a resolve starts, so restoring from it
	// would restore nil.
	question *wire.PendingWorktreeChoice
	// base is the ref "proceed" should branch from for a fetch failure:
	// the cached upstream tip. Empty for a create failure, where
	// proceeding means no worktree at all.
	base string
}

// parkWorktreeChoice suspends a create on a user decision. It returns
// (true, nil) when the question was parked, and (false, err) when
// nothing can answer it — in which case the entry is removed and the
// create fails, because proceeding silently is the behaviour this
// whole feature exists to delete.
func (r *Registry) parkWorktreeChoice(e *Entry, spec wire.CreateSpec, p createPlan, q *wire.PendingWorktreeChoice, base string) (bool, error) {
	// Scrub AND cap at the sink, not at each source. git echoes the
	// remote back in its errors and an HTTPS remote can carry a token
	// in its userinfo; this message goes to a GUI dialog and to
	// hived.log, and rides every SessionInfo broadcast for as long as
	// the park lasts. Every park path funnels through here, so one call
	// covers the fetch, the add, and the plan-time failures — and any
	// added later.
	q.Message = worktree.ScrubURLCredentials(q.Message)
	if len(q.Message) > wire.MaxWorktreeChoiceMessage {
		q.Message = q.Message[:wire.MaxWorktreeChoiceMessage] + "…"
	}
	// Identifies THIS park, so an answer composed against a superseded
	// question is not applied under its new meaning.
	q.ParkID = uuid.NewString()

	if !r.canAskUser() {
		log.Printf("registry: worktree setup failed for %s and no control client is connected to ask: %s", p.id, q.Message)
		r.discardWorktree(p)
		r.removeEntry(p.id)
		return false, fmt.Errorf("worktree setup failed and no client is connected to ask: %s", q.Message)
	}

	qCopy := *q
	r.parkedMu.Lock()
	r.parked[p.id] = &parkedCreate{
		entry: e, spec: spec, plan: p, kind: q.Kind, base: base, question: &qCopy,
	}
	r.parkedMu.Unlock()

	r.mu.Lock()
	if _, ok := r.entries[p.id]; !ok {
		// Killed while the git work ran. Undo the park and let the
		// caller unwind exactly as the spawn-failure path does.
		r.mu.Unlock()
		r.parkedMu.Lock()
		delete(r.parked, p.id)
		r.parkedMu.Unlock()
		r.discardWorktree(p)
		return false, ErrNotFound
	}
	e.pendingChoice = q
	e.Phase = wire.PhaseBlocked
	// The one bit that outlives this daemon: without it, boot cannot
	// tell this entry from an ordinary worktree-less session.
	e.awaitingChoice = true
	r.persistEntryLoggedLocked(e, "create (parked on worktree choice)")
	info := e.Info()
	r.broadcastLocked(wire.SessionEventUpdated, info)
	r.mu.Unlock()

	log.Printf("registry: create %s parked on %s: %s", p.id, q.Kind, q.Message)
	return true, nil
}

// ResolveWorktreeChoice answers a parked worktree-setup question and
// resumes (or abandons) the create.
//
// Resolving a session that is not parked is a no-op: two clients can
// race to answer the same dialog, and the loser must not see an error
// for a decision that was made correctly once.
func (r *Registry) ResolveWorktreeChoice(ctx context.Context, id, choice, parkID string) error {
	// Validate before mutating anything. An unknown value used to be
	// handled by clearing the question and then putting it back, which
	// could not work: the restore read the entry the clear had just
	// nil-ed, leaving the session blocked with nothing to answer.
	switch choice {
	case wire.WorktreeChoiceCancel, wire.WorktreeChoiceRetry, wire.WorktreeChoiceProceed:
	default:
		return fmt.Errorf("unknown worktree choice %q", choice)
	}

	r.parkedMu.Lock()
	pc, ok := r.parked[id]
	if ok && parkID != "" && pc.question != nil && pc.question.ParkID != parkID {
		// An answer to a question this session has already moved on
		// from (a retry that failed differently, so "proceed" now means
		// something else). Ignore it rather than apply it under the new
		// meaning. An empty parkID is a client too old to echo it.
		r.parkedMu.Unlock()
		log.Printf("registry: ignoring stale worktree answer for %s", id)
		return nil
	}
	if ok {
		delete(r.parked, id)
	}
	r.parkedMu.Unlock()
	if !ok {
		return nil
	}

	// Clear the question first: whatever happens next, it has been
	// answered, and a stale copy would re-raise the dialog on any
	// client that reconnects.
	r.clearPendingChoice(id)

	switch choice {
	case wire.WorktreeChoiceCancel:
		log.Printf("registry: create %s cancelled at the worktree prompt", id)
		r.discardWorktree(pc.plan)
		r.removeEntry(id)
		return nil

	case wire.WorktreeChoiceRetry:
		// Re-run the whole worktree step, including the fetch: the
		// user's reason for retrying is usually that they fixed the
		// network, and a retry that skipped the fetch would branch
		// from the same stale ref it just warned about.
		plan := pc.plan
		// A half-made worktree directory from the failed attempt would
		// make `git worktree add` fail forever ("already exists"), so
		// Retry would be permanently useless. cancel and kill already
		// discard; this path has to as well.
		r.discardWorktree(plan)
		// A failure at PLAN time left no branch to retry with, so
		// re-plan rather than replaying the cached error string.
		if plan.wtBranch == "" && plan.wtPlanErr != "" {
			plan.wtPlanErr = ""
			r.planWorktreeAndName(pc.spec, &plan)
			// Re-planning can land on a different branch than the name
			// was derived from, and a session labelled after a branch
			// it is not on is exactly what renameAfterWorktreeFailure
			// exists to prevent.
			if plan.nameFromBranch && plan.name != "" {
				r.renameEntry(plan.id, plan.name)
			}
		}
		parked, err := r.materializeWorktree(ctx, pc.entry, pc.spec, &plan)
		if err != nil || parked {
			return err
		}
		return r.finishCreateTail(ctx, pc.entry, pc.spec, plan)

	case wire.WorktreeChoiceProceed:
		plan := pc.plan
		if pc.kind == wire.WorktreeChoiceFetchFailed {
			// Explicitly accept the cached ref.
			root, err := worktree.Root(plan.cwd)
			if err != nil {
				r.discardWorktree(plan)
				r.removeEntry(id)
				return err
			}
			// Same reason as Retry: a leftover directory from the
			// failed attempt would fail the add.
			r.discardWorktree(plan)
			parked, aerr := r.addWorktree(ctx, pc.entry, pc.spec, &plan, root, pc.base)
			if aerr != nil || parked {
				return aerr
			}
			return r.finishCreateTail(ctx, pc.entry, pc.spec, plan)
		}
		// create_failed: proceed means a plain session in the project
		// directory, which is what used to happen silently.
		log.Printf("registry: create %s proceeding without a worktree by user choice", id)
		// Discard BEFORE blanking the plan: wtPath is the only handle
		// to the half-made directory, so clearing it first would orphan
		// that directory with nothing left able to clean it up. Every
		// other exit in this function discards.
		r.discardWorktree(plan)
		plan.wtPath, plan.wtBranch = "", ""
		return r.finishCreateTail(ctx, pc.entry, pc.spec, plan)
	}

	// Unreachable: the switch above validated choice before anything
	// was touched.
	return nil
}

// renameEntry relabels an entry in place, persisting and announcing
// it. Used when a re-plan moves the worktree branch the name came
// from. Takes r.mu.
func (r *Registry) renameEntry(id, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok || e.Name == name {
		return
	}
	e.Name = name
	r.persistEntryLoggedLocked(e, "create (renamed after re-plan)")
	r.broadcastLocked(wire.SessionEventUpdated, e.Info())
}

// clearPendingChoice drops a parked entry's question and its persisted
// marker, and announces the change.
func (r *Registry) clearPendingChoice(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return
	}
	e.pendingChoice = nil
	e.awaitingChoice = false
	e.Phase = wire.PhaseStarting
	r.persistEntryLoggedLocked(e, "resolve worktree choice")
	r.broadcastLocked(wire.SessionEventUpdated, e.Info())
}

// removeEntry deletes an entry that never became a session and tells
// clients it is gone. Used by the cancel and no-client paths, where
// beginCreate has already announced an entry that will now never exist.
func (r *Registry) removeEntry(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return
	}
	delete(r.entries, id)
	for i, oid := range r.order {
		if oid == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	r.reindexLocked()
	r.persistIndexLoggedLocked("remove parked entry")
	_ = os.RemoveAll(filepath.Join(SessionsDir(r.stateDir), id))
	r.broadcastLocked(wire.SessionEventRemoved, e.Info())
}

// dropParked removes any parked create for id and returns its plan, so
// a kill can discard a worktree the entry does not yet know about.
// Kill is the one path that can delete a parked entry from under the
// dialog, and without this the resume state would leak and a
// partly-made worktree would be orphaned.
func (r *Registry) dropParked(id string) (createPlan, bool) {
	r.parkedMu.Lock()
	defer r.parkedMu.Unlock()
	pc, ok := r.parked[id]
	if !ok {
		return createPlan{}, false
	}
	delete(r.parked, id)
	return pc.plan, true
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
	r.attachSessionHooks(e, sess)
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
