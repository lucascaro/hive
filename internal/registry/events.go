package registry

import (
	"log"
	"time"

	"github.com/lucascaro/hive/internal/agentstate"
	"github.com/lucascaro/hive/internal/wire"
)

// Listener is a channel that receives SessionEvent notifications.
type Listener chan wire.SessionEvent

// ProjectListener is a channel that receives ProjectEvent.
type ProjectListener chan wire.ProjectEvent

// IdeaListener is a channel that receives IdeaEvent.
type IdeaListener chan wire.IdeaEvent

// ActivityListener is a channel that receives ActivityMsg deltas.
type ActivityListener chan wire.ActivityMsg

// Subscribe returns a channel that receives every SessionEvent. The
// returned cleanup function unsubscribes and closes the channel.
// Slow consumers are dropped — listeners must drain promptly.
func (r *Registry) Subscribe() (Listener, func()) {
	// 64, not 16: Update with an order change broadcasts one event per
	// session while holding r.mu (see reindexLocked), so a listener
	// that's merely a beat behind on a many-session registry could
	// overflow a small buffer and get dropped.
	ch := make(Listener, 64)
	r.mu.Lock()
	if r.listeners == nil {
		// Close() ran first — it nils the map after closing every
		// listener. serve() goroutines are spawned unsynchronized
		// (daemon.go: `go d.serve(ctx, conn)`), so a connection
		// accepted just before shutdown can land here afterwards and
		// used to panic with "assignment to entry in nil map".
		// Hand back an already-closed channel: the caller's range
		// loop drains and exits at once, which is what a subscriber
		// to a dead registry should see.
		r.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	r.listeners[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		if _, ok := r.listeners[ch]; ok {
			delete(r.listeners, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
}

func (r *Registry) broadcast(kind string, info wire.SessionInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.broadcastLocked(kind, info)
}

func (r *Registry) broadcastLocked(kind string, info wire.SessionInfo) {
	ev := wire.SessionEvent{Kind: kind, Session: info}
	for ch := range r.listeners {
		select {
		case ch <- ev:
		default:
			// Listener can't keep up. Dropping it silently would leave
			// the client permanently desynced with no trace. The channel close
			// makes the daemon hang up on it (it reconnects); warn so
			// "the GUI went stale" can be correlated with this moment.
			log.Printf("registry: dropping slow session-event listener (buffer %d full, %d listeners); closing the channel, so the daemon hangs up on that client and it reconnects",
				cap(ch), len(r.listeners))
			delete(r.listeners, ch)
			close(ch)
		}
	}
}

// SubscribeActivity returns a channel that receives every ACTIVITY
// delta. The returned cleanup function unsubscribes and closes it.
//
// Deliberately not subscription-gated per session: the frames are
// ~100 bytes at a handful per second, on connections that already
// stream raw PTY bytes, and the activity grid wants every session
// anyway. A subscription protocol would be real complexity bought for
// no measurable saving.
func (r *Registry) SubscribeActivity() (ActivityListener, func()) {
	ch := make(ActivityListener, 128)
	r.mu.Lock()
	if r.activityListeners == nil {
		// Close() ran first — same reasoning as Subscribe.
		r.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	r.activityListeners[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		if _, ok := r.activityListeners[ch]; ok {
			delete(r.activityListeners, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
}

// broadcastActivityLocked fans out one delta for the event just
// applied. Callers hold r.mu.
//
// Every accepted tier event sends one, stamped with stale_at: the kinds
// that carry activity send it (one tool event, or the whole plan), and
// the rest (prompt, turn end, ping, the waits, subagent lifecycle) send
// a bare liveness frame. Without those, a client's stale_at would lag
// every report that is not a tool or plan, and a session thinking for
// longer than HookStaleAfter between tool calls would read as stale. A
// frame is ~80 bytes, a handful per turn, on a feed that already
// carries PTY bytes.
//
// An event the machine rejected (the out-of-order guard) sends nothing.
func (r *Registry) broadcastActivityLocked(e *Entry, kind string) {
	m := e.machine()
	if !m.TakeAccepted() {
		// Consume any tool delta too, so it cannot leak into a later
		// frame. A rejected event never sets one today; this keeps the
		// pairing honest if that changes.
		m.LastToolDelta()
		return
	}
	msg := wire.ActivityMsg{SessionID: e.ID, StaleAt: staleAtString(m)}
	switch kind {
	case wire.AgentEventToolStart, wire.AgentEventToolEnd:
		if ev, ok := m.LastToolDelta(); ok {
			msg.Events = []wire.ToolEvent{ev}
		}
	case wire.AgentEventPlan, wire.AgentEventPlanItem:
		// A per-item update still sends the whole plan. It is at most
		// MaxPlanItems small rows, and a client that only ever receives
		// complete plans never has to replicate the merge rules — the
		// daemon stays the one place a plan is assembled. Activity()
		// returns a non-nil slice, so an emptied plan marshals as [].
		_, msg.Plan = m.Activity()
	}
	for ch := range r.activityListeners {
		select {
		case ch <- msg:
		default:
			// Same policy as session events: a consumer that cannot
			// keep up is dropped loudly rather than silently desynced.
			log.Printf("registry: dropping slow activity listener (buffer %d full, %d listeners); closing the channel, so the daemon hangs up on that client and it reconnects",
				cap(ch), len(r.activityListeners))
			delete(r.activityListeners, ch)
			close(ch)
		}
	}
}

// staleAtString is the machine's StaleAt on the wire: RFC3339Nano UTC,
// empty when the session has no reporting tier.
func staleAtString(m *agentstate.Machine) string {
	at, ok := m.StaleAt()
	if !ok {
		return ""
	}
	return at.UTC().Format(time.RFC3339Nano)
}

// ActivitySnapshot returns one session's whole stored ring and plan —
// the answer to GET_ACTIVITY. Issued when a panel or activity tile
// first renders, so nobody pays for 200 events x N sessions at connect.
func (r *Registry) ActivitySnapshot(id string) (wire.ActivityMsg, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	if !ok {
		return wire.ActivityMsg{}, ErrNotFound
	}
	events, plan := e.machine().Activity()
	return wire.ActivityMsg{
		SessionID: id,
		Events:    events,
		Plan:      plan,
		Full:      true,
		StaleAt:   staleAtString(e.machine()),
	}, nil
}
