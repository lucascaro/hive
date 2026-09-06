//go:build darwin

package main

import (
	"encoding/json"
	"log"
	"net"
	"sync"
	"time"

	"github.com/lucascaro/hive/internal/buildinfo"
	hdaemon "github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/wire"
)

// buildContract is a function, not a direct reference, so model.go can
// report it without importing buildinfo (which keeps the model free of
// anything but data).
func buildContract() int { return buildinfo.DaemonContract }

// reconnectDelay is how long hivebar waits before redialling a daemon
// that is not there.
//
// Two seconds, not milliseconds: a menu bar showing "not running" for a
// couple of seconds after the daemon comes up costs the user nothing,
// and hivebar is a background process that may sit dialling for days
// while no daemon exists at all. Each attempt that finds no socket is
// a failed connect(2), not a running daemon's accepted connection, so
// this is about hivebar's own wakeups rather than daemon load.
var reconnectDelay = 2 * time.Second

// publishInterval coalesces menu updates.
//
// The daemon emits a `title` event every time a shell redraws its
// prompt, so an uncoalesced menu retitles itself many times a second
// while the user is reading it. This is the same trailing-coalesce
// shape internal/session uses for the titles themselves, and for the
// same reason: the last state of a burst is the one that matters, and
// nothing here is worth showing at a child process's redraw rate.
//
// Trailing, not dropping — the final update of a burst always lands.
var publishInterval = 400 * time.Millisecond

// Client keeps one control connection to hived and publishes a Model
// whenever the daemon's view changes.
//
// It is a pure wire client: no PTY, no registry, no state-dir writes —
// the same rule the GUI obeys (DESIGN.md). Everything it knows arrives
// on the control connection.
type Client struct {
	// onChange is called with a fresh Model on every update, from the
	// read goroutine. The menu layer marshals onto its own thread.
	onChange func(Model)

	mu       sync.Mutex
	conn     *wire.Client
	welcome  wire.Welcome
	projects []wire.ProjectInfo
	sessions []wire.SessionInfo

	// Coalescing state, guarded by mu. timer is non-nil while a
	// trailing publish is pending; dirty records that something
	// changed while it was.
	timer *time.Timer
	dirty bool
}

func NewClient(onChange func(Model)) *Client {
	return &Client{onChange: onChange}
}

// Run dials the daemon and keeps redialling for the life of the
// process. It never returns.
//
// It does NOT spawn hived when none is running, unlike the GUI's
// dialOrSpawn. A menu bar that started a daemon merely by existing
// would resurrect one the user had just quit, at login, every time.
// hivebar reports; the GUI starts things.
func (c *Client) Run() {
	for {
		if err := c.session(); err != nil {
			log.Printf("hivebar: control: %v", err)
		}
		c.setDisconnected()
		time.Sleep(reconnectDelay)
	}
}

// session runs one connection to exhaustion.
func (c *Client) session() error {
	sock := hdaemon.SocketPath()
	// Refuse an impostor before handshaking with it: hivebar never
	// creates the directory (the daemon or the GUI does), so a check is
	// all that is wanted here.
	if err := hdaemon.CheckSocketDir(sock); err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", sock, 2*time.Second)
	if err != nil {
		return err
	}
	cs, err := wire.Handshake(conn, wire.Hello{
		Client:  "hivebar/0.1",
		BuildID: buildinfo.BuildID(),
		Mode:    wire.ModeControl,
	})
	if err != nil {
		return err
	}
	defer cs.Close()

	c.mu.Lock()
	c.conn = cs
	c.welcome = cs.Welcome()
	// A reconnect must not show the previous daemon's sessions while
	// the new snapshot is in flight.
	c.projects, c.sessions = nil, nil
	c.mu.Unlock()
	c.publish()

	for {
		ft, payload, err := cs.ReadFrame()
		if err != nil {
			c.mu.Lock()
			c.conn = nil
			c.mu.Unlock()
			return err
		}
		c.handleFrame(ft, payload)
	}
}

func (c *Client) handleFrame(ft wire.FrameType, payload []byte) {
	switch ft {
	case wire.FrameSessions:
		var resp wire.SessionsResp
		if json.Unmarshal(payload, &resp) == nil {
			c.mu.Lock()
			c.sessions = resp.Sessions
			c.mu.Unlock()
			c.publish()
		}
	case wire.FrameProjects:
		var resp wire.ProjectsResp
		if json.Unmarshal(payload, &resp) == nil {
			c.mu.Lock()
			c.projects = resp.Projects
			c.mu.Unlock()
			c.publish()
		}
	case wire.FrameSessionEvent:
		var ev wire.SessionEvent
		if json.Unmarshal(payload, &ev) == nil {
			c.applySessionEvent(ev)
			c.publish()
		}
	case wire.FrameProjectEvent:
		// Projects change rarely and the menu shows only their names,
		// so re-listing is simpler than patching a local copy — and it
		// cannot drift.
		c.request(wire.FrameListProjects, wire.ListProjectsReq{})
	}
	// Everything else — CLIENT_BROADCAST included — is not hivebar's
	// business. It relays reload_gui to the windows; it has no window
	// of its own to relaunch.
}

// applySessionEvent patches the local session list.
func (c *Client) applySessionEvent(ev wire.SessionEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch ev.Kind {
	case wire.SessionEventRemoved:
		out := c.sessions[:0]
		for _, s := range c.sessions {
			if s.ID != ev.Session.ID {
				out = append(out, s)
			}
		}
		c.sessions = out
		return
	case wire.SessionEventAdded:
		c.sessions = append(c.sessions, ev.Session)
		return
	}
	// updated / title / attention, and any kind a newer daemon
	// introduces: replace in place. Unknown kinds are treated as an
	// update rather than ignored — the payload is a full SessionInfo
	// either way, so adopting it is strictly closer to the truth than
	// keeping a stale copy.
	for i, s := range c.sessions {
		if s.ID == ev.Session.ID {
			c.sessions[i] = ev.Session
			return
		}
	}
	c.sessions = append(c.sessions, ev.Session)
}

// publish pushes a fresh Model, at most once per publishInterval.
//
// The first change in a quiet period goes out immediately — a menu that
// lagged 400ms behind every click would feel broken — and anything
// during the window that follows is coalesced into one trailing update.
func (c *Client) publish() {
	c.mu.Lock()
	if c.timer != nil {
		// A trailing publish is already pending; it will build from
		// whatever state exists when it fires.
		c.dirty = true
		c.mu.Unlock()
		return
	}
	m := BuildModel(c.projects, c.sessions, c.welcome, buildinfo.DaemonContract)
	c.timer = time.AfterFunc(publishInterval, c.flush)
	c.mu.Unlock()
	c.onChange(m)
}

// flush runs when a coalescing window closes, and publishes again only
// if something changed while it was open.
func (c *Client) flush() {
	c.mu.Lock()
	c.timer = nil
	if !c.dirty {
		c.mu.Unlock()
		return
	}
	c.dirty = false
	m := BuildModel(c.projects, c.sessions, c.welcome, buildinfo.DaemonContract)
	// Re-arm: the burst is evidently still going, so keep coalescing
	// rather than letting the next event through unthrottled.
	c.timer = time.AfterFunc(publishInterval, c.flush)
	c.mu.Unlock()
	c.onChange(m)
}

func (c *Client) setDisconnected() { c.onChange(Disconnected()) }

// request sends one control frame, dropping it if there is no
// connection. Every caller is a menu click, and a click that arrives
// while the daemon is down has nothing useful to report — the menu
// already says it is not running.
func (c *Client) request(ft wire.FrameType, v any) {
	c.mu.Lock()
	cs := c.conn
	c.mu.Unlock()
	if cs == nil {
		return
	}
	if err := cs.WriteJSON(ft, v); err != nil {
		log.Printf("hivebar: send %s: %v", ft, err)
	}
}

// FocusSession asks the GUI windows to bring one session forward.
func (c *Client) FocusSession(id string) {
	c.request(wire.FrameClientCommand, wire.ClientCommand{
		Cmd: wire.CmdFocusSession, SessionID: id,
	})
}

// ReloadGUIs asks every GUI window to relaunch, leaving the daemon and
// every session alone.
func (c *Client) ReloadGUIs() {
	c.request(wire.FrameClientCommand, wire.ClientCommand{Cmd: wire.CmdReloadGUI})
}

// ShutdownDaemon asks hived to stop. This ends every running shell and
// agent, so the caller confirms first.
func (c *Client) ShutdownDaemon() {
	c.request(wire.FrameShutdown, nil)
}

// CheckForUpdates asks the GUI to run its update check. hivebar has no
// updater of its own — see the note in menu.go.
func (c *Client) CheckForUpdates() {
	c.request(wire.FrameClientCommand, wire.ClientCommand{Cmd: wire.CmdCheckUpdate})
}
