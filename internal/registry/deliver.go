package registry

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/agentstate"
	"github.com/lucascaro/hive/internal/wire"
)

// Session messaging (spec 338): hand a running session a note, and get
// told when it stops working.
//
// Five strategies, chosen by the target:
//   - socket/extension: a live Claude Code session takes the note through
//     its own inbox, a Pi session through its Hive extension's, working or
//     idle (see tryInbox); any failure falls through.
//   - acp:    an ACP-kind session takes the note as a user turn.
//   - typed:  a PTY session that is idle gets the text and Enter.
//   - queued: a PTY session that is busy holds the text until its next
//     idle transition, when the caller asked for that.
//
// What "idle" means here is whatever agentstate reports. For an agent
// with a hook tier that is a real signal; for a plain shell or an
// unhooked agent it is "no output for QuietAfter", which a program
// reading stdin satisfies just as well as a prompt does. The typed path
// is therefore a best effort, and says so to the user.

var (
	// ErrMessageInvalid is returned for text that is empty once
	// sanitised, or over wire.MaxMessageLen.
	ErrMessageInvalid = errors.New("registry: message is empty or too long")
	// ErrSessionBusy is returned when the target is not idle and the
	// caller did not ask to queue (or cannot: ACP sessions have no queue).
	ErrSessionBusy = errors.New("registry: session is busy")
	// ErrDeliveryFailed is returned when the PTY write fails or the
	// target's queue is full.
	ErrDeliveryFailed = errors.New("registry: delivery failed")
)

const (
	// maxQueuedMessages and maxQueuedBytes bound one session's queue, so
	// a runaway `hived msg` loop against a busy session cannot grow
	// memory without limit.
	maxQueuedMessages = 16
	maxQueuedBytes    = wire.MaxMessageLen
	// drainGrace is how long a session must STAY idle after the
	// transition before one queued message is typed. The idle edge
	// itself is not a promise that the prompt is ready.
	drainGrace = 300 * time.Millisecond
	// typedHold is how long after typing a message the session still
	// counts as busy. A hook-tier agent takes a moment to report the
	// prompt it was just handed, and until it does its state still reads
	// "settled"; without the hold a second note would be typed on top.
	typedHold = time.Second
)

// settled reports whether a session is at a prompt that is safe to type
// into: idle, or waiting_input because the agent itself reported
// turn_end. Plain waiting_input is not enough — that is also a question
// on screen, and typing at it answers the question.
func settled(s agentstate.Snapshot) bool {
	return s.State == wire.StateIdle || s.TurnEnded
}

// settledLocked is settled plus the hold after our own last write.
func (e *Entry) settledLocked(now time.Time) bool {
	return settled(e.stateSnapshot()) && now.Sub(e.typedAt) >= typedHold
}

// sanitizeInput prepares message text for a PTY write. It is not
// typedPrompt: that collapses all whitespace and, on Windows, strips
// characters argv cannot carry, both wrong for a note someone chose
// the words of. What it must do is keep the text from being anything
// but text: ESC, the other C0 controls (bar \n and \t) and DEL are
// dropped, so a message cannot inject an escape sequence or a
// bracketed-paste terminator into whatever TUI is on the other end.
func sanitizeInput(s string) (string, error) {
	if len(s) > wire.MaxMessageLen || !utf8.ValidString(s) {
		return "", ErrMessageInvalid
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ErrMessageInvalid
	}
	return s, nil
}

// SendToSession delivers text to a session. origin says who sent it
// (wire.OriginUser, or a principal); only the ACP path records it today.
// Returns the wire.Delivery* kind.
func (r *Registry) SendToSession(id, text string, queue bool, origin string) (string, error) {
	clean, err := sanitizeInput(text)
	if err != nil {
		return "", err
	}
	if kind := r.tryInbox(id, clean); kind != "" {
		return kind, nil
	}
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok {
		r.mu.Unlock()
		return "", ErrNotFound
	}
	if e.isACP() {
		r.mu.Unlock()
		if origin == "" {
			origin = wire.OriginUser
		}
		// PromptACP is synchronous and cheap: it returns once the turn
		// has started.
		switch err := r.PromptACP(id, clean, origin); {
		case err == nil:
			return wire.DeliveryACP, nil
		case errors.Is(err, ErrACPBusy):
			return "", ErrSessionBusy
		case errors.Is(err, ErrNoLiveSession):
			return "", ErrNoLiveSession
		default:
			return "", err
		}
	}
	sess := e.sess
	if sess == nil {
		r.mu.Unlock()
		return "", ErrNoLiveSession
	}
	// An opening prompt nobody has placed yet means the agent has not
	// been asked anything: it may be sitting on a startup gate that a
	// stray Enter would answer. Never type into that.
	if e.pendingPrompt != "" {
		r.mu.Unlock()
		return "", ErrSessionBusy
	}
	// Older queued notes go first: while any wait, a new one joins the
	// back of the line even when the session is settled.
	if len(e.pendingInputs) > 0 || !e.settledLocked(time.Now()) {
		if !queue {
			r.mu.Unlock()
			return "", ErrSessionBusy
		}
		if len(e.pendingInputs) >= maxQueuedMessages || e.pendingBytes+len(clean) > maxQueuedBytes {
			r.mu.Unlock()
			return "", ErrDeliveryFailed
		}
		e.pendingInputs = append(e.pendingInputs, clean)
		e.pendingBytes += len(clean)
		if e.drainTimer == nil && settled(e.stateSnapshot()) {
			e.armDrainLocked(r)
		}
		r.mu.Unlock()
		return wire.DeliveryQueued, nil
	}
	e.typedAt = time.Now()
	r.mu.Unlock()
	// Outside r.mu: a PTY write can block on a full pipe.
	if _, err := sess.Write(ptyBytes(clean)); err != nil {
		log.Printf("registry: delivering message to %s: %v", id, err)
		return "", ErrDeliveryFailed
	}
	return wire.DeliveryTyped, nil
}

// tryInbox hands the note to a live Claude through its own inbox, or to
// a Pi through its Hive extension's. Either takes it between turns or as
// a new one, idle or not. It returns the wire.Delivery* kind that worked,
// or "" on any failure; the caller then runs the usual typed/queued logic
// from scratch, so nothing that changed during the dial is trusted.
//
// Older queued notes keep their place: with any waiting, or an opening
// prompt still unplaced, the inbox is not used.
func (r *Registry) tryInbox(id, text string) string {
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok || e.isACP() || e.sess == nil || e.pendingPrompt != "" || len(e.pendingInputs) > 0 {
		r.mu.Unlock()
		return ""
	}
	var send func() error
	kind := ""
	switch {
	case e.Agent == string(agent.IDClaude) && r.inboxSend != nil:
		// Claude names its session with the id Hive gave it at first launch;
		// after a resume the agent's own id can differ.
		ids, f := []string{e.AgentSessionID, e.ID}, r.inboxSend
		send, kind = func() error { return f(ids, text) }, wire.DeliverySocket
	case e.Agent == string(agent.IDPi) && r.piInboxSend != nil:
		sock, f := agent.PiInboxPath(r.socketPath, e.ID), r.piInboxSend
		send, kind = func() error { return f(sock, text) }, wire.DeliveryExtension
	default:
		r.mu.Unlock()
		return ""
	}
	r.mu.Unlock()
	// Outside r.mu: a dial can block.
	if err := send(); err != nil {
		return ""
	}
	r.mu.Lock()
	if cur := r.entries[id]; cur == e {
		// The agent is about to start a turn it has not reported yet; hold
		// off typing a fallback note onto it.
		e.typedAt = time.Now()
	}
	r.mu.Unlock()
	return kind
}

// ptyBytes is the on-the-wire form of a sanitised message: lines are
// separated by CR, as a terminal sends Enter, and a final CR submits.
// ponytail: a TUI that treats CR as submit sends each line of a
// multi-line note as its own prompt; bracketed paste would be the
// upgrade, but not every agent enables it.
func ptyBytes(clean string) []byte {
	return []byte(strings.ReplaceAll(clean, "\n", "\r") + "\r")
}

// dropSendStateLocked discards everything messaging holds for an entry
// whose process is gone or going: the queue, its timer, and the
// idle-ping arm. Called under r.mu.
func (e *Entry) dropSendStateLocked() {
	if e.drainTimer != nil {
		e.drainTimer.Stop()
		e.drainTimer = nil
	}
	if len(e.pendingInputs) > 0 {
		log.Printf("registry: dropping %d queued message(s) for %s", len(e.pendingInputs), e.ID)
	}
	e.pendingInputs = nil
	e.pendingBytes = 0
	e.notifyWhenIdle = false
}

// settleSendStateLocked is the messaging half of a state change. It runs
// under r.mu from announceStateLocked and from watchSessionExit (which
// does not go through announce), and does two things:
//
//   - ping-when-idle: an armed session that was working and has now
//     stopped (idle, exited, errored, or waiting_input — which is where
//     a hook-tier or ACP agent lands at the end of a turn) fires one
//     SESSION_EVENT(idle_ping) and disarms. Only a transition out of
//     "working" counts: idle→idle is not a transition. waiting_permission
//     does not ping; it already raises attention.
//   - queue: when the session settles (see settled) with messages
//     waiting, arm the drain timer; when it is gone, drop everything.
func (r *Registry) settleSendStateLocked(e *Entry, prev, cur agentstate.Snapshot) {
	if e.notifyWhenIdle && prev.State == wire.StateWorking &&
		(cur.State == wire.StateIdle || cur.State == wire.StateWaitingInput ||
			cur.State == wire.StateExited || cur.State == wire.StateError) {
		e.notifyWhenIdle = false
		r.broadcastLocked(wire.SessionEventIdlePing, e.Info())
	}
	if cur.State == wire.StateExited || !e.Alive() {
		e.dropSendStateLocked()
		return
	}
	if settled(cur) && len(e.pendingInputs) > 0 && e.drainTimer == nil {
		e.armDrainLocked(r)
	}
}

// armDrainLocked schedules drainOne after drainGrace, or after the
// typing hold if that ends later: a session that stays idle after our
// own write has no state change to re-arm the timer.
func (e *Entry) armDrainLocked(r *Registry) {
	id := e.ID
	delay := max(drainGrace, typedHold-time.Since(e.typedAt))
	e.drainTimer = time.AfterFunc(delay, func() { r.drainOne(id) })
}

// drainOne types ONE queued message into a session that is still settled
// when the grace period ends. If the session went working again, it does
// nothing: the next idle transition re-arms the timer.
func (r *Registry) drainOne(id string) {
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok {
		r.mu.Unlock()
		return
	}
	e.drainTimer = nil
	if e.sess == nil || len(e.pendingInputs) == 0 || e.pendingPrompt != "" ||
		!settled(e.stateSnapshot()) {
		r.mu.Unlock()
		return
	}
	if time.Since(e.typedAt) < typedHold {
		e.armDrainLocked(r)
		r.mu.Unlock()
		return
	}
	e.typedAt = time.Now()
	msg := e.pendingInputs[0]
	e.pendingInputs = e.pendingInputs[1:]
	e.pendingBytes -= len(msg)
	sess := e.sess
	r.mu.Unlock()
	if _, err := sess.Write(ptyBytes(msg)); err != nil {
		// The message was popped before the write: a failure here loses
		// it, so say so rather than dropping it silently.
		log.Printf("registry: typing a queued message into %s: %v", id, err)
	}
}

// stopDrainTimers stops every pending drain timer. Registry.Close calls
// it so no timer fires into a closed registry.
func (r *Registry) stopDrainTimers() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.drainTimer != nil {
			e.drainTimer.Stop()
			e.drainTimer = nil
		}
	}
}

var (
	// ErrNotOrchestrator is returned when the sending session has not
	// been granted leave to message its siblings (spec 389), or no longer
	// exists.
	ErrNotOrchestrator = errors.New("registry: session may not direct other sessions")
	// ErrSelfMessage is returned when a session addresses itself.
	ErrSelfMessage = errors.New("registry: a session cannot message itself")
	// ErrAmbiguousTarget is returned when a name matches several sessions
	// in the sender's project. The wrapped message lists their ids.
	ErrAmbiguousTarget = errors.New("registry: several sessions have that name")
	// ErrTargetNotAgent is returned for a plain shell target: typing an
	// agent's words into a shell would run them as commands.
	ErrTargetNotAgent = errors.New("registry: target is not an agent session")
)

// maxSenderNameRunes bounds the sender name in the provenance prefix, so
// a long session name cannot eat the message budget.
const maxSenderNameRunes = 64

// provenancePrefix is the daemon-built lead of every message one session
// sends another. The name is the sender's, cleaned so it cannot carry a
// newline (a forged second "Message from…" line), a quote or a control
// character; the short id tells apart two sessions with one name.
func provenancePrefix(name, id string) string {
	clean := strings.Map(func(r rune) rune {
		switch {
		case r == '"':
			return '\''
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
			return ' '
		}
		return r
	}, name)
	clean = strings.Join(strings.Fields(clean), " ")
	if rs := []rune(clean); len(rs) > maxSenderNameRunes {
		clean = string(rs[:maxSenderNameRunes])
	}
	if clean == "" {
		clean = "unnamed"
	}
	short := id
	if len(short) > 8 {
		short = short[:8]
	}
	return `Message from Hive session "` + clean + `" (` + short + `): `
}

// resolveInProjectLocked finds the session ref names inside caller's
// project: an exact id, else an exact name. named lists every id that
// carried the name, so len(named) > 1 is an ambiguous target (target is
// then one of them and must not be used). r.mu must be held.
func (r *Registry) resolveInProjectLocked(caller *Entry, ref string) (target *Entry, named []string) {
	if e, ok := r.entries[ref]; ok && e.ProjectID == caller.ProjectID {
		return e, nil
	}
	for _, id := range r.order {
		if e := r.entries[id]; e != nil && e.ProjectID == caller.ProjectID && e.Name == ref {
			target = e
			named = append(named, e.ID)
		}
	}
	return target, named
}

// ResolveSibling resolves ref to a session in callerID's project, for a
// read-only verb: no grant, because a same-user process can already
// read this from the control socket (spec 390). ErrNotFound for a
// missing or other-project target (including an unknown caller),
// ErrAmbiguousTarget for a name shared by several, ErrSelfMessage when
// the target is the caller.
func (r *Registry) ResolveSibling(callerID, ref string) (wire.SessionInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	caller, ok := r.entries[callerID]
	if !ok {
		return wire.SessionInfo{}, ErrNotFound
	}
	target, named := r.resolveInProjectLocked(caller, ref)
	switch {
	case target == nil:
		return wire.SessionInfo{}, ErrNotFound
	case len(named) > 1:
		return wire.SessionInfo{}, fmt.Errorf("%w: %s", ErrAmbiguousTarget, strings.Join(named, ", "))
	case target.ID == callerID:
		return wire.SessionInfo{}, ErrSelfMessage
	}
	return target.Info(), nil
}

// SendFromSession delivers text from one session to a sibling in its own
// project, on behalf of a ModeSession connection (spec 389). It returns
// the resolved target id and the wire.Delivery* kind.
//
// The grant, the sender's CURRENT project (a session can be moved while
// its connection stays open) and the target are all read under one hold
// of r.mu, so a revocation or move cannot slip between the check and the
// resolution. Delivery itself happens after, as for any send: a revoke
// that lands during it is ordered after the send.
//
// Always queued (a busy target holds the note until it idles), only to
// agent sessions, and always prefixed with provenance the caller cannot
// influence.
func (r *Registry) SendFromSession(callerID, ref, text string) (targetID, delivery string, err error) {
	r.mu.Lock()
	caller, ok := r.entries[callerID]
	if !ok || !caller.Orchestrator {
		r.mu.Unlock()
		return "", "", ErrNotOrchestrator
	}
	target, named := r.resolveInProjectLocked(caller, ref)
	switch {
	case target == nil:
		r.mu.Unlock()
		return "", "", ErrNotFound
	case len(named) > 1:
		r.mu.Unlock()
		return "", "", fmt.Errorf("%w: %s", ErrAmbiguousTarget, strings.Join(named, ", "))
	case target.ID == callerID:
		r.mu.Unlock()
		return "", "", ErrSelfMessage
	case target.Agent == "" || target.Agent == string(agent.IDShell):
		r.mu.Unlock()
		return "", "", ErrTargetNotAgent
	}
	prefix := provenancePrefix(caller.Name, caller.ID)
	targetID = target.ID
	r.mu.Unlock()

	// The body alone is validated first: prefix + nothing must never
	// pass as a message.
	body, err := sanitizeInput(text)
	if err != nil {
		return targetID, "", err
	}
	delivery, err = r.SendToSession(targetID, prefix+body, true, wire.OriginUser)
	return targetID, delivery, err
}

// StateView reads one session's Info and its state-entry counters in one
// hold of r.mu, so the counters describe exactly the state returned. The
// counters (copied, keyed by wire state) say how often the session has
// moved into each state this run — see Entry.stateEntries. found is false
// when id names no session.
func (r *Registry) StateView(id string) (info wire.SessionInfo, entries map[string]int, found bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return wire.SessionInfo{}, nil, false
	}
	entries = make(map[string]int, len(e.stateEntries))
	for st, n := range e.stateEntries {
		entries[st] = n
	}
	return e.Info(), entries, true
}
