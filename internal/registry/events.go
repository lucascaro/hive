package registry

import (
	"log"

	"github.com/lucascaro/hive/internal/wire"
)

// Listener is a channel that receives SessionEvent notifications.
type Listener chan wire.SessionEvent

// ProjectListener is a channel that receives ProjectEvent.
type ProjectListener chan wire.ProjectEvent

// IdeaListener is a channel that receives IdeaEvent.
type IdeaListener chan wire.IdeaEvent

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
			// the client permanently desynced with no trace — warn so
			// "the GUI went stale" can be correlated with this moment.
			log.Printf("registry: dropping slow session-event listener (buffer %d full, %d listeners); client is desynced until it resubscribes",
				cap(ch), len(r.listeners))
			delete(r.listeners, ch)
			close(ch)
		}
	}
}
