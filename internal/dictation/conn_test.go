// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package dictation

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// recorder implements HUDEvents and HotkeyEvents and logs every call.
type recorder struct {
	mu    sync.Mutex
	calls []string
	ids   [][]string
	keys  []wire.GlobalHotkey
}

func (r *recorder) add(s string) { r.mu.Lock(); r.calls = append(r.calls, s); r.mu.Unlock() }
func (r *recorder) SetSessions(_ []wire.ProjectInfo, ss []wire.SessionInfo) {
	var ids []string
	for _, s := range ss {
		ids = append(ids, s.ID)
	}
	r.mu.Lock()
	r.ids = append(r.ids, ids)
	r.mu.Unlock()
	r.add("sessions")
}
func (r *recorder) OnSent(id string)           { r.add("sent:" + id) }
func (r *recorder) OnError(id, code, _ string) { r.add("error:" + id + ":" + code) }
func (r *recorder) Disconnected()              { r.add("disconnected") }
func (r *recorder) SetHandyAutoStart(on bool) {
	r.add(map[bool]string{true: "handy:on", false: "handy:off"}[on])
}
func (r *recorder) Connected(contract int) { r.add("connected") }
func (r *recorder) SetHotkey(h wire.GlobalHotkey) {
	r.mu.Lock()
	r.keys = append(r.keys, h)
	r.mu.Unlock()
	r.add("hotkey")
}
func (r *recorder) has(s string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.calls, s)
}
func (r *recorder) lastIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.ids) == 0 {
		return nil
	}
	return r.ids[len(r.ids)-1]
}

func waitUntil(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// fakeDaemon answers one HELLO over a pipe and hands back its end.
func fakeDaemon(t *testing.T, contract int) (func() (*wire.Client, error), chan net.Conn, chan wire.Hello) {
	conns, hellos := make(chan net.Conn, 4), make(chan wire.Hello, 4)
	dial := func() (*wire.Client, error) {
		cli, srv := net.Pipe()
		go func() {
			ft, payload, err := wire.ReadFrame(srv)
			if err != nil || ft != wire.FrameHello {
				srv.Close()
				return
			}
			var h wire.Hello
			_ = jsonUnmarshal(payload, &h)
			hellos <- h
			_ = wire.WriteJSON(srv, wire.FrameWelcome, wire.Welcome{DaemonContract: contract})
			conns <- srv
		}()
		return wire.Handshake(cli, wire.Hello{Mode: wire.ModeControl, Client: wire.DictationClientPrefix + "test"})
	}
	return dial, conns, hellos
}

func TestConnRoutesFrames(t *testing.T) {
	dial, conns, _ := fakeDaemon(t, WindowsContract)
	r := &recorder{}
	c := &Conn{Dial: dial, HUD: r, Hotkeys: r, MinContract: WindowsContract, Retry: 10 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	srv := <-conns
	wrote := make(chan wire.FrameType, 16)
	go func() { // drain what the box writes
		for {
			ft, _, err := wire.ReadFrame(srv)
			if err != nil {
				return
			}
			wrote <- ft
		}
	}()
	write := func(ft wire.FrameType, v any) {
		t.Helper()
		if err := wire.WriteJSON(srv, ft, v); err != nil {
			t.Fatal(err)
		}
	}
	waitUntil(t, "connected", func() bool { return r.has("connected") })
	write(wire.FrameSessions, wire.SessionsResp{Sessions: []wire.SessionInfo{{ID: "a"}, {ID: "b"}}})
	write(wire.FrameSessionEvent, wire.SessionEvent{Kind: wire.SessionEventAdded, Session: wire.SessionInfo{ID: "c"}})
	write(wire.FrameSessionEvent, wire.SessionEvent{Kind: wire.SessionEventRemoved, Session: wire.SessionInfo{ID: "a"}})
	waitUntil(t, "session list b,c", func() bool { return slices.Equal(r.lastIDs(), []string{"b", "c"}) })
	write(wire.FrameSent, wire.SentResp{SessionID: "b"})
	write(wire.FrameError, wire.Error{SessionID: "c", Code: "session_busy"})
	write(wire.FrameError, wire.Error{Code: "unrelated"})
	write(wire.FrameDictation, wire.Dictation{Hotkey: wire.DefaultGlobalHotkey, HandyAutoStart: true})
	waitUntil(t, "routed replies", func() bool {
		return r.has("sent:b") && r.has("error:c:session_busy") && r.has("handy:on") && r.has("hotkey")
	})
	if r.has("error::unrelated") {
		t.Error("an error with no session reached the box")
	}
	// A project event is answered with a fresh project list.
	write(wire.FrameProjectEvent, wire.ProjectEvent{})
	if ft := <-wrote; ft != wire.FrameListProjects {
		t.Fatalf("a project event sent %s, want LIST_PROJECTS", ft)
	}
	if !c.SendToSession("b", "hi", false) {
		t.Error("send failed on a live connection")
	}
	if ft := <-wrote; ft != wire.FrameSendToSession {
		t.Fatalf("the send wrote %s, want SEND_TO_SESSION", ft)
	}
	srv.Close()
	waitUntil(t, "disconnect", func() bool { return r.has("disconnected") })
	waitUntil(t, "redial", func() bool { return len(conns) > 0 })
}

func TestConnBacksOffOnOldContract(t *testing.T) {
	var dials int
	var mu sync.Mutex
	dial, conns, _ := fakeDaemon(t, WindowsContract-1)
	counting := func() (*wire.Client, error) {
		mu.Lock()
		dials++
		mu.Unlock()
		return dial()
	}
	r := &recorder{}
	c := &Conn{Dial: counting, HUD: r, Hotkeys: r, MinContract: WindowsContract, Retry: time.Millisecond, OldDaemon: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)
	<-conns
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if dials != 1 {
		t.Fatalf("dialled an old daemon %d times in 100ms; it must back off, not loop", dials)
	}
	if r.has("connected") {
		t.Error("the box treated an old daemon as connected")
	}
}

func TestConnRetriesFailedDial(t *testing.T) {
	var n int
	var mu sync.Mutex
	r := &recorder{}
	c := &Conn{Dial: func() (*wire.Client, error) {
		mu.Lock()
		n++
		mu.Unlock()
		return nil, errors.New("no daemon")
	}, HUD: r, Hotkeys: r, Retry: 5 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	c.Run(ctx)
	if n < 2 || !r.has("disconnected") {
		t.Fatalf("dials %d, disconnected reported %v", n, r.has("disconnected"))
	}
	if c.SendToSession("a", "x", false) {
		t.Error("send reported success with no connection")
	}
}

func TestApplySessionEvent(t *testing.T) {
	s := []wire.SessionInfo{{ID: "a", Title: "old"}, {ID: "b"}}
	s = ApplySessionEvent(s, wire.SessionEvent{Kind: "title", Session: wire.SessionInfo{ID: "a", Title: "new"}})
	if s[0].Title != "new" {
		t.Fatalf("update not applied: %+v", s)
	}
	s = ApplySessionEvent(s, wire.SessionEvent{Kind: "some_future_kind", Session: wire.SessionInfo{ID: "z"}})
	s = ApplySessionEvent(s, wire.SessionEvent{Kind: wire.SessionEventRemoved, Session: wire.SessionInfo{ID: "b"}})
	var ids []string
	for _, x := range s {
		ids = append(ids, x.ID)
	}
	if !slices.Equal(ids, []string{"a", "z"}) {
		t.Fatalf("ids = %v, want [a z]", ids)
	}
}

// The daemon subscribes before it sends the snapshot, so an added event
// can name a session the snapshot already listed: it must not duplicate.
func TestApplySessionEventAddedIsIdempotent(t *testing.T) {
	s := []wire.SessionInfo{{ID: "a", Title: "old"}}
	s = ApplySessionEvent(s, wire.SessionEvent{Kind: wire.SessionEventAdded, Session: wire.SessionInfo{ID: "a", Title: "new"}})
	s = ApplySessionEvent(s, wire.SessionEvent{Kind: wire.SessionEventAdded, Session: wire.SessionInfo{ID: "b"}})
	if len(s) != 2 || s[0].Title != "new" || s[1].ID != "b" {
		t.Fatalf("sessions = %+v, want [a(new) b]", s)
	}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
