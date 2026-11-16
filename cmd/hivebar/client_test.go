// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import (
	"encoding/json"
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

type recHUD struct {
	sessions [][]wire.SessionInfo
	sent     []string
	errs     []string
}

func (r *recHUD) SetSessions(_ []wire.ProjectInfo, s []wire.SessionInfo) {
	r.sessions = append(r.sessions, s)
}
func (r *recHUD) OnSent(id string)             { r.sent = append(r.sent, id) }
func (r *recHUD) OnError(id, code, msg string) { r.errs = append(r.errs, id+":"+code) }

type recHotkeys struct {
	keys []wire.GlobalHotkey
	down int
}

func (r *recHotkeys) Connected(int)                 {}
func (r *recHotkeys) Disconnected()                 { r.down++ }
func (r *recHotkeys) SetHotkey(h wire.GlobalHotkey) { r.keys = append(r.keys, h) }

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// SENT and session-scoped ERRORs reach the HUD; an ERROR with no
// session id (a reply to a menu click) does not; GLOBAL_HOTKEY reaches
// the hotkey.
func TestClientRoutesHUDFrames(t *testing.T) {
	c := NewClient(func(Model) {})
	h, k := &recHUD{}, &recHotkeys{}
	c.AttachHUD(h, k)
	c.handleFrame(wire.FrameSent, mustMarshal(t, wire.SentResp{SessionID: "a"}))
	c.handleFrame(wire.FrameError, mustMarshal(t, wire.Error{Code: wire.ErrCodeSessionBusy, SessionID: "a"}))
	c.handleFrame(wire.FrameError, mustMarshal(t, wire.Error{Code: "mode_not_allowed"}))
	c.handleFrame(wire.FrameGlobalHotkey, mustMarshal(t, wire.DefaultGlobalHotkey))
	if len(h.sent) != 1 || h.sent[0] != "a" {
		t.Errorf("sent = %v", h.sent)
	}
	if len(h.errs) != 1 || h.errs[0] != "a:"+wire.ErrCodeSessionBusy {
		t.Errorf("errs = %v", h.errs)
	}
	if len(k.keys) != 1 || k.keys[0] != wire.DefaultGlobalHotkey {
		t.Errorf("hotkeys = %v", k.keys)
	}
}

func TestClientFeedsHUDSessionsAndDisconnect(t *testing.T) {
	c := NewClient(func(Model) {})
	h, k := &recHUD{}, &recHotkeys{}
	c.AttachHUD(h, k)
	c.handleFrame(wire.FrameSessions, mustMarshal(t, wire.SessionsResp{Sessions: []wire.SessionInfo{{ID: "a", Alive: true}}}))
	if len(h.sessions) == 0 || len(h.sessions[len(h.sessions)-1]) != 1 {
		t.Fatalf("HUD did not get the snapshot: %v", h.sessions)
	}
	c.setDisconnected()
	if got := h.sessions[len(h.sessions)-1]; len(got) != 0 || k.down != 1 {
		t.Fatalf("after disconnect: sessions %v, down %d", got, k.down)
	}
}

func TestClientSendToSessionWithoutConnection(t *testing.T) {
	c := NewClient(func(Model) {})
	if c.SendToSession("a", "hi", false) {
		t.Fatal("SendToSession reported success with no connection")
	}
}
