package daemon

import (
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

func TestCommandHubFansOutToEverySubscriber(t *testing.T) {
	h := newCommandHub()
	a, unsubA := h.Subscribe()
	defer unsubA()
	b, unsubB := h.Subscribe()
	defer unsubB()

	want := wire.ClientCommand{Cmd: wire.CmdReloadGUI}
	h.Publish(want)

	for i, ch := range []chan wire.ClientCommand{a, b} {
		select {
		case got := <-ch:
			if got != want {
				t.Errorf("subscriber %d: got %+v, want %+v", i, got, want)
			}
		default:
			t.Errorf("subscriber %d received nothing", i)
		}
	}
}

// The sender must receive its own command. "Reload every window"
// includes the window that asked, and routing it back through the
// broadcast is what keeps one code path instead of a local special
// case that can drift from the remote one.
func TestCommandHubDeliversToTheSender(t *testing.T) {
	h := newCommandHub()
	sender, unsub := h.Subscribe()
	defer unsub()

	h.Publish(wire.ClientCommand{Cmd: wire.CmdReloadGUI})

	select {
	case got := <-sender:
		if got.Cmd != wire.CmdReloadGUI {
			t.Errorf("got %+v", got)
		}
	default:
		t.Fatal("sender did not receive its own command")
	}
}

func TestCommandHubUnsubscribeStopsDelivery(t *testing.T) {
	h := newCommandHub()
	ch, unsub := h.Subscribe()
	unsub()

	// The channel is closed, so a receive must not block and must not
	// yield a command.
	if got, ok := <-ch; ok {
		t.Fatalf("channel still open after unsubscribe, got %+v", got)
	}

	// Publishing to a hub with no listeners must not panic (it would
	// if unsub left a closed channel in the map).
	h.Publish(wire.ClientCommand{Cmd: wire.CmdReloadGUI})
}

// A listener that stops draining is dropped rather than allowed to
// block the publisher — same contract as registry's session-event
// fanout. A hung GUI must not be able to wedge every other window's
// reload.
func TestCommandHubDropsSlowSubscriber(t *testing.T) {
	h := newCommandHub()
	slow, unsub := h.Subscribe()
	defer unsub()
	healthy, unsubH := h.Subscribe()
	defer unsubH()

	// Overrun the slow subscriber's buffer. The healthy one drains
	// every round, so only the slow one should ever be dropped.
	for i := 0; i < cap(slow)+2; i++ {
		h.Publish(wire.ClientCommand{Cmd: wire.CmdReloadGUI})
		select {
		case <-healthy:
		default:
			// Already dropped, which the assertion below catches.
		}
	}

	// The slow one was dropped: its channel is closed, so draining it
	// terminates instead of blocking.
	drained := 0
	for range slow {
		drained++
		if drained > cap(slow)+4 {
			t.Fatal("slow subscriber channel never closed; it was not dropped")
		}
	}

	// The healthy one survived and still receives. This is the half
	// that matters: one wedged GUI must not cost every other window
	// its reload.
	h.Publish(wire.ClientCommand{Cmd: wire.CmdFocusSession, SessionID: "s1"})
	select {
	case got, ok := <-healthy:
		if !ok {
			t.Fatal("healthy subscriber was dropped alongside the slow one")
		}
		if got.SessionID != "s1" {
			t.Errorf("healthy subscriber got %+v", got)
		}
	default:
		t.Fatal("healthy subscriber received nothing after the slow one was dropped")
	}
}

func TestCommandHubSubscribeAfterCloseIsClosed(t *testing.T) {
	h := newCommandHub()
	h.Close()

	// Connections are accepted unsynchronized, so one can subscribe
	// after shutdown has begun. It must get a dead channel, not a
	// panic and not a live subscription to a hub nobody will publish
	// to again.
	ch, unsub := h.Subscribe()
	defer unsub()
	if _, ok := <-ch; ok {
		t.Fatal("subscribe after Close must yield a closed channel")
	}
}

func TestCommandHubCloseIsIdempotent(t *testing.T) {
	h := newCommandHub()
	_, unsub := h.Subscribe()
	h.Close()
	h.Close() // must not panic on the already-closed listener
	unsub()   // must not double-close either
}
