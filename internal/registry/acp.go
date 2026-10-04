package registry

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
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
	// ErrACPRefused is returned when the user's settings forbid an ACP
	// session of the agent (agent.Def.ACPRefusal).
	ErrACPRefused = errors.New("registry: ACP is off for this agent")
	// ErrACPModeAboveCeiling is returned by Create for a CreateSpec.ACPMode
	// above the user's ceiling for the agent. Refused, never lowered.
	ErrACPModeAboveCeiling = errors.New("registry: ACP mode is above your ceiling for this agent")
	// ErrSubmitRejected is returned by SubmitResult for a wrong nonce, a
	// session with no running turn to attach to, or a malformed result.
	ErrSubmitRejected = errors.New("registry: result rejected")
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
	spec  *agent.ACPSpec
	// mode is the mode set after session/new and session/load, and the
	// one an unrequested escalation is reset to; ceiling is the rank of
	// the user's ceiling, which a mode switch may reach on its own.
	mode    string
	ceiling int
	// userAllowed is set when the user answers a permission request
	// with an allow option, and cleared by the next tool call, prompt
	// or turn end. A mode switch above the ceiling inside that window
	// is the user's own choice (Claude's exit-plan card offers them) and
	// is kept; any other is reset to mode.
	userAllowed bool
	// nonce authenticates SUBMIT_RESULT from this process's submit
	// server; server is that server's name, which every structured
	// identity of its tool embeds (acp.SubmitIdentities). New per start.
	nonce, server string
	// tools is each live tool_call update by ToolCallID, for the
	// permission request that may follow it. Cleared every prompt.
	tools map[string]acp.Update
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

// acpResult is the typed result of the latest prompt; see the result
// fields of wire.AcpTranscriptMsg.
type acpResult struct {
	// epoch is the transcript epoch promptID belongs to: item ids
	// restart with each adapter process.
	epoch    int
	promptID int
	status   string
	value    json.RawMessage
}

// maxACPToolRefs bounds acpSession.tools within one turn.
const maxACPToolRefs = 512

// acpSetModeTimeout bounds a set_mode that resets an escalation.
var acpSetModeTimeout = 30 * time.Second

type acpPending struct {
	info   wire.AcpPermission
	answer chan any // buffered 1; written once, under r.mu
	// answered is set by the one answer that counts; any later one (a
	// second window, a double click) is refused as stale. Under r.mu.
	answered bool
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
	st := agent.SpawnSettings()
	if reason := def.ACPRefusal(st); reason != "" {
		return fmt.Errorf("%w: %s", ErrACPRefused, reason)
	}
	if spec.ACPMode != "" {
		acpSpec := def.ACP()
		rank := acpSpec.ModeRank(spec.ACPMode)
		if rank < 0 {
			return fmt.Errorf("%w: %s has no ACP mode %q", ErrBadKind, def.Name, spec.ACPMode)
		}
		if ceiling := st.ACPCeiling(def.ID); rank > acpSpec.ModeRank(ceiling) {
			return fmt.Errorf("%w: %q is above %q", ErrACPModeAboveCeiling, spec.ACPMode, ceiling)
		}
	}
	return nil
}

// acpModeFor is the mode an ACP session starts in: the one it asked for
// at create when that is still at or below the user's ceiling, and the
// ceiling otherwise — the user may have lowered it since (revive,
// restart). Returns the mode and the ceiling's rank.
func acpModeFor(spec *agent.ACPSpec, ceiling, requested string) (string, int) {
	limit := spec.ModeRank(ceiling)
	if r := spec.ModeRank(requested); r >= 0 && r <= limit {
		return requested, limit
	}
	return ceiling, limit
}

// newNonce returns n random bytes, hex-encoded.
func newNonce(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // never fails (crypto/rand, Go 1.24+)
	return hex.EncodeToString(b)
}

// acpExtras is what one adapter start adds to the seam's argv/env: the
// session's submit transport. Every agent but Pi gets a stdio MCP
// server, `hived mcp-submit`, sent on session/new and session/load.
// Pi ignores mcpServers (F5), so it gets Hive's extension through the
// pi-acp shim instead, and the extension registers the same tool. The
// session's own HIVE_* values reach only these two places: AdapterEnv
// stripped every inherited one from the adapter.
func (r *Registry) acpExtras(id string, spec *agent.ACPSpec, as *acpSession) (env []string, mcp []acp.MCPServer) {
	r.mu.Lock()
	hived, stateDir := r.hivedPath, r.stateDir
	r.mu.Unlock()
	hive := append(r.hiveEnv(id), "HIVE_SUBMIT_NONCE="+as.nonce)
	if spec.NoApprovalGate {
		shim, err := agent.PiACPShim(stateDir)
		if err != nil {
			log.Printf("registry: acp %s: no Pi shim (%v); this session cannot submit a result", id, err)
			return nil, nil
		}
		// Only the submit tool: the extension's state reports are dropped
		// for an ACP session, so its plan tools would be dead weight.
		return append(hive, "PI_ACP_PI_COMMAND="+shim, agent.PiTodoToolEnv+"=0", agent.PiPlanReviewEnv+"=0"), nil
	}
	if hived == "" {
		log.Printf("registry: acp %s: hived path unknown; this session cannot submit a result", id)
		return nil, nil
	}
	server := acp.MCPServer{Name: as.server, Command: hived, Args: []string{"mcp-submit"}}
	for _, kv := range hive {
		k, v, _ := strings.Cut(kv, "=")
		server.Env = append(server.Env, acp.EnvVar{Name: k, Value: v})
	}
	return nil, []acp.MCPServer{server}
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
	agentID, requested := e.Agent, e.ACPMode
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
	// Checked at every start, not only at create: a setting the user
	// lowered since applies from the next start on.
	st := agent.SpawnSettings()
	if reason := def.ACPRefusal(st); reason != "" {
		return fail(fmt.Errorf("%w: %s", ErrACPRefused, reason))
	}
	spec := def.ACP()
	as := &acpSession{spec: spec, nonce: newNonce(32), server: "hive-" + newNonce(6), tools: map[string]acp.Update{}}
	as.mode, as.ceiling = acpModeFor(spec, st.ACPCeiling(def.ID), requested)
	argv, env := acpCommand(def)
	extraEnv, mcp := r.acpExtras(id, spec, as)
	env = append(env, extraEnv...)
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
	e.acpRes = acpResult{}
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
	var modes *acp.Modes
	if loadID != "" {
		if !init.AgentCapabilities.LoadSession {
			return abort(errors.New("the ACP adapter cannot reopen conversations (no loadSession)"))
		}
		ld, err := a.LoadSession(ctx, loadID, cwd, mcp)
		if err != nil {
			return abort(fmt.Errorf("acp session/load: %w", err))
		}
		modes = ld.Modes
	} else {
		ns, err := a.NewSession(ctx, cwd, mcp)
		if err != nil {
			return abort(fmt.Errorf("acp session/new: %w", err))
		}
		loadID, modes = ns.SessionID, ns.Modes
	}
	// The mode is set explicitly after every new and load, never left
	// to the adapter: Codex starts — and reloads — in "agent", which
	// called the submit tool unasked in spike 492 (F4).
	if !spec.NoApprovalGate {
		if modes != nil && !slices.ContainsFunc(modes.AvailableModes, func(m acp.Mode) bool { return m.ID == as.mode }) {
			return abort(fmt.Errorf("the ACP adapter does not offer mode %q; refusing to run it in another", as.mode))
		}
		if err := a.SetMode(ctx, loadID, as.mode); err != nil {
			return abort(fmt.Errorf("acp session/set_mode %q: %w", as.mode, err))
		}
	}

	r.mu.Lock()
	if e.acp != as {
		r.mu.Unlock()
		return ErrNotFound
	}
	wasReplay := as.replaying
	as.replaying = false
	as.ready = true
	if wasReplay {
		r.broadcastACPLocked(e, e.acpTx.Snapshot(), true)
	}
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
		case acp.UpdateCurrentMode:
			r.onACPModeLocked(id, e, as, u.CurrentModeID)
		case acp.UpdateToolCall:
			as.userAllowed = false
			if len(as.tools) >= maxACPToolRefs {
				clear(as.tools)
			}
			as.tools[u.ToolCallID] = u
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
	// A session/load replay can be thousands of chunks; one broadcast
	// each would overflow a listener and drop the client mid-replay.
	// The replay is announced once, as a reset, when it ends.
	if len(items) > 0 && !as.replaying {
		r.broadcastACPLocked(e, items, false)
	}
}

// onACPModeLocked enforces the ceiling on a mode switch the adapter
// reports. At or below the ceiling it stands. Above it, it stands only
// when the user just allowed something on a permission card (Claude's
// exit-plan card switches mode this way); otherwise it is reset to the
// session's mode, and an adapter that refuses the reset is closed.
// Pi's modes are thinking levels, not permissions, so they are not
// policed. Callers hold r.mu.
func (r *Registry) onACPModeLocked(id string, e *Entry, as *acpSession, mode string) {
	if as.spec.NoApprovalGate {
		return
	}
	if rank := as.spec.ModeRank(mode); rank >= 0 && rank <= as.ceiling {
		return
	}
	if as.userAllowed {
		as.userAllowed = false
		log.Printf("registry: acp %s: mode %q above the ceiling, kept: the user chose it on a permission card", id, mode)
		return
	}
	log.Printf("registry: acp %s: adapter switched to mode %q above the ceiling; resetting to %q", id, mode, as.mode)
	sid, reset := e.AgentSessionID, as.mode
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), acpSetModeTimeout)
		defer cancel()
		if err := as.agent.SetMode(ctx, sid, reset); err != nil {
			log.Printf("registry: acp %s: reset to mode %q failed (%v); closing the adapter", id, reset, err)
			r.mu.Lock()
			if cur, ok := r.entries[id]; ok && cur.acp == as && cur.LastError == "" {
				cur.LastError = fmt.Sprintf("closed: the agent switched to a mode above your ceiling and would not switch back (%v)", err)
			}
			r.mu.Unlock()
			as.agent.Close() // watchACPExit reports it
		}
	}()
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
// Every request goes to the user except one: the session's own
// submit_result tool, auto-allowed on exact identity.
func (r *Registry) onACPPermission(ctx context.Context, id string, as *acpSession, req acp.PermissionRequest) any {
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok || e.acp != as {
		r.mu.Unlock()
		return acp.Cancelled()
	}
	// The one request the daemon answers itself: this session's own
	// submit tool, matched on exact structured identity bound to its
	// own server name. Everything else goes to the user.
	if as.server != "" && !as.spec.NoApprovalGate {
		var earlier *acp.Update
		if u, ok := as.tools[req.ToolCall.ToolCallID]; ok {
			earlier = &u
		}
		if opt, ok := acp.AutoAllowOption(req, earlier, acp.SubmitIdentities(as.server)); ok {
			r.mu.Unlock()
			return acp.Selected(opt)
		}
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
		// Epoch-qualified: each adapter process restarts its counter,
		// and a late answer to request 1 of the last process must not
		// satisfy request 1 of this one.
		RequestID:  strconv.Itoa(e.acpTx.Epoch()) + "-" + strconv.Itoa(as.nextRq),
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
	if e.acp == nil || e.acp.perm == nil || e.acp.perm.info.RequestID != requestID || e.acp.perm.answered {
		return ErrPermissionStale
	}
	p := e.acp.perm
	for _, o := range p.info.Options {
		if o.OptionID == optionID {
			p.answered = true
			e.acp.userAllowed = strings.HasPrefix(o.Kind, "allow")
			p.answer <- acp.Selected(optionID) // buffered, never blocks: first and only send
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
	as.userAllowed = false
	clear(as.tools)
	sid := e.AgentSessionID
	item := e.acpTx.AddUser(text, origin)
	e.acpRes = acpResult{epoch: e.acpTx.Epoch(), promptID: item.ID}
	epoch := e.acpRes.epoch
	r.applyACPLocked(e, agentstate.Event{Kind: agentstate.KindPrompt, Text: text})
	r.broadcastACPLocked(e, []wire.AcpItem{item}, false)
	r.mu.Unlock()

	go func() {
		stop, err := as.agent.Prompt(context.Background(), sid, text)
		r.mu.Lock()
		defer r.mu.Unlock()
		as.busy = false
		as.userAllowed = false
		cur, ok := r.entries[id]
		if !ok {
			return
		}
		// A turn that ends — however it ends, the adapter dying with it
		// included — with nothing submitted has no result. Never read
		// as success (F3).
		if res := &cur.acpRes; res.epoch == epoch && res.promptID == item.ID && res.status == "" {
			res.status = wire.AcpResultNone
			r.broadcastACPLocked(cur, nil, false)
		}
		if cur.acp != as {
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

// SubmitResult records the typed result of session id's running turn,
// sent by its own submit server (or Pi extension) as SUBMIT_RESULT.
// nonce must be the one this adapter process was started with, so no
// other process holding the events socket — another session's agent
// included — can put a result in this session. result must be a JSON
// object of at most wire.MaxAcpResult bytes. A later submit in the same
// turn replaces an earlier one.
func (r *Registry) SubmitResult(id, nonce string, result json.RawMessage) error {
	var buf bytes.Buffer
	if len(result) > wire.MaxAcpResult || json.Compact(&buf, result) != nil || buf.Len() == 0 || buf.Bytes()[0] != '{' {
		return fmt.Errorf("%w: the result must be a JSON object of at most %d bytes", ErrSubmitRejected, wire.MaxAcpResult)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return ErrNotFound
	}
	if !e.isACP() {
		return ErrNotACP
	}
	as := e.acp
	if as == nil || nonce == "" || subtle.ConstantTimeCompare([]byte(nonce), []byte(as.nonce)) != 1 {
		return fmt.Errorf("%w: not this session's submit server", ErrSubmitRejected)
	}
	if !as.busy || e.acpRes.promptID == 0 {
		return fmt.Errorf("%w: no turn is running", ErrSubmitRejected)
	}
	e.acpRes.status, e.acpRes.value = wire.AcpResultSubmitted, json.RawMessage(buf.Bytes())
	r.broadcastACPLocked(e, nil, false)
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

// SendAcpTranscript queues an ACP session's whole transcript, as a
// reset, on ch — the caller's own listener from SubscribeACP — the
// answer to GET_ACP_TRANSCRIPT. It goes through the listener, under
// r.mu, rather than being written by the caller, so it is ordered with
// the deltas: every delta made before the snapshot is ahead of it on
// ch and every one after is behind it. Written directly, a delta from
// after the snapshot could reach the client first and be wiped by the
// reset, or one from before could follow it and be applied twice.
// A ch that is no longer subscribed (its connection is closing) gets
// nothing.
func (r *Registry) SendAcpTranscript(id string, ch AcpListener) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return ErrNotFound
	}
	if !e.isACP() {
		return ErrNotACP
	}
	if _, ok := r.acpListeners[ch]; ok {
		r.sendACPLocked(ch, acpMsgLocked(e, e.acpTx.Snapshot(), true))
	}
	return nil
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
	msg := wire.AcpTranscriptMsg{SessionID: e.ID, Epoch: e.acpTx.Epoch(), Reset: reset, Items: items,
		PromptID: e.acpRes.promptID, ResultStatus: e.acpRes.status, Result: e.acpRes.value}
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
		r.sendACPLocked(ch, msg)
	}
}

// sendACPLocked queues msg on one listener, dropping the listener if
// its buffer is full. Callers hold r.mu.
func (r *Registry) sendACPLocked(ch AcpListener, msg wire.AcpTranscriptMsg) {
	select {
	case ch <- msg:
	default:
		log.Printf("registry: dropping slow ACP transcript listener (buffer %d full); closing the channel, so the daemon hangs up on that client and it reconnects", cap(ch))
		delete(r.acpListeners, ch)
		close(ch)
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

// ACPNonceForTest returns the submit nonce of id's running adapter, for
// the daemon's tests, which play its submit server.
func (r *Registry) ACPNonceForTest(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[id]; ok && e.acp != nil {
		return e.acp.nonce
	}
	return ""
}
