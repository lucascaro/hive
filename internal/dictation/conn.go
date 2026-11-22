// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package dictation

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// HUDEvents is what the box needs from a daemon connection: the live
// session list and the answers to its sends. *HUD implements it.
type HUDEvents interface {
	SetSessions(projects []wire.ProjectInfo, sessions []wire.SessionInfo)
	OnSent(id string)
	OnError(id, code, msg string)
	Disconnected()
	SetHandyAutoStart(on bool)
}

// HotkeyEvents is what the hotkey needs: the daemon's contract and the
// chord it relays in DICTATION. *Hotkeys implements it.
type HotkeyEvents interface {
	Connected(contract int)
	Disconnected()
	SetHotkey(h wire.GlobalHotkey)
}

// WindowsContract is the DaemonContract the Windows box needs: from 41
// the daemon treats its connection as headless (internal/daemon
// canAnswer). An older one would count it as a window that can answer
// a plan review, so Conn does not stay connected to one.
const WindowsContract = 41

// Conn is a dictation box's own control connection to the daemon.
//
// The Windows GUI already has a control connection, but it forwards
// every frame to the frontend: a SENT answering the box would reach the
// GUI's message sheet, and the sheet's answers would reach the box. A
// connection of its own keeps the two apart, and its HELLO names it
// headless (wire.DictationClientPrefix) so the daemon never parks a
// question on it.
//
// Dial must only connect, never start a daemon: Conn redials for the
// life of the window, and one that spawned hived would resurrect a
// daemon the user had just quit.
type Conn struct {
	Dial        func() (*wire.Client, error)
	HUD         HUDEvents
	Hotkeys     HotkeyEvents
	MinContract int
	// Retry is the wait between failed dials; OldDaemon the wait after
	// meeting a daemon below MinContract, which will not change until it
	// restarts. Zero means the defaults.
	Retry, OldDaemon time.Duration

	mu       sync.Mutex
	cs       *wire.Client
	projects []wire.ProjectInfo
	sessions []wire.SessionInfo
}

// Run connects and redials until ctx ends.
func (c *Conn) Run(ctx context.Context) {
	retry, old := c.Retry, c.OldDaemon
	if retry == 0 {
		retry = 2 * time.Second
	}
	if old == 0 {
		old = time.Minute
	}
	for ctx.Err() == nil {
		wait := retry
		if cs, err := c.Dial(); err == nil {
			if cs.Welcome().DaemonContract < c.MinContract {
				log.Printf("dictation: daemon contract %d < %d; restart the daemon to use the dictation box", cs.Welcome().DaemonContract, c.MinContract)
				_ = cs.Close()
				wait = old
			} else {
				c.serve(ctx, cs)
			}
		}
		c.HUD.Disconnected()
		c.Hotkeys.Disconnected()
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

func (c *Conn) serve(ctx context.Context, cs *wire.Client) {
	c.mu.Lock()
	c.cs = cs
	c.projects, c.sessions = nil, nil
	c.mu.Unlock()
	stop := context.AfterFunc(ctx, func() { _ = cs.Close() })
	defer stop()
	defer func() {
		c.mu.Lock()
		c.cs = nil
		c.mu.Unlock()
		_ = cs.Close()
	}()
	c.Hotkeys.Connected(cs.Welcome().DaemonContract)
	for {
		ft, payload, err := cs.ReadFrame()
		if err != nil {
			return
		}
		c.handle(ft, payload)
	}
}

// handle routes one frame. Nothing here may block: the HUD only takes
// its lock and hands Handy work to its own worker, so a slow Handy
// cannot stall reads into the daemon's slow-client hang-up.
func (c *Conn) handle(ft wire.FrameType, payload []byte) {
	switch ft {
	case wire.FrameSessions:
		var r wire.SessionsResp
		if json.Unmarshal(payload, &r) == nil {
			c.mu.Lock()
			c.sessions = r.Sessions
			c.mu.Unlock()
			c.feed()
		}
	case wire.FrameProjects:
		var r wire.ProjectsResp
		if json.Unmarshal(payload, &r) == nil {
			c.mu.Lock()
			c.projects = r.Projects
			c.mu.Unlock()
			c.feed()
		}
	case wire.FrameSessionEvent:
		var ev wire.SessionEvent
		if json.Unmarshal(payload, &ev) == nil {
			c.mu.Lock()
			c.sessions = ApplySessionEvent(c.sessions, ev)
			c.mu.Unlock()
			c.feed()
		}
	case wire.FrameProjectEvent:
		// Re-list rather than patch: projects change rarely.
		c.write(wire.FrameListProjects, wire.ListProjectsReq{})
	case wire.FrameSent:
		// The box is this connection's only sender, so every SENT is its.
		var r wire.SentResp
		if json.Unmarshal(payload, &r) == nil {
			c.HUD.OnSent(r.SessionID)
		}
	case wire.FrameError:
		var e wire.Error
		if json.Unmarshal(payload, &e) == nil && e.SessionID != "" {
			c.HUD.OnError(e.SessionID, e.Code, e.Message)
		}
	case wire.FrameDictation:
		var d wire.Dictation
		if json.Unmarshal(payload, &d) == nil {
			c.Hotkeys.SetHotkey(d.Hotkey)
			c.HUD.SetHandyAutoStart(d.HandyAutoStart)
		}
	}
}

func (c *Conn) feed() {
	c.mu.Lock()
	p := append([]wire.ProjectInfo(nil), c.projects...)
	s := append([]wire.SessionInfo(nil), c.sessions...)
	c.mu.Unlock()
	c.HUD.SetSessions(p, s)
}

func (c *Conn) write(ft wire.FrameType, v any) bool {
	c.mu.Lock()
	cs := c.cs
	c.mu.Unlock()
	if cs == nil {
		return false
	}
	if err := cs.WriteJSON(ft, v); err != nil {
		log.Printf("dictation: send %s: %v", ft, err)
		return false
	}
	return true
}

// SendToSession delivers the box's confirmed text through the same
// SEND_TO_SESSION the GUI's message sheet uses. False when there is no
// connection; the answer arrives later as SENT or ERROR.
func (c *Conn) SendToSession(id, text string, queue bool) bool {
	return c.write(wire.FrameSendToSession, wire.SendToSessionReq{SessionID: id, Text: text, Queue: queue})
}

// ApplySessionEvent patches a session list with one SESSION_EVENT.
// Every kind but removed upserts by ID. An added event can repeat a
// session the snapshot already holds (the daemon subscribes before it
// lists), and unknown kinds carry a full SessionInfo either way.
func ApplySessionEvent(sessions []wire.SessionInfo, ev wire.SessionEvent) []wire.SessionInfo {
	switch ev.Kind {
	case wire.SessionEventRemoved:
		out := sessions[:0]
		for _, s := range sessions {
			if s.ID != ev.Session.ID {
				out = append(out, s)
			}
		}
		return out
	}
	for i, s := range sessions {
		if s.ID == ev.Session.ID {
			sessions[i] = ev.Session
			return sessions
		}
	}
	return append(sessions, ev.Session)
}
