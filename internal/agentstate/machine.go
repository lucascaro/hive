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
// arriving. Clients render the difference (an "uncertain" ring on
// heuristic states) and the machine uses it to decide who wins.
type Source = string

// Tunables for the heuristic tier.
const (
	// QuietAfter is how long a session must emit nothing before the
	// heuristic tier calls it idle. Two seconds is longer than any
	// render pause inside an agent's streaming reply and shorter than
	// a human notices.
	QuietAfter = 2 * time.Second

	// HookStaleAfter is how long the machine keeps trusting a hook or
	// extension tier that has gone silent while output keeps arriving.
	// Past it, output demotes the session back to the heuristic tier —
	// otherwise a crashed hook would pin a session at whatever state it
	// last reported, forever.
	HookStaleAfter = 30 * time.Second
)

// Event kinds accepted by Apply. They are the agent-reported vocabulary,
// shared with wire.AgentEvent (phase 2 carries them over the socket);
// the heuristic tier never produces one.
const (
	KindPrompt             = "prompt"
	KindTurnEnd            = "turn_end"
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
)

// Event is one agent-reported observation.
type Event struct {
	Kind   string
	Source Source
	At     time.Time
	Text   string // prompt / summary / error text; capped by Apply
}

// Snapshot is the machine's externally visible state, as a value.
type Snapshot struct {
	State       State
	Source      Source
	LastPrompt  string
	LastSummary string
}

// Machine tracks one session. The zero value is not usable; call New.
type Machine struct {
	state       State
	source      Source
	lastPrompt  string
	lastSummary string

	lastOutputAt time.Time
	hookSeenAt   time.Time // zero ⇔ no hook/extension event ever seen
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
	return Snapshot{
		State:       m.state,
		Source:      m.source,
		LastPrompt:  m.lastPrompt,
		LastSummary: m.lastSummary,
	}
}

// trusted reports whether a non-heuristic tier is currently in charge
// and still fresh. While it is, byte-level observations (output, bell)
// are ignored: a streaming reply is already "working", and a permission
// prompt repainting itself must not flip waiting_permission back.
func (m *Machine) trusted(now time.Time) bool {
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
	// A session that asked for the user stays asking until the user
	// answers. Redrawing is not an answer — and a program that rings
	// and then keeps painting would otherwise bury its own request
	// within one tick. ClearWaiting, driven by the client that sees
	// the user look, is the only way out.
	if m.state == wire.StateWaitingInput || m.state == wire.StateWaitingPermission {
		m.lastOutputAt = now
		return false
	}
	m.lastOutputAt = now
	if m.trusted(now) {
		return false
	}
	// Either we were already heuristic, or the tier that claimed this
	// session has gone quiet for longer than HookStaleAfter while bytes
	// kept coming. Take it back.
	changed := m.state != wire.StateWorking || m.source != wire.StateSourceHeuristic
	m.state = wire.StateWorking
	m.source = wire.StateSourceHeuristic
	return changed
}

// Bell records a terminal bell: the program wants the user, so
// waiting_input — on EVERY tier. A hooked agent rings when it finishes
// a turn, and its Stop hook maps to idle, which raises nothing; the
// bell is the only "come look" a finished turn produces, and dropping
// it on the trusted tier is exactly what broke the alert people rely
// on today. A wait already in progress (either kind) absorbs the bell,
// so a permission prompt that also rings notifies once.
//
// The tier is left alone: a bell says nothing about who owns the
// session, and demoting a hooked session over one would hand its next
// redraw to the heuristic tier.
func (m *Machine) Bell(now time.Time) bool {
	if m.state != wire.StateIdle && m.state != wire.StateWorking {
		return false
	}
	m.state = wire.StateWaitingInput
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
	return true
}

// ClearWaiting resolves either wait back to idle. It is the "the user
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
	switch m.state {
	case wire.StateWaitingInput:
		// Nothing runs until the next prompt is submitted.
		m.state = wire.StateIdle
	case wire.StateWaitingPermission:
		// The agent was mid-turn and the answer resumes it. Claude fires
		// no hook between "allowed" and the tool finishing (PreToolUse
		// runs BEFORE the dialog), so this keystroke is the only signal
		// that the tool is now running.
		m.state = wire.StateWorking
	default:
		return false
	}
	return true
}

// Tick applies the passage of time: a working session that has emitted
// nothing for QuietAfter has finished its turn. Only the heuristic tier
// times out — a trusted tier reports its own turn_end, and inventing
// one for it would race the real thing.
func (m *Machine) Tick(now time.Time) bool {
	if m.state != wire.StateWorking || m.trusted(now) {
		return false
	}
	if now.Sub(m.lastOutputAt) < QuietAfter {
		return false
	}
	m.state = wire.StateIdle
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
	if !m.hookSeenAt.IsZero() && ev.At.Before(m.hookSeenAt) {
		return false
	}

	before := m.Snapshot()

	m.source = ev.Source
	m.hookSeenAt = ev.At
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
		m.state = wire.StateIdle
		m.lastSummary = text
	case KindWaitingInput:
		m.state = wire.StateWaitingInput
	case KindWaitingPermission:
		m.state = wire.StateWaitingPermission
	case KindPermissionResolved:
		m.state = wire.StateWorking
	case KindError:
		m.state = wire.StateError
		m.lastSummary = text
	case KindSessionEnd:
		m.state = wire.StateExited
	case KindPing:
		// No state change by design.
	default:
		// Tolerant parsing: an unknown or renamed event keeps the tier
		// alive rather than dropping the session back to heuristics.
	}

	return m.Snapshot() != before
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
