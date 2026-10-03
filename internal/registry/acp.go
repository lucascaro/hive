package registry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/lucascaro/hive/internal/acp"
	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/agentstate"
	"github.com/lucascaro/hive/internal/wire"
)

// ACP sessions (spec 496). An ACP entry runs its agent through an ACP
// adapter the daemon talks JSON-RPC to over stdio, instead of in a PTY.
// It shares everything else with a terminal session — the entry, its
// persistence, worktree, phase and state machine — and differs only in
// what is alive: e.acp, never e.sess.

var (
	// ErrNotACP is returned by an ACP operation on a terminal session.
	ErrNotACP = errors.New("registry: session is not an ACP session")
	// ErrACPBusy is returned by PromptACP while a turn is running. ACP
	// allows one prompt in flight per session; rejecting rather than
	// queueing keeps every result and origin on the turn that caused it.
	ErrACPBusy = errors.New("registry: the agent is still working on the last prompt")
	// ErrPermissionStale is returned by AnswerPermission for a request
	// that is no longer pending, or an option it did not offer.
	ErrPermissionStale = errors.New("registry: that permission request is no longer pending")
	// ErrBadKind is returned by Create for a kind it cannot honour.
	ErrBadKind = errors.New("registry: invalid session kind")
)

// acpStartTimeout bounds initialize plus session/new or session/load.
// Generous because the first `npx -y <adapter>` downloads the package.
var acpStartTimeout = 2 * time.Minute

// acpCommand returns the adapter argv and its complete environment for
// def. The seam tests use to run the fake agent; the default reads the
// login-shell PATH, which must never run under r.mu.
var acpCommand = func(def agent.Def) (argv, env []string) {
	return def.ACP().Argv, acp.AdapterEnv(os.Environ(), agent.ACPEnvPATH())
}

// acpSession is the live half of an ACP entry. Guarded by r.mu.
type acpSession struct {
	agent *acp.Agent
	// replaying is true while session/load replays history: those
	// updates rebuild the transcript but are past turns, not state.
	replaying bool
	// ready is set once the conversation is open (session/new or
	// session/load returned); no prompt is sent before it.
	ready bool
	// busy is true while a session/prompt is in flight.
	busy bool
	// perm is the permission request the agent is blocked on, or nil.
	perm   *acpPending
	nextRq int
}

type acpPending struct {
	info   wire.AcpPermission
	answer chan any
}

// acpKind normalises CreateSpec.Kind for storage: "" for a terminal
// session (so its session.json is unchanged), wire.KindACP otherwise.
func acpKind(k string) string {
	if k == wire.KindACP {
		return wire.KindACP
	}
	return ""
}

// validateKind rejects a create the requested kind cannot serve,
// before anything is registered.
func validateKind(spec wire.CreateSpec) error {
	switch spec.Kind {
	case "", wire.KindPTY:
		return nil
	case wire.KindACP:
	default:
		return fmt.Errorf("%w %q", ErrBadKind, spec.Kind)
	}
	def, ok := agent.Get(agent.ID(spec.Agent))
	if !ok || def.ACP() == nil {
		return fmt.Errorf("%w: agent %q cannot run as an ACP session", ErrBadKind, spec.Agent)
	}
	if len(spec.Cmd) > 0 || spec.ContinueConversation {
		return fmt.Errorf("%w: an ACP session takes neither a command nor continue-conversation", ErrBadKind)
	}
	return nil
}

// finishCreateACP is finishCreateTail for an ACP entry: start the
// adapter, open a conversation, then send the opening prompt, if any,
// as its first turn.
func (r *Registry) finishCreateACP(e *Entry, spec wire.CreateSpec, p createPlan) error {
	// Bind the worktree before the adapter starts, as attachSession
	// does for a PTY: revive and restart take their cwd from it, kill
	// disposes of it, and an unclaimed worktree is reclaimed at boot.
	r.mu.Lock()
	if cur, ok := r.entries[p.id]; !ok || cur != e {
		r.mu.Unlock()
		r.discardWorktree(p)
		return ErrNotFound
	}
	if p.wtPath != "" {
		e.WorktreePath = p.wtPath
		e.WorktreeBranch = p.wtBranch
		r.persistEntryLoggedLocked(e, "create (acp worktree)")
	}
	r.mu.Unlock()

	r.setPhase(p.id, wire.PhaseSpawning)
	if err := r.startACP(p.id, p.cwd, ""); err != nil {
		r.setPhaseIf(p.id, wire.PhaseSpawning, wire.PhaseReady)
		return err
	}
	r.setPhaseIf(p.id, wire.PhaseSpawning, wire.PhaseReady)
	// The opening prompt is the first turn, not argv: an ACP session
	// has no command line. The idea it came from is claimed only once
	// the prompt is actually on its way, as for a terminal session.
	if text := spec.InitialPrompt; text != "" {
		origin := spec.SpawnedBy
		if origin == "" {
			origin = wire.OriginUser
		}
		if err := r.PromptACP(p.id, text, origin); err != nil {
			log.Printf("registry: create %s: opening prompt not sent: %v", p.id, err)
			return nil
		}
	}
	r.linkIdeaToSession(spec.IdeaID, p.id)
	return nil
}

// startACP starts the entry's adapter in cwd and opens a conversation:
// session/load of loadID when set (revive, restart), otherwise
// session/new, whose id becomes the entry's AgentSessionID. Must not be
// called with r.mu held.
func (r *Registry) startACP(id, cwd, loadID string) error {
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	agentID := e.Agent
	r.mu.Unlock()

	fail := func(err error) error {
		r.mu.Lock()
		if cur, ok := r.entries[id]; ok && cur == e {
			e.LastError = err.Error()
			info := e.Info()
			r.mu.Unlock()
			r.broadcast(wire.SessionEventUpdated, info)
		} else {
			r.mu.Unlock()
		}
		log.Printf("registry: acp %s: %v", id, err)
		return err
	}

	def, ok := agent.Get(agent.ID(agentID))
	if !ok || def.ACP() == nil {
		return fail(fmt.Errorf("%w: agent %q cannot run as an ACP session", ErrBadKind, agentID))
	}
	argv, env := acpCommand(def)
	as := &acpSession{}
	a, err := acp.Start(acp.Spec{Argv: argv, Env: env, Cwd: cwd}, acp.Handler{
		Update: func(_ string, u acp.Update) { r.onACPUpdate(id, as, u) },
		Permission: func(ctx context.Context, req acp.PermissionRequest) any {
			return r.onACPPermission(ctx, id, as, req)
		},
	})
	if err != nil {
		return fail(err)
	}
	as.agent = a
	as.replaying = loadID != ""

	// Bind before the conversation opens, so the replay a session/load
	// streams lands in this entry's transcript.
	r.mu.Lock()
	if cur, ok := r.entries[id]; !ok || cur != e || e.acp != nil {
		r.mu.Unlock()
		a.Close()
		return ErrNotFound
	}
	e.acp = as
	e.state = agentstate.New(time.Now())
	e.acpTx.Reset()
	r.broadcastACPLocked(e, nil, true)
	r.mu.Unlock()
	go r.watchACPExit(id, as)

	ctx, cancel := context.WithTimeout(context.Background(), acpStartTimeout)
	defer cancel()
	abort := func(err error) error {
		a.Close() // watchACPExit unbinds it
		return fail(err)
	}
	init, err := a.Initialize(ctx)
	if err != nil {
		return abort(fmt.Errorf("acp initialize: %w", err))
	}
	if loadID != "" {
		if !init.AgentCapabilities.LoadSession {
			return abort(errors.New("the ACP adapter cannot reopen conversations (no loadSession)"))
		}
		if err := a.LoadSession(ctx, loadID, cwd, nil); err != nil {
			return abort(fmt.Errorf("acp session/load: %w", err))
		}
	} else {
		ns, err := a.NewSession(ctx, cwd, nil)
		if err != nil {
			return abort(fmt.Errorf("acp session/new: %w", err))
		}
		loadID = ns.SessionID
	}

	r.mu.Lock()
	if e.acp != as {
		r.mu.Unlock()
		return ErrNotFound
	}
	as.replaying = false
	as.ready = true
	e.LastError = ""
	if e.AgentSessionID != loadID {
		e.AgentSessionID = loadID
		r.persistEntryLoggedLocked(e, "acp session id")
	}
	info := e.Info()
	r.mu.Unlock()
	r.broadcast(wire.SessionEventUpdated, info)
	return nil
}

// watchACPExit is watchSessionExit for an adapter: when it exits and
// is still the entry's, unbind it and report the session dead.
func (r *Registry) watchACPExit(id string, as *acpSession) {
	<-as.agent.Done()
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok || e.acp != as {
		r.mu.Unlock()
		return
	}
	e.acp = nil
	if msg := as.agent.LastError(); msg != "" && e.LastError == "" {
		e.LastError = "ACP adapter exited: " + msg
	}
	prev := e.stateSnapshot()
	e.machine().Exit()
	logStateLocked(e, prev, e.stateSnapshot(), "exit")
	r.broadcastACPLocked(e, nil, false)
	info := e.Info()
	r.mu.Unlock()
	r.broadcast(wire.SessionEventUpdated, info)
}

// stopACP closes the entry's adapter, if any, and waits for it to go,
// clearing e.acp itself so watchACPExit records no error.
func (r *Registry) stopACP(id string, as *acpSession) {
	if as == nil {
		return
	}
	r.mu.Lock()
	if e, ok := r.entries[id]; ok && e.acp == as {
		e.acp = nil
	}
	r.mu.Unlock()
	as.agent.Close()
}

// onACPUpdate folds one session/update into the transcript and, for a
// live turn, into the state machine.
func (r *Registry) onACPUpdate(id string, as *acpSession, u acp.Update) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok || e.acp != as {
		return
	}
	items := e.acpTx.Apply(u, as.replaying)
	if !as.replaying {
		switch u.SessionUpdate {
		case acp.UpdateToolCall:
			r.applyACPLocked(e, agentstate.Event{Kind: agentstate.KindToolStart, Tool: toolLabel(u), CallID: u.ToolCallID})
		case acp.UpdateToolCallPatch:
			if u.Status == "completed" || u.Status == "failed" {
				ok := u.Status == "completed"
				r.applyACPLocked(e, agentstate.Event{Kind: agentstate.KindToolEnd, Tool: toolLabel(u), CallID: u.ToolCallID, OK: &ok})
			}
		case acp.UpdatePlan:
			plan := make([]wire.PlanItem, len(u.Entries))
			for i, p := range u.Entries {
				plan[i] = wire.PlanItem{Text: p.Content, Status: p.Status}
			}
			r.applyACPLocked(e, agentstate.Event{Kind: agentstate.KindPlan, Items: plan})
		}
	}
	if len(items) > 0 {
		r.broadcastACPLocked(e, items, false)
	}
}

func toolLabel(u acp.Update) string {
	if u.Kind != "" {
		return u.Kind
	}
	return u.Title
}

// onACPPermission parks the agent's permission request until the user
// answers it (AnswerPermission) or the adapter goes away.
//
// Every request goes to the user. Phase 3 of spec 496 adds the one
// exception — the session's own submit_result tool — and nothing else.
func (r *Registry) onACPPermission(ctx context.Context, id string, as *acpSession, req acp.PermissionRequest) any {
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok || e.acp != as {
		r.mu.Unlock()
		return acp.Cancelled()
	}
	if as.perm != nil {
		// Adapters ask one at a time; a second concurrent request has no
		// place in the UI, so it is refused rather than hidden.
		r.mu.Unlock()
		log.Printf("registry: acp %s: second concurrent permission request refused", id)
		return acp.Cancelled()
	}
	as.nextRq++
	p := &acpPending{answer: make(chan any, 1), info: wire.AcpPermission{
		RequestID:  strconv.Itoa(as.nextRq),
		ToolCallID: req.ToolCall.ToolCallID,
		Title:      req.ToolCall.Title,
	}}
	for _, o := range req.Options {
		p.info.Options = append(p.info.Options, wire.AcpPermissionOption{OptionID: o.OptionID, Name: o.Name, Kind: o.Kind})
	}
	as.perm = p
	r.applyACPLocked(e, agentstate.Event{Kind: agentstate.KindWaitingPermission})
	r.broadcastACPLocked(e, nil, false)
	r.mu.Unlock()

	var answer any
	select {
	case answer = <-p.answer:
	case <-ctx.Done():
		answer = acp.Cancelled()
	}

	r.mu.Lock()
	if as.perm == p {
		as.perm = nil
		if e.acp == as {
			r.applyACPLocked(e, agentstate.Event{Kind: agentstate.KindPermissionResolved})
			r.broadcastACPLocked(e, nil, false)
		}
	}
	r.mu.Unlock()
	return answer
}

// AnswerPermission answers the pending permission request requestID
// with one of the options it offered.
func (r *Registry) AnswerPermission(id, requestID, optionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return ErrNotFound
	}
	if !e.isACP() {
		return ErrNotACP
	}
	if e.acp == nil || e.acp.perm == nil || e.acp.perm.info.RequestID != requestID {
		return ErrPermissionStale
	}
	p := e.acp.perm
	for _, o := range p.info.Options {
		if o.OptionID == optionID {
			select {
			case p.answer <- acp.Selected(optionID):
			default: // already answered; the waiter is on its way out
			}
			return nil
		}
	}
	return ErrPermissionStale
}

// PromptACP sends one user turn. It returns once the turn has started;
// the turn runs on its own goroutine and ends in turn_end or error.
// origin is who sent it (wire.OriginUser, or a principal), recorded on
// the transcript item.
func (r *Registry) PromptACP(id, text, origin string) error {
	if text == "" {
		return errors.New("registry: empty prompt")
	}
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	if !e.isACP() {
		r.mu.Unlock()
		return ErrNotACP
	}
	as := e.acp
	if as == nil || !as.ready {
		r.mu.Unlock()
		return ErrNoLiveSession
	}
	if as.busy {
		r.mu.Unlock()
		return ErrACPBusy
	}
	as.busy = true
	sid := e.AgentSessionID
	item := e.acpTx.AddUser(text, origin)
	r.applyACPLocked(e, agentstate.Event{Kind: agentstate.KindPrompt, Text: text})
	r.broadcastACPLocked(e, []wire.AcpItem{item}, false)
	r.mu.Unlock()

	go func() {
		stop, err := as.agent.Prompt(context.Background(), sid, text)
		r.mu.Lock()
		defer r.mu.Unlock()
		as.busy = false
		cur, ok := r.entries[id]
		if !ok || cur.acp != as {
			return
		}
		if err != nil {
			r.applyACPLocked(cur, agentstate.Event{Kind: agentstate.KindError, Text: err.Error()})
			return
		}
		ev := agentstate.Event{Kind: agentstate.KindTurnEnd}
		if stop != "" && stop != "end_turn" {
			ev.Text = "stopped: " + stop
		}
		r.applyACPLocked(cur, ev)
	}()
	return nil
}

// IsACP reports whether id names an ACP session.
func (r *Registry) IsACP(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	return ok && e.isACP()
}

// AcpTranscript returns the whole transcript of an ACP session — the
// answer to GET_ACP_TRANSCRIPT.
func (r *Registry) AcpTranscript(id string) (wire.AcpTranscriptMsg, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return wire.AcpTranscriptMsg{}, ErrNotFound
	}
	if !e.isACP() {
		return wire.AcpTranscriptMsg{}, ErrNotACP
	}
	return acpMsgLocked(e, e.acpTx.Snapshot(), true), nil
}

// applyACPLocked feeds one ACP-derived event to the state machine and
// announces it, the way ApplyAgentEvent does for hook events.
func (r *Registry) applyACPLocked(e *Entry, ev agentstate.Event) {
	now := time.Now()
	ev.Source, ev.At, ev.Now = wire.StateSourceACP, now, now
	prev := e.stateSnapshot()
	if e.machine().Apply(ev) {
		r.announceStateLocked(e, prev, ev.Kind)
	}
	r.broadcastActivityLocked(e, ev.Kind)
}

func acpMsgLocked(e *Entry, items []wire.AcpItem, reset bool) wire.AcpTranscriptMsg {
	msg := wire.AcpTranscriptMsg{SessionID: e.ID, Epoch: e.acpTx.Epoch(), Reset: reset, Items: items}
	if e.acp != nil && e.acp.perm != nil {
		p := e.acp.perm.info
		p.Options = append([]wire.AcpPermissionOption(nil), p.Options...)
		msg.Permission = &p
	}
	return msg
}

// broadcastACPLocked fans one transcript message out to every ACP
// listener. Callers hold r.mu.
func (r *Registry) broadcastACPLocked(e *Entry, items []wire.AcpItem, reset bool) {
	msg := acpMsgLocked(e, items, reset)
	for ch := range r.acpListeners {
		select {
		case ch <- msg:
		default:
			log.Printf("registry: dropping slow ACP transcript listener (buffer %d full); closing the channel, so the daemon hangs up on that client and it reconnects", cap(ch))
			delete(r.acpListeners, ch)
			close(ch)
		}
	}
}

// AcpListener receives ACP_TRANSCRIPT messages.
type AcpListener chan wire.AcpTranscriptMsg

// SubscribeACP returns a channel that receives every ACP transcript
// change, for every session — the same unfiltered model as
// SubscribeActivity. The cleanup function unsubscribes and closes it.
func (r *Registry) SubscribeACP() (AcpListener, func()) {
	ch := make(AcpListener, 256)
	r.mu.Lock()
	if r.acpListeners == nil {
		r.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	r.acpListeners[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		if _, ok := r.acpListeners[ch]; ok {
			delete(r.acpListeners, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
}

// SetACPCommandForTest replaces how ACP adapters are launched, for
// packages that drive the registry from outside (the daemon's tests);
// it returns a restore func.
func SetACPCommandForTest(fn func(agent.Def) (argv, env []string)) func() {
	prev := acpCommand
	acpCommand = fn
	return func() { acpCommand = prev }
}
