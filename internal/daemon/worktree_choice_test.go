package daemon

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// #451: the daemon half of the parked-worktree decision — the frame
// dispatch, and the control-client count the registry consults before
// it parks at all.

// A well-formed RESOLVE_WORKTREE_CHOICE for a session that is not
// parked must be silently accepted. Two GUI windows race to answer the
// same dialog, and the loser must not see an error.
func TestResolveWorktreeChoiceUnknownSessionIsSilent(t *testing.T) {
	d := newFrameTestDaemon(t)
	rec := &recordOps{}

	payload, err := json.Marshal(wire.ResolveWorktreeChoiceReq{
		SessionID: "no-such-session",
		Choice:    wire.WorktreeChoiceProceed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if done := d.handleControlFrame(context.Background(), rec.ops(),
		wire.FrameResolveWorktreeChoice, payload); done {
		t.Fatal("handler must not close the connection")
	}
	// The work runs off the read loop, so let it finish before asserting.
	d.ops.Wait()

	if len(rec.errs) != 0 {
		t.Errorf("resolving an unknown session must be silent; got %+v", rec.errs)
	}
}

// A malformed payload is answered with an error and keeps the
// connection open, like every other decoded frame.
func TestResolveWorktreeChoiceBadPayload(t *testing.T) {
	d := newFrameTestDaemon(t)
	rec := &recordOps{}

	if done := d.handleControlFrame(context.Background(), rec.ops(),
		wire.FrameResolveWorktreeChoice, []byte(`["not","an","object"]`)); done {
		t.Fatal("a bad payload must not close the connection")
	}
	if len(rec.errs) == 0 {
		t.Fatal("a bad payload must be answered with an error")
	}
}

// An unknown choice value reaches the registry, which rejects it
// without touching the session, and the daemon reports that.
func TestResolveWorktreeChoiceUnknownChoiceReportsError(t *testing.T) {
	d := newFrameTestDaemon(t)
	rec := &recordOps{}

	payload, err := json.Marshal(wire.ResolveWorktreeChoiceReq{
		SessionID: "no-such-session",
		Choice:    "banana",
	})
	if err != nil {
		t.Fatal(err)
	}
	if done := d.handleControlFrame(context.Background(), rec.ops(),
		wire.FrameResolveWorktreeChoice, payload); done {
		t.Fatal("handler must not close the connection")
	}
	d.ops.Wait()

	if len(rec.errs) != 1 || rec.errs[0].Code != "resolve_worktree_choice_failed" {
		t.Errorf("an unknown choice must be reported; got %+v", rec.errs)
	}
}

// The control-client count is what decides park-vs-hard-fail when
// worktree setup fails: with nothing attached that could show a dialog,
// the registry must refuse rather than choose for the user. Driven
// through the real serve() dispatch — poking the field would only
// assert this test's own arithmetic.
func TestControlClientCountTracksRealConnections(t *testing.T) {
	d := newFrameTestDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	count := func() int {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.controlClients
	}
	// serve() registers the conn in d.clients, so the map must exist.
	d.mu.Lock()
	if d.clients == nil {
		d.clients = make(map[net.Conn]struct{})
	}
	d.mu.Unlock()

	if got := count(); got != 0 {
		t.Fatalf("a fresh daemon has no control clients; got %d", got)
	}

	server, client := net.Pipe()
	go d.serve(ctx, server)
	if err := wire.WriteJSON(client, wire.FrameHello, wire.Hello{
		Mode: wire.ModeControl, Version: wire.PROTOCOL_VERSION,
	}); err != nil {
		t.Fatalf("write HELLO: %v", err)
	}
	// Drain whatever the daemon writes back (WELCOME, snapshots) so the
	// unbuffered pipe cannot park the server goroutine before it counts.
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := client.Read(buf); err != nil {
				return
			}
		}
	}()

	waitFor(t, 2*time.Second, func() bool { return count() == 1 })
	if count() != 1 {
		t.Fatalf("a connected ModeControl client must be counted; got %d", count())
	}

	_ = client.Close()
	waitFor(t, 2*time.Second, func() bool { return count() == 0 })
	if count() != 0 {
		t.Fatalf("the count must return to zero on disconnect; got %d", count())
	}
}

// A ModeSession connection is an agent's own events socket. It cannot
// render a dialog, so counting it would let the registry park a
// question nothing can answer.
func TestSessionModeIsNotCountedAsAControlClient(t *testing.T) {
	d := newFrameTestDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d.mu.Lock()
	if d.clients == nil {
		d.clients = make(map[net.Conn]struct{})
	}
	d.mu.Unlock()

	server, client := net.Pipe()
	defer client.Close()
	go d.serve(ctx, server)
	if err := wire.WriteJSON(client, wire.FrameHello, wire.Hello{
		Mode: wire.ModeSession, Version: wire.PROTOCOL_VERSION,
	}); err != nil {
		t.Fatalf("write HELLO: %v", err)
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := client.Read(buf); err != nil {
				return
			}
		}
	}()

	// The assertion is "it was served and still not counted", so wait
	// for proof it was served: serve() registers every connection in
	// d.clients regardless of mode. Asserting the count alone would
	// pass just as well if the HELLO had never been read.
	waitFor(t, 2*time.Second, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return len(d.clients) > 0
	})
	d.mu.Lock()
	served, got := len(d.clients), d.controlClients
	d.mu.Unlock()
	if served == 0 {
		t.Fatal("the session-mode connection was never served, so the count below proves nothing")
	}
	if got != 0 {
		t.Errorf("ModeSession must not count as a client that can answer a dialog; got %d", got)
	}
}
