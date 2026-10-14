package registry

import (
	"errors"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lucascaro/hive/internal/agentstate"
	"github.com/lucascaro/hive/internal/wire"
)

// Session messaging (spec 338, phase 1): hand a running session a note,
// and get told when it stops working.
//
// Three strategies, chosen by the target:
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
		if e.drainTimer == nil && e.settledLocked(time.Now()) {
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

func (e *Entry) armDrainLocked(r *Registry) {
	id := e.ID
	e.drainTimer = time.AfterFunc(drainGrace, func() { r.drainOne(id) })
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
