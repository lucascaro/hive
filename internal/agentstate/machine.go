// Package agentstate holds the state machine that answers "what is this
// session doing right now" — working, idle, waiting on the user, dead.
//
// It is a pure domain package: no I/O, no locks, no clock of its own.
// Every method takes the time from its caller, which is what makes the
// whole thing table-testable and what lets the registry drive it from
// three unrelated feeders (PTY output, agent hooks, agent extensions)
// without any of them agreeing on a clock.
//
// The machine is deliberately not concurrency-safe. Its single owner is
// registry.Entry, which is already guarded by the registry mutex; adding
// a second lock here would only invite the lock-ordering bugs the
// registry's one-way session→registry rule exists to prevent.
package agentstate

import (
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// State is the coarse answer to "what is this session doing". It is the
// wire.State* set; the constants are re-exported here so the machine
// does not force every caller through the wire package.
type State = string

// Source records which tier produced the current State. It matters
// because the tiers are not equally trustworthy: a hook knows the agent
// asked a question, while the heuristic tier only knows bytes stopped
// arriving. The machine uses it to decide who wins, and clients render
// the difference in words — the state icon's tooltip ends "reported by
// the agent" or "guessed from terminal output" (see the frontend's
// stateTooltip). The glyph itself is identical on every tier.
type Source = string

// Tunables for the heuristic tier.
const (
	// QuietAfter is how long a session must emit nothing before the
	// heuristic tier calls it idle. Two seconds is longer than any
	// render pause inside an agent's streaming reply and shorter than
	// a human notices.
	QuietAfter = 2 * time.Second

	// HookStaleAfter is how long the machine keeps trusting a hook or
	// extension tier that has gone silent. Past it the heuristic tier
	// takes the session back — through Output if the screen is still
	// changing, through Tick if it has gone quiet — because otherwise a
	// crashed hook would pin a session at whatever state it last
	// reported, forever.
	//
	// The states this does NOT rescue are the ones that want the user:
	// on every tier, waiting_input, waiting_permission and error are
	// immune to elapsed time and to output, because neither is evidence
	// that the user saw them. They end only on an actual answer —
	// ClearWaiting, a later agent event (prompt / permission_resolved /
	// session_end, ...), or Exit.
	HookStaleAfter = 30 * time.Second
)

// Tunables for the Laya tier (spec 458).
const (
	// ClassifyQuietAfter is how long a changed screen must hold still
	// before it is worth classifying. Shorter than QuietAfter so a
	// classification usually lands in the same breath as the heuristic
	// idle, and long enough that a streaming reply is never sent.
	ClassifyQuietAfter = time.Second

	// LayaRecheckAfter bounds a Laya "working" on a screen that has
	// stopped changing. Tick does not time a Laya state out — that would
	// flip it idle and back on the next classification — so the
	// classifier asks again after this long instead. Only working is
	// rechecked: idle stays idle until output, and the waits stand
	// until the user acts.
	LayaRecheckAfter = 30 * time.Second
)

// Event kinds accepted by Apply. They are the agent-reported vocabulary,
// shared with wire.AgentEvent (phase 2 carries them over the socket);
// the heuristic tier never produces one.
const (
	KindPrompt = "prompt"
	// KindTurnEnd is the agent finishing a turn on its own. A finished
	// agent is waiting for the user's next move, so it lands in
	// waiting_input and raises attention.
	KindTurnEnd = "turn_end"
	// KindIdle is a plain stop that asks nothing of the user: the agent
	// is back at its prompt because the user just did something there
	// (answered a dialog outside a turn, ran /new). Reporting turn_end
	// for those would light up the session the user is already in.
	KindIdle               = "idle"
	KindWaitingInput       = "waiting_input"
	KindWaitingPermission  = "waiting_permission"
	KindPermissionResolved = "permission_resolved"
	KindError              = "error"
	KindSessionEnd         = "session_end"
	// KindPing changes no state. It exists so an agent can say "my
	// hooks are alive" — which promotes the session out of the
	// heuristic tier before its first real event, and refreshes
	// HookStaleAfter — and so an unknown or renamed hook event has
	// somewhere harmless to land.
	KindPing = "ping"
	// KindToolStart / KindToolEnd report one tool call. They keep the
	// working-state effect the collapsed KindPermissionResolved
	// carried: a tool running means the agent is working.
	KindToolStart = "tool_start"
	KindToolEnd   = "tool_end"
	// KindPlan replaces the agent's plan wholesale. It changes no
	// state — an agent revising its plan is not a state transition —
	// but it does refresh the tier clock like every other event.
	KindPlan = "plan"
	// KindPlanItem merges individual steps into the plan by ID — the
	// shape Claude's task tools report. Like KindPlan it changes no
	// state.
	KindPlanItem = "plan_item"
	// KindSubagentStart / KindSubagentEnd bracket one subagent's run,
	// keyed by Event.AgentID. They change no state; they feed
	// Snapshot.SubagentsRunning.
	KindSubagentStart = "subagent_start"
	KindSubagentEnd   = "subagent_end"
)

// Event is one agent-reported observation.
type Event struct {
	Kind   string
	Source Source
	At     time.Time
	Text   string // prompt / summary / error text; capped by Apply

	// Now is the DAEMON's clock at the moment the event was received,
	// as distinct from At, which is the reporter's claim about when it
	// observed the thing. Tool durations are measured across Now,
	// never across At: the two ends of a pair come from two separate
	// reporter processes and can straddle a clock adjustment.
	//
	// Zero falls back to At, so a caller that does not set it still
	// gets sane (if reporter-derived) timings.
	Now time.Time

	// Tool activity, carried by KindToolStart / KindToolEnd. Target is
	// the reporter-derived label — a basename, a command head, a URL
	// host — never a raw argument.
	Tool   string
	Target string
	CallID string
	OK     *bool

	// Items carries KindPlan.
	Items []wire.PlanItem

	// AgentID / AgentType tag an event from inside a subagent. A tagged
	// event is recorded — the ring keeps it, the count follows its
	// lifecycle — but never moves State, CurrentTool, the plan or the
	// plan-step tally: a subagent's work runs beside the parent's turn
	// and outlives it.
	AgentID   string
	AgentType string
	// RunningAgents rides KindTurnEnd: the subagents still running when
	// the turn ended. nil means not reported.
	RunningAgents *[]string

	// Instance / Seq are the Pi extension's ordering key
	// (wire.AgentEvent.Instance). Empty Instance means unkeyed: the
	// timestamp guard orders it, as it does every hook event.
	Instance string
	Seq      uint64
}

// Snapshot is the machine's externally visible state, as a value.
type Snapshot struct {
	State       State
	Source      Source
	LastPrompt  string
	LastSummary string

	// PlanDone / PlanTotal / CurrentTool are the compact activity
	// summary. They are part of Snapshot — and therefore part of the
	// "did anything change" comparison Apply returns — so a tool
	// starting or a plan step completing repaints the sidebar through
	// the announce path that already exists.
	//
	// Every field here must stay comparable with ==; Apply depends on
	// it. That is why the plan itself is reached through Activity()
	// rather than carried here as a slice.
	PlanDone    int
	PlanTotal   int
	CurrentTool string
	// SubagentsRunning counts subagents started and not yet ended.
	SubagentsRunning int
	// TurnEnded is true while State is waiting_input BECAUSE the agent
	// reported turn_end — it finished and is back at its prompt. A
	// question on screen, a bell, and a Laya classification are
	// waiting_input too but leave this false: typing at them would
	// answer, not start, a turn.
	TurnEnded bool
}

// Machine tracks one session. The zero value is not usable; call New.
type Machine struct {
	state       State
	source      Source
	lastPrompt  string
	lastSummary string

	lastOutputAt time.Time
	hookSeenAt   time.Time // zero ⇔ no hook/extension event ever seen
	// orderAt is the ordering guard's reference: the stamp of the last
	// MAIN-THREAD event applied. It is kept apart from hookSeenAt
	// because subagent hooks race the parent's — a subagent's
	// PostToolUse routinely lands milliseconds before the parent's
	// Stop — and letting one advance the guard would get the Stop
	// dropped as out of order.
	orderAt time.Time
	// reportedAt is the DAEMON's clock (Event.Now) at the last accepted
	// tier report, main-thread or subagent. It exists only for StaleAt:
	// hookSeenAt is the reporter's stamp and orders events, and a
	// deadline shown to a client must be on the clock the daemon owns.
	reportedAt time.Time
	// lastEventAt is the DAEMON's clock at the last accepted
	// state-bearing agent event: anything but a ping, a heartbeat Replay
	// or a late activity-only report. It is what "the agent's hooks are
	// still firing" means for the Laya tier. hookSeenAt cannot answer
	// that: the Pi extension's heartbeat refreshes it every few seconds
	// by re-sending an old report, so a Pi that missed a turn_end would
	// otherwise look live forever. Zero ⇔ no such event yet.
	lastEventAt time.Time
	// accepted records that the most recent Apply got past the ordering
	// guard. Consumed by TakeAccepted, like LastToolDelta's hasDelta, so
	// the registry never broadcasts a dropped event.
	accepted bool

	// extInstance / extSeq are the newest ordering key applied, and
	// extState is the state as of the extension tier's last say — the
	// keyed event's result, or a user's ClearWaiting or a bell that
	// followed it. A heartbeat replaying a seen key onto a session the
	// heuristic tier took over restores extState rather than re-running
	// the event, so a wait the user cleared stays cleared. See Replay.
	extInstance string
	extSeq      uint64
	extState    State
	// extTurnEnded rides with extState so Replay restores a finished
	// turn as finished.
	extTurnEnded bool
	// turnEnded: the current waiting_input came from KindTurnEnd. Only
	// meaningful while state is waiting_input (Snapshot gates on it);
	// every other way of changing state clears it.
	turnEnded bool

	// OSC 7501 tier (spec 517). oscSeenAt is the daemon clock at the last
	// status report, applied or not; it bounds how long a report shields
	// the state from byte-level observation. oscWorking is true between
	// an osc working and the next osc report that ends it — the "same
	// turn" an interrupt edge needs. interrupted/interruptedAt latch an
	// Esc: the hook processes the interrupt itself fires (a tool's
	// PostToolUseFailure, a late permission request) are stamped after it
	// and must not resurrect the turn.
	oscSeenAt     time.Time
	oscWorking    bool
	interrupted   bool
	interruptedAt time.Time

	// act is the tool ring and plan snapshot. Its lifetime is this
	// struct's: New zeroes it, and a fresh Machine on restart/revive
	// discards it. See activity.go.
	act activity
}

// New returns a machine for a session that has just come into
// existence: idle, on the heuristic tier, with nothing said yet. Revive
// and Restart build a fresh one rather than reusing the old, which is
// what makes "a daemon restart starts every session idle" true without
// any clearing code.
func New(now time.Time) *Machine {
	return &Machine{
		state:        wire.StateIdle,
		source:       wire.StateSourceHeuristic,
		lastOutputAt: now,
	}
}

// Snapshot returns the current state as a value.
func (m *Machine) Snapshot() Snapshot {
	done, total, current := m.planSummary()
	return Snapshot{
		SubagentsRunning: len(m.act.subagents),
		State:            m.state,
		Source:           m.source,
		LastPrompt:       m.lastPrompt,
		LastSummary:      m.lastSummary,
		PlanDone:         done,
		PlanTotal:        total,
		CurrentTool:      current,
		TurnEnded:        m.turnEnded && m.state == wire.StateWaitingInput,
	}
}

// trusted reports whether a non-heuristic tier is currently in charge
// and still fresh. While it is, byte-level observations (output, bell)
// are ignored: a streaming reply is already "working", and a permission
// prompt repainting itself must not flip waiting_permission back.
func (m *Machine) trusted(now time.Time) bool {
	// The ACP tier never goes stale: the daemon owns the ACP stream, so
	// a quiet stream is a quiet agent (a long tool call, a slow model),
	// not a reporter that stopped. Liveness comes from the adapter
	// process exiting, which ends in Exit.
	if m.source == wire.StateSourceACP {
		return true
	}
	// An osc state is a byte-level report, forgeable and heartbeat-less:
	// never "a tier in charge". Its own shield is oscFresh, kept separate
	// so a heartbeating Pi refreshing hookSeenAt cannot make a stale osc
	// state look trusted forever.
	if m.source == wire.StateSourceOSC {
		return false
	}
	if m.source == wire.StateSourceHeuristic || m.hookSeenAt.IsZero() {
		return false
	}
	return now.Sub(m.hookSeenAt) <= HookStaleAfter
}

// Output records that the session's visible screen changed. Returns
// whether the state changed with it.
//
// The caller samples a screen digest rather than watching the byte
// stream, because those are different questions: an idle agent TUI can
// write continuously (cursor-position queries, cursor blinks) without
// changing a single cell. See session.VT.ScreenDigest.
func (m *Machine) Output(now time.Time) bool {
	if m.state == wire.StateExited {
		return false
	}
	// A session that asked for the user stays asking until it is
	// actually answered, and one that failed stays failed until someone
	// has seen it. Redrawing is neither — and a program that rings and
	// then keeps painting would otherwise bury its own request within
	// one tick. The ways out are ClearWaiting (driven by the client that
	// sees the user look), a later agent event, and Exit; output is not
	// one of them.
	//
	// A Laya wait is the exception: it is Hive's guess from the screen,
	// not the program asking, and a guess about a screen that has since
	// changed is about the past. Left standing, a misread would pulse
	// until the user looked and hide every screen after it (spec 458).
	if wantsUser(m.state) && m.source != wire.StateSourceLaya {
		m.lastOutputAt = now
		return false
	}
	m.lastOutputAt = now
	if m.trusted(now) {
		return false
	}
	// A fresh osc report is exact; the repaint that follows it (Claude
	// redraws "Interrupted" right after Esc) must not flip it back to a
	// guess. Once osc goes quiet it falls through like any stale tier.
	if m.source == wire.StateSourceOSC && m.oscFresh(now) {
		return false
	}
	// Either we were already heuristic, or the tier that claimed this
	// session has gone quiet for longer than HookStaleAfter while bytes
	// kept coming. Take it back.
	changed := m.state != wire.StateWorking || m.source != wire.StateSourceHeuristic
	m.state = wire.StateWorking
	m.turnEnded = false
	m.source = wire.StateSourceHeuristic
	return changed
}

// Bell records a terminal bell: the program wants the user, so
// waiting_input — on EVERY tier. A hooked agent's finished turn already
// lands in waiting_input, but a program under a trusted tier can still
// ring for reasons no hook reports, and dropping that is what once
// broke the alert people rely on. A state that already wants the user
// (either wait, or an error) absorbs the bell, so a permission prompt
// that also rings notifies once.
//
// The tier is left alone: a bell says nothing about who owns the
// session, and demoting a hooked session over one would hand its next
// redraw to the heuristic tier.
//
// Except from Laya: a bell is the program itself asking, not a guess,
// so its wait must not stay labelled as one Laya may revise or Output
// may clear. It goes to the extension tier when one is keyed on this
// session — as its last say, which is exactly what extState records, so
// a heartbeat restores the wait instead of the stale report — and to
// the heuristic tier otherwise.
func (m *Machine) Bell(now time.Time) bool {
	if m.state != wire.StateIdle && m.state != wire.StateWorking {
		return false
	}
	if m.source == wire.StateSourceLaya {
		if m.extInstance != "" {
			m.source = wire.StateSourceExtension
		} else {
			m.source = wire.StateSourceHeuristic
		}
	}
	m.state = wire.StateWaitingInput
	m.turnEnded = false
	m.noteExtState()
	return true
}

// Exit records that the child process is gone. The exit code is
// deliberately not consulted: error is reserved for failures the agent
// itself reported, so a shell exiting 1 is exited, not error. Keeping
// them apart is what stops a red dot from meaning nothing.
func (m *Machine) Exit() bool {
	if m.state == wire.StateExited {
		return false
	}
	m.state = wire.StateExited
	m.act.clearSubagents()
	return true
}

// ClearWaiting resolves either wait, or an agent-reported error, once the
// user has looked (a wait for permission resumes working). It is the "the user
// has now acted on this session" transition, which only a client can
// observe, and it applies on every tier and to both kinds of wait: a
// keystroke into a permission dialog IS the answer, and a dismissed
// dialog does not always produce an agent event (a declined question
// tool ends the turn with nothing Hive hears). Under-alerting for one
// keystroke beats a session lit "waiting for permission" for an hour.
//
// Deliberately not folded into Apply: this is not something an agent
// reported, so it must not touch the tier or the staleness clock.
func (m *Machine) ClearWaiting() bool {
	m.turnEnded = false
	switch m.state {
	case wire.StateWaitingInput, wire.StateError:
		// Nothing runs until the next prompt is submitted.
		m.state = wire.StateIdle
	case wire.StateWaitingPermission:
		// The agent was mid-turn and the answer resumes it. Claude fires
		// no hook between "allowed" and the tool finishing (PreToolUse
		// runs BEFORE the dialog), so this keystroke is the only signal
		// that the tool is now running.
		m.state = wire.StateWorking
		if m.interrupted {
			// Except when the dialog was dismissed with Esc: the turn
			// is over, nothing is running.
			m.state = wire.StateIdle
		}
	default:
		return false
	}
	m.noteExtState()
	return true
}

// noteExtState records a state change made while the extension tier
// owns the session, so a later restore (Replay) brings back what the
// user last saw rather than the event that preceded it.
func (m *Machine) noteExtState() {
	if m.source == wire.StateSourceExtension {
		m.extState = m.state
		m.extTurnEnded = m.turnEnded
	}
}

// Replay handles an extension event whose ordering key was already
// applied — the extension's heartbeat re-sending its latest report
// (spec 423). handled is false for anything else, which then goes
// through Apply.
//
// On the extension tier a replay is proof of life and nothing more:
// no state, no text, no tool or plan change — only the liveness clocks
// (and the accepted flag, so clients get a bare staleness refresh). After a stall long enough for the
// heuristic tier to take the session (HookStaleAfter, then a repaint),
// it restores the tier and extState instead of re-running the event:
// re-running a turn_end would re-raise a wait the user cleared, and a
// tool_start would duplicate its ring entry.
//
// Liveness is the daemon's clock, not ev.At: a replay carries the
// original stamp, which is as old as the report it repeats.
func (m *Machine) Replay(ev Event) (handled, changed bool) {
	if ev.Instance == "" || ev.Instance != m.extInstance || ev.Seq > m.extSeq {
		return false, false
	}
	now := ev.Now
	if now.IsZero() {
		now = ev.At
	}
	m.hookSeenAt = now
	m.reportedAt = now
	if m.state == wire.StateExited {
		return true, false
	}
	m.accepted = true
	// A Laya classification replaced this session's stale extension
	// state; the heartbeat is repeating the report Laya corrected, so it
	// proves liveness and nothing else. The next real event takes the
	// session back through Apply.
	if m.source == wire.StateSourceExtension || m.source == wire.StateSourceLaya ||
		m.source == wire.StateSourceOSC {
		return true, false
	}
	before := m.Snapshot()
	m.source = wire.StateSourceExtension
	m.state = m.extState
	m.turnEnded = m.extTurnEnded
	return true, m.Snapshot() != before
}

// Tick applies the passage of time: a working session that has emitted
// nothing for QuietAfter has finished its turn. Only the heuristic tier
// times out — a trusted tier reports its own turn_end, and inventing
// one for it would race the real thing.
func (m *Machine) Tick(now time.Time) bool {
	// A Laya working is bounded by LayaRecheckAfter, not by silence:
	// timing it out here would flip it idle until the next
	// classification put it back.
	if m.state != wire.StateWorking || m.source == wire.StateSourceLaya || m.trusted(now) {
		return false
	}
	if now.Sub(m.lastOutputAt) < QuietAfter {
		return false
	}
	m.state = wire.StateIdle
	m.turnEnded = false
	// The tier is stale by definition here — trusted() said so above —
	// so this idle is ours, inferred from silence, not something the
	// agent reported. Say so, the way Output does when it takes a
	// stale session back. Without this the session reports idle while
	// still claiming state_source=hook, and every surface that renders
	// provenance (the tooltip's last line) credits the agent with a
	// state it never sent.
	m.source = wire.StateSourceHeuristic
	return true
}

// Apply folds in one agent-reported event. Any event promotes the
// session to the event's tier and refreshes the staleness clock, even
// when it changes no state — that is what KindPing is for.
func (m *Machine) Apply(ev Event) bool {
	// Out-of-order guard. A report is one short-lived connection —
	// `hived hook` is a whole process per Claude hook event, and the Pi
	// extension opens one socket per event — and the daemon serves each
	// on its own goroutine, so two events reported milliseconds apart
	// can reach Apply in either order. An inverted pair is a wrong glyph
	// that nothing corrects until the next event or HookStaleAfter.
	//
	// ev.At is comparable to what is already recorded: the reporter
	// reached us over a unix socket, so it shares this host's clock.
	// An event older than the last one applied describes a moment that
	// has already passed, and it is dropped whole — including the
	// staleness clock, which is already newer. Equal stamps apply, since
	// nothing distinguishes them.
	//
	// The window is bounded, and that bound is load-bearing. ev.At is
	// parsed from the wire and carries no monotonic reading, so this is
	// wall-clock arithmetic: if the host's clock steps backward (a VM
	// resume, a large NTP correction), an unbounded guard would drop
	// every later event for the whole duration of the step, while
	// trusted() computed a negative age that never exceeds
	// HookStaleAfter — so nothing would correct it. A delivery inversion
	// is milliseconds apart; a gap of HookStaleAfter or more is a clock
	// that moved, and the newest report wins.
	//
	// Accepting the far-behind event restores recovery for working and
	// idle, which is where a wedge actually strands a user: Observe
	// reclaims on the next byte, and Tick times a stale working out. It
	// does NOT recover a wait or an error — Observe returns early on
	// waiting_input/waiting_permission/error by design (a prompt repainting
	// itself must not clear itself) and Tick only demotes working — so a
	// report landing HookStaleAfter or more behind, on a wait, pins that
	// wait until ClearWaiting or the next agent event. Reaching that
	// needs a reporter stalled longer than the staleness horizon: the Pi
	// extension destroys its connection after 2s, so only a
	// pathologically wedged `hived hook` qualifies. Left as-is rather
	// than special-cased — a state-dependent rewind rule is more machine
	// for a case that is hard to reach and self-corrects on the next
	// event.
	// The daemon's own clock, for tool durations. See Event.Now.
	now := ev.Now
	if now.IsZero() {
		now = ev.At
	}

	// Subagent events skip the guard: their handling is
	// order-independent (set semantics for the count, CallID pairing for
	// tools) and they never move state, so there is nothing for an
	// inversion to get wrong.
	sub := ev.AgentID != ""
	// A keyed event is ordered by its key, which Replay has already
	// checked; the stamp says nothing more.
	keyed := ev.Instance != ""
	if !sub && !keyed && !m.orderAt.IsZero() {
		if behind := m.orderAt.Sub(ev.At); behind > 0 && behind < HookStaleAfter {
			return m.applyLateActivity(ev, now)
		}
	}

	before := m.Snapshot()
	// Liveness counts even for a session that has exited (the tier
	// clock below is refreshed too); acceptance waits for the exit check.
	m.reportedAt = now

	// An event that moves no state says nothing about the state Laya
	// classified, so it must not relabel Laya's guess as the agent's
	// report: that would make a Laya wait sticky, and unrevisable.
	if m.source != wire.StateSourceLaya || !stateless(ev) {
		m.source = ev.Source
	}
	if sub {
		// Liveness only, never backwards: a subagent still reporting
		// keeps the hook tier trusted after the parent's turn ended.
		if ev.At.After(m.hookSeenAt) {
			m.hookSeenAt = ev.At
		}
	} else if keyed {
		// Liveness from our clock (a healed report carries an old
		// stamp). orderAt is max'd from ev.At, never ev.Now: a keyed
		// tool_end and the unkeyed plan on its connection share one
		// stamp, and the plan must not sort behind it. Max, so a healed
		// old stamp never drags the guard backwards.
		m.hookSeenAt = now
		if ev.At.After(m.orderAt) {
			m.orderAt = ev.At
		}
		if ev.Instance != m.extInstance || ev.Seq > m.extSeq {
			m.extInstance, m.extSeq = ev.Instance, ev.Seq
		}
	} else {
		// Assigned, not max'd: a main-thread report HookStaleAfter or
		// more behind is a clock that stepped, and the newest report
		// wins — see the guard above.
		m.hookSeenAt = ev.At
		m.orderAt = ev.At
	}
	text := truncate(ev.Text)

	// Exit is terminal, on every feeder. A hook process still in flight
	// when the PTY dies lands after it — Claude fires SessionEnd and
	// the child exits, and the two race — so without this an event
	// resurrects a dead session to working or, worse, to waiting_input:
	// needs_attention true for a session ClearWaiting can never clear,
	// because clearing it needs a user to type into a PTY that is gone.
	// The tier clock is still refreshed above, so a revived session
	// starts from a fresh Machine and not from a stale one.
	if m.state == wire.StateExited {
		return false
	}
	m.accepted = true
	if ev.Kind != KindPing {
		m.lastEventAt = now
	}

	if sub {
		m.applySubagent(ev, now)
		return m.Snapshot() != before
	}

	// An Esc latched by the osc tier (see OSC) outlives the interrupt by
	// the hooks it fires: they are stamped after it, so the ordering
	// guard above cannot catch them. Until the next turn begins or ends,
	// they record activity but do not move the state back.
	prevState, prevTurn := m.state, m.turnEnded
	latched := m.interruptLatched(now)
	switch ev.Kind {
	case KindPrompt, KindTurnEnd, KindSessionEnd:
		m.interrupted, latched = false, false
	}
	switch ev.Kind {
	case KindTurnEnd, KindWaitingInput, KindPing, KindPlan, KindPlanItem:
		// These leave a finished turn finished (waiting_input after
		// turn_end stays a turn end through Claude's idle_prompt).
	default:
		m.turnEnded = false
	}
	switch ev.Kind {
	case KindPrompt:
		m.state = wire.StateWorking
		// First prompt only: the question the session was opened to
		// answer is what the user is scanning a list of ten sessions
		// for, and it is the one thing that stops being visible as
		// soon as the conversation scrolls.
		if m.lastPrompt == "" {
			m.lastPrompt = text
		}
	case KindTurnEnd:
		m.state = wire.StateWaitingInput
		m.turnEnded = true
		m.lastSummary = text
		m.act.endTurn(ev.At)
		if ev.RunningAgents != nil {
			m.act.reconcileSubagents(*ev.RunningAgents, ev.At)
		}
	case KindIdle:
		m.state = wire.StateIdle
		m.lastSummary = text
		m.act.endTurn(ev.At)
	case KindWaitingInput:
		// An error already wants the user, and says more. Claude fires
		// Notification(idle_prompt) ~60s after ANY turn, a failed one
		// included, so without this the red cross turned into a plain
		// "waiting for you" before the user ever saw it.
		if m.state != wire.StateError {
			m.state = wire.StateWaitingInput
		}
	case KindWaitingPermission:
		m.state = wire.StateWaitingPermission
	case KindPermissionResolved:
		m.state = wire.StateWorking
	case KindToolStart:
		// Same state effect as the permission_resolved these were
		// split out of: a tool running means the agent is working.
		m.state = wire.StateWorking
		m.toolStart(ev, now)
	case KindToolEnd:
		m.state = wire.StateWorking
		m.toolEnd(ev, now)
	case KindPlan:
		// No state change by design: revising a plan is not a
		// transition. The tier clock was refreshed above.
		m.setPlan(ev.Items, ev.At)
	case KindPlanItem:
		m.mergePlanItems(ev.Items, ev.At)
	case KindError:
		m.state = wire.StateError
		m.lastSummary = text
		m.act.endTurn(ev.At)
	case KindSessionEnd:
		m.state = wire.StateExited
		m.act.endTurn(ev.At)
		m.act.clearSubagents()
	case KindPing:
		// No state change by design.
	default:
		// Tolerant parsing: an unknown or renamed event keeps the tier
		// alive rather than dropping the session back to heuristics.
	}

	if latched {
		switch ev.Kind {
		case KindToolStart, KindToolEnd, KindPermissionResolved,
			KindWaitingInput, KindWaitingPermission, KindIdle:
			m.state, m.turnEnded = prevState, prevTurn
		}
	}

	if keyed {
		m.extState = m.state
		m.extTurnEnded = m.turnEnded
	}
	return m.Snapshot() != before
}

// StaleAt is the daemon-clock instant past which this session's tier
// counts as silent: the last accepted report plus HookStaleAfter. ok is
// false on the heuristic tier, where there is nothing to go stale.
//
// Silence is only meaningful while working. Neither Claude's hooks nor
// the Pi extension heartbeat, so a healthy tier at rest is exactly as
// quiet as a dead one; clients apply the deadline to working sessions
// only.
func (m *Machine) StaleAt() (time.Time, bool) {
	// Laya reports nothing, so it has no report deadline — its own
	// staleness is LayaRecheckAfter, applied by the classifier.
	// ACP has no deadline either: see trusted.
	if m.source == wire.StateSourceHeuristic || m.source == wire.StateSourceLaya ||
		m.source == wire.StateSourceACP || m.source == wire.StateSourceOSC || m.reportedAt.IsZero() {
		return time.Time{}, false
	}
	return m.reportedAt.Add(HookStaleAfter), true
}

// Classifiable reports whether a Laya classification may replace the
// current state: the session is alive, not waiting on the user (an
// agent's or a bell's wait stands until the user acts, as it does
// against Output; only Laya's own guess may be revised), and no tier is currently speaking for it — either none is
// trusted, or the one that is has sent nothing but heartbeats and pings
// for HookStaleAfter. See lastEventAt for why liveness is not enough.
func (m *Machine) Classifiable(now time.Time) bool {
	if m.state == wire.StateExited {
		return false
	}
	// Laya may revise its own wait — on a heartbeating Pi, Output
	// cannot clear it (the tier is still trusted), so a changed screen
	// is re-asked instead. Every other wait is the user's to answer.
	if wantsUser(m.state) && m.source != wire.StateSourceLaya {
		return false
	}
	// A fresh osc report is an exact reading; a screen guess must not
	// overwrite it.
	if m.source == wire.StateSourceOSC && m.oscFresh(now) {
		return false
	}
	if !m.trusted(now) {
		return true
	}
	return m.lastEventAt.IsZero() || now.Sub(m.lastEventAt) > HookStaleAfter
}

// Classify applies a Laya classification of the visible screen. It
// changes nothing unless the session is Classifiable and s is one of the
// five states a screen can show; it never touches the tier clocks
// (hookSeenAt, orderAt, reportedAt, lastEventAt), so a stale tier stays
// stale and its next real event takes the session back through Apply.
func (m *Machine) Classify(s State, now time.Time) bool {
	switch s {
	case wire.StateWorking, wire.StateIdle, wire.StateWaitingInput,
		wire.StateWaitingPermission, wire.StateError:
	default:
		return false
	}
	if !m.Classifiable(now) {
		return false
	}
	if m.state == s && m.source == wire.StateSourceLaya {
		return false
	}
	m.state = s
	m.turnEnded = false
	m.source = wire.StateSourceLaya
	return true
}

// stateless reports whether ev is one Apply never moves State for.
func stateless(ev Event) bool {
	if ev.AgentID != "" {
		return true
	}
	switch ev.Kind {
	case KindPing, KindPlan, KindPlanItem:
		return true
	}
	return false
}

// LastEventAt is the daemon-clock time of the last state-bearing agent
// event (zero when none). See lastEventAt.
func (m *Machine) LastEventAt() time.Time { return m.lastEventAt }

// QuietFor is how long the screen has gone unchanged, as last reported
// through Output.
func (m *Machine) QuietFor(now time.Time) time.Duration {
	return now.Sub(m.lastOutputAt)
}

// TakeAccepted reports whether the most recent Apply accepted its event,
// and consumes the flag.
func (m *Machine) TakeAccepted() bool {
	ok := m.accepted
	m.accepted = false
	return ok
}

// wantsUser reports the states that stand until the user acts on them:
// the two waits and an agent-reported error.
func wantsUser(s State) bool {
	return s == wire.StateWaitingInput || s == wire.StateWaitingPermission || s == wire.StateError
}

// truncate caps agent-supplied text at the wire limit. The content is
// attacker-influenced in the ordinary sense — it is whatever was typed
// at a prompt or printed by a tool — and it is rebroadcast to every
// connected client, so it is bounded here rather than trusted.
//
// Byte-slice, then drop any partial rune left at the tail — exactly
// what registry.truncateTitle does for Title. The two are the same
// kind of attacker-influenced text and must not sanitize differently.
func truncate(s string) string {
	if len(s) <= wire.MaxSummaryLen {
		return s
	}
	return strings.ToValidUTF8(s[:wire.MaxSummaryLen], "")
}

// interruptLatch bounds how long an Esc keeps late hook events from
// moving the state. The processes an interrupt fires report within a
// second or two; the bound keeps a latch that nothing cleared (no new
// prompt, no turn end) from muting the agent's own later reports.
const interruptLatch = 10 * time.Second

func (m *Machine) interruptLatched(now time.Time) bool {
	return m.interrupted && now.Sub(m.interruptedAt) <= interruptLatch
}

// oscFresh reports whether an osc report arrived within HookStaleAfter.
func (m *Machine) oscFresh(now time.Time) bool {
	return !m.oscSeenAt.IsZero() && now.Sub(m.oscSeenAt) <= HookStaleAfter
}

// OSC folds in one OSC 7501 status report read from the PTY stream
// (spec 517) and reports whether the visible state changed.
//
// It is the lowest-trust tier, and it fills gaps: a report applies only
// when no tier is speaking for the session — none trusted, or the
// trusted one silent of state-bearing events for HookStaleAfter, the
// Classifiable rule — and never over an ACP session, an exited one, or a
// wait that a hook, extension or the bell raised (only Laya's guess and
// osc's own waits may be revised by it). It never touches the tier
// clocks, so a stale hook stays stale and its next event takes the
// session straight back through Apply.
//
// The one exception is Esc. Claude fires no hook when the user
// interrupts, so a hook-driven turn would read "working" until the
// staleness timeout. An osc idle that ends an osc working in the same
// turn therefore ends a working state on any non-ACP tier, keeping that
// tier's label, and latches the interrupt (see Apply). It never lowers a
// wait and never raises anything.
func (m *Machine) OSC(st, kind string, now time.Time) bool {
	if m.state == wire.StateExited {
		return false
	}
	m.oscSeenAt = now
	before := m.Snapshot()

	var to State
	turnEnded := false
	switch st {
	case "working":
		to = wire.StateWorking
	case "idle":
		to = wire.StateIdle
	case "done":
		// The turn finished, like a hook turn_end: the user's move.
		to, turnEnded = wire.StateWaitingInput, true
	case "blocked":
		to = wire.StateWaitingInput
		if kind == "permission" {
			to = wire.StateWaitingPermission
		}
	case "error":
		to = wire.StateError
	default:
		// clear (the program is shutting down), or a state this version
		// does not know: not something to act on.
		m.oscWorking = false
		return false
	}

	// oscWorking spans the turn: a blocked report (a dialog inside the
	// turn) keeps it, so Esc on that dialog still reads as an interrupt.
	wasWorking := m.oscWorking
	switch st {
	case "working":
		m.oscWorking = true
	case "blocked":
	default:
		m.oscWorking = false
	}
	if st == "working" {
		m.interrupted = false
	}
	if st == "idle" && wasWorking {
		m.interrupted, m.interruptedAt = true, now
		if m.state == wire.StateWorking && m.source != wire.StateSourceACP {
			m.state = wire.StateIdle
			m.turnEnded = false
			m.noteExtState()
			return m.Snapshot() != before
		}
	}

	if m.source == wire.StateSourceACP {
		return false
	}
	if wantsUser(m.state) && m.source != wire.StateSourceOSC && m.source != wire.StateSourceLaya {
		return false
	}
	if m.trusted(now) && !m.lastEventAt.IsZero() && now.Sub(m.lastEventAt) <= HookStaleAfter {
		return false
	}
	// A live tier that has only pinged (Claude's SessionStart) is not yet
	// speaking, so osc may fill in — but restating the state it already
	// holds is not a gap, and must not take the label from the tier. The
	// startup `idle` on a fresh hooked session is exactly this.
	if m.trusted(now) && m.state == to && m.turnEnded == turnEnded {
		return false
	}
	m.state = to
	m.turnEnded = turnEnded
	// A live tier keeps its label: its hooks are installed and answering
	// (it has pinged, or just heartbeated), so the session is still that
	// tier's — osc only supplied a state the tier has not yet reported.
	// Relabelling would swap "reported by the agent" for "read from the
	// terminal" on every startup, since Claude prints a working/idle pair
	// before its first hook event.
	liveTier := m.trusted(now) && (m.source == wire.StateSourceHook || m.source == wire.StateSourceExtension)
	if !liveTier {
		m.source = wire.StateSourceOSC
	}
	return m.Snapshot() != before
}
