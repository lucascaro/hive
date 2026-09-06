package daemon

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// A control client must not be able to miss an event that happens
// between its connection being accepted and its subscription being
// live. serveControl subscribes before it writes WELCOME for exactly
// this reason.
//
// net.Pipe is unbuffered, so the server goroutine parks inside the
// WELCOME write until this test reads it: the event below is applied
// strictly before the client is told it is connected. With the
// subscribe after the write, the broadcast lands on no listener and
// this test times out.
func TestControlSubscribesBeforeWelcome(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)

	server, client := net.Pipe()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.serveControl(ctx, server, wire.Hello{Mode: wire.ModeControl})

	// Let the goroutine reach the WELCOME write and park there — the
	// pipe is unbuffered and nothing else in serveControl can block, so
	// after this it is either parked in that write or has already
	// subscribed. Waiting longer only makes that more certain, so the
	// sleep cannot flake the passing direction.
	time.Sleep(100 * time.Millisecond)

	// Applied strictly before the client is told it is connected: the
	// broadcast is only seen if the subscription came first.
	if err := d.Registry().ApplyAgentEvent(id, wire.AgentEvent{
		Kind:   wire.AgentEventPrompt,
		Source: wire.StateSourceHook,
		Text:   "say pong",
	}); err != nil {
		t.Fatalf("apply agent event: %v", err)
	}

	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	for {
		ft, payload, err := wire.ReadFrame(client)
		if err != nil {
			t.Fatalf("read frame: %v (never saw SESSION_EVENT(state))", err)
		}
		if ft != wire.FrameSessionEvent {
			continue // WELCOME, PROJECTS, SESSIONS
		}
		var ev wire.SessionEvent
		if err := json.Unmarshal(payload, &ev); err != nil {
			t.Fatalf("unmarshal session event: %v", err)
		}
		if ev.Kind == wire.SessionEventState && ev.Session.ID == id {
			return
		}
	}
}
