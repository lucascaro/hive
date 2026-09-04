//go:build darwin

package main

import (
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// The daemon emits a `title` event every time a shell redraws its
// prompt. Without coalescing the menu retitles itself many times a
// second while the user is reading it — which is exactly how this was
// reported: "sessions jump up and down".
func TestPublishCoalescesBursts(t *testing.T) {
	prev := publishInterval
	publishInterval = 50 * time.Millisecond
	t.Cleanup(func() { publishInterval = prev })

	var mu sync.Mutex
	var models []Model
	c := NewClient(func(m Model) {
		mu.Lock()
		models = append(models, m)
		mu.Unlock()
	})
	c.welcome = wire.Welcome{DaemonContract: 1}

	// A burst of 50 events, as a prompt-heavy machine produces.
	for i := 0; i < 50; i++ {
		c.mu.Lock()
		c.sessions = []wire.SessionInfo{{ID: "a", Name: "alpha", Alive: true}}
		c.mu.Unlock()
		c.publish()
	}

	mu.Lock()
	immediate := len(models)
	mu.Unlock()
	if immediate != 1 {
		t.Errorf("burst produced %d immediate publishes, want 1 — the rest must coalesce", immediate)
	}

	// The trailing publish still lands: coalescing must not drop the
	// final state of a burst.
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(models)
		mu.Unlock()
		if n >= 2 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("the trailing publish never arrived; a burst's final state was dropped")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// A single change in a quiet period must go out at once. A menu that
// lagged behind every click by the coalescing window would feel broken.
func TestPublishIsImmediateWhenQuiet(t *testing.T) {
	prev := publishInterval
	publishInterval = 50 * time.Millisecond
	t.Cleanup(func() { publishInterval = prev })

	got := make(chan Model, 4)
	c := NewClient(func(m Model) { got <- m })
	c.welcome = wire.Welcome{DaemonContract: 1}
	c.sessions = []wire.SessionInfo{{ID: "a", Alive: true}}

	c.publish()
	select {
	case m := <-got:
		if m.Sessions != 1 {
			t.Errorf("published %+v", m)
		}
	case <-time.After(time.Second):
		t.Fatal("no immediate publish")
	}
}

// applySessionEvent is the only thing keeping hivebar's list in step
// between snapshots, and an unknown kind must not be a hole in it.
func TestApplySessionEventPatchesTheList(t *testing.T) {
	c := NewClient(func(Model) {})

	c.applySessionEvent(wire.SessionEvent{
		Kind: wire.SessionEventAdded, Session: wire.SessionInfo{ID: "a", Name: "alpha"},
	})
	c.applySessionEvent(wire.SessionEvent{
		Kind: wire.SessionEventAdded, Session: wire.SessionInfo{ID: "b", Name: "beta"},
	})
	if len(c.sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(c.sessions))
	}

	c.applySessionEvent(wire.SessionEvent{
		Kind:    wire.SessionEventAttention,
		Session: wire.SessionInfo{ID: "a", Name: "alpha", NeedsAttention: true},
	})
	if !c.sessions[0].NeedsAttention {
		t.Error("attention event did not patch the entry")
	}

	// A kind a newer daemon introduces carries a full SessionInfo, so
	// adopting it is strictly closer to the truth than keeping a stale
	// copy.
	c.applySessionEvent(wire.SessionEvent{
		Kind: "something-new", Session: wire.SessionInfo{ID: "a", Name: "renamed"},
	})
	if c.sessions[0].Name != "renamed" {
		t.Errorf("unknown kind was ignored; name = %q", c.sessions[0].Name)
	}

	c.applySessionEvent(wire.SessionEvent{
		Kind: wire.SessionEventRemoved, Session: wire.SessionInfo{ID: "a"},
	})
	if len(c.sessions) != 1 || c.sessions[0].ID != "b" {
		t.Errorf("after removal: %+v", c.sessions)
	}
}
