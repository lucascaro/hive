package daemon

import (
	"log"
	"sync"

	"github.com/lucascaro/hive/internal/wire"
)

// commandHub relays wire.ClientCommand from one control connection to
// every other. It is the daemon's only fan-out that carries no state:
// commands are transient, nothing is replayed to a late subscriber,
// and nothing is persisted.
//
// It lives here and not in internal/registry deliberately. The
// registry is the sole writer of persisted state (see DESIGN.md), and
// a "reload your window" verb is not state — putting it there would
// mean the thing that owns sessions on disk also owns UI chatter.
//
// The shape is a deliberate copy of registry.Subscribe/broadcast,
// including the slow-listener drop and its log line: two fan-outs that
// behave differently under back-pressure is one more thing to hold in
// your head when a client goes quiet.
type commandHub struct {
	mu        sync.Mutex
	listeners map[chan wire.ClientCommand]struct{}
	closed    bool
}

func newCommandHub() *commandHub {
	return &commandHub{listeners: make(map[chan wire.ClientCommand]struct{})}
}

// Subscribe returns a channel receiving every relayed command, and a
// cleanup that unsubscribes and closes it.
//
// A subscribe after Close hands back an already-closed channel rather
// than panicking: connections are accepted unsynchronized (`go
// d.serve(...)`), so one can land here after shutdown has begun. The
// caller's range loop then drains and exits immediately, which is what
// a subscriber to a dead daemon should see.
func (h *commandHub) Subscribe() (chan wire.ClientCommand, func()) {
	// 8 is ample: commands are user-initiated (a menu click), not
	// event-stream traffic, so a listener that is even briefly awake
	// cannot fall behind. The buffer exists so Publish never blocks on
	// a conn whose write goroutine is mid-frame.
	ch := make(chan wire.ClientCommand, 8)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	h.listeners[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.listeners[ch]; ok {
			delete(h.listeners, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// Publish relays cmd to every subscriber, including the one that sent
// it. That is intentional for CmdReloadGUI: the window that asked must
// relaunch along with its siblings, and routing it through the same
// broadcast keeps one code path instead of a local special case that
// could drift from the remote one.
func (h *commandHub) Publish(cmd wire.ClientCommand) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.listeners {
		select {
		case ch <- cmd:
		default:
			log.Printf("daemon: dropping slow client-command listener (buffer %d full, %d listeners); that client will not see %q",
				cap(ch), len(h.listeners), cmd.Cmd)
			delete(h.listeners, ch)
			close(ch)
		}
	}
}

// Close unsubscribes and closes every listener. Idempotent.
func (h *commandHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for ch := range h.listeners {
		delete(h.listeners, ch)
		close(ch)
	}
}
