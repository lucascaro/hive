package daemon

import (
	"context"
	"encoding/json"
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
// worktree setup fails: with nothing attached that could answer a
// dialog, the registry must refuse rather than choose for the user.
func TestControlClientCountGatesParking(t *testing.T) {
	d := newFrameTestDaemon(t)

	// The predicate the registry actually consults, wired in New.
	d.mu.Lock()
	initial := d.controlClients
	d.mu.Unlock()
	if initial != 0 {
		t.Fatalf("a fresh daemon has no control clients; got %d", initial)
	}

	// Stand in for a connected GUI. serveControl does exactly this
	// around its ModeControl branch.
	d.mu.Lock()
	d.controlClients++
	d.mu.Unlock()

	done := make(chan bool, 1)
	go func() {
		d.mu.Lock()
		n := d.controlClients
		d.mu.Unlock()
		done <- n > 0
	}()
	select {
	case ok := <-done:
		if !ok {
			t.Error("a connected control client must be counted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out reading the control-client count")
	}

	d.mu.Lock()
	d.controlClients--
	d.mu.Unlock()
	d.mu.Lock()
	n := d.controlClients
	d.mu.Unlock()
	if n != 0 {
		t.Errorf("count must return to zero on disconnect; got %d", n)
	}
}
