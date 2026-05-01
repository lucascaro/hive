package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	hdaemon "github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/wire"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails-bound type. The frontend calls Connect once on
// mount and then drives the session via WriteStdin / Resize.
type App struct {
	ctx context.Context

	mu       sync.Mutex
	conn     net.Conn
	writeMu  sync.Mutex // serializes writes to conn
	sessID   string
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn != nil {
		_ = a.conn.Close()
	}
}

// ConnectInfo is what the frontend gets back from Connect.
type ConnectInfo struct {
	SessionID string `json:"sessionId"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

// Connect attaches to hived. If the daemon isn't running, it tries to
// spawn it as a detached child and retries with backoff.
//
// The frontend passes its current xterm grid size; that becomes the
// PTY's initial size if the daemon is auto-spawned. If the daemon is
// already running with a different size, the frontend should follow
// up with a Resize call after Connect — Welcome reports the daemon's
// current size, not the requested one.
func (a *App) Connect(cols, rows int) (*ConnectInfo, error) {
	a.mu.Lock()
	if a.conn != nil {
		// Already connected (HMR / double-call). Re-emit Welcome-equivalent.
		info := &ConnectInfo{SessionID: a.sessID, Cols: cols, Rows: rows}
		a.mu.Unlock()
		return info, nil
	}
	a.mu.Unlock()

	conn, err := dialOrSpawn(hdaemon.SocketPath(), cols, rows)
	if err != nil {
		return nil, err
	}

	// Handshake.
	if err := wire.WriteJSON(conn, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION,
		Client:  "hivegui/0.1",
	}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("hello: %w", err)
	}
	var welcome wire.Welcome
	ft, err := wire.ReadJSON(conn, &welcome)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("welcome: %w", err)
	}
	if ft == wire.FrameError {
		_ = conn.Close()
		return nil, fmt.Errorf("hived rejected handshake")
	}
	if ft != wire.FrameWelcome {
		_ = conn.Close()
		return nil, fmt.Errorf("unexpected frame %s during handshake", ft)
	}

	a.mu.Lock()
	a.conn = conn
	a.sessID = welcome.SessionID
	a.mu.Unlock()

	go a.readLoop(conn)

	return &ConnectInfo{
		SessionID: welcome.SessionID,
		Cols:      welcome.Cols,
		Rows:      welcome.Rows,
	}, nil
}

// readLoop drains frames from the daemon and emits Wails events to
// the frontend. DATA bytes go to "pty:data" (base64-encoded). The
// scrollback_replay_done event becomes "pty:replay-done".
func (a *App) readLoop(conn net.Conn) {
	defer func() {
		a.mu.Lock()
		if a.conn == conn {
			a.conn = nil
		}
		a.mu.Unlock()
		_ = conn.Close()
		wruntime.EventsEmit(a.ctx, "pty:disconnect", "")
	}()

	for {
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("hivegui: read frame: %v", err)
			}
			return
		}
		switch ft {
		case wire.FrameData:
			wruntime.EventsEmit(a.ctx, "pty:data", base64.StdEncoding.EncodeToString(payload))
		case wire.FrameEvent:
			// Just forward the JSON payload to the frontend; the
			// frontend cares about kind=="scrollback_replay_done".
			wruntime.EventsEmit(a.ctx, "pty:event", string(payload))
		case wire.FrameError:
			wruntime.EventsEmit(a.ctx, "pty:error", string(payload))
		default:
			log.Printf("hivegui: unexpected frame %s", ft)
		}
	}
}

// WriteStdin forwards keystrokes (base64-encoded by the frontend for
// binary safety) to hived as a DATA frame.
func (a *App) WriteStdin(b64 string) error {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return err
	}
	return a.send(wire.FrameData, data)
}

// Resize sends a RESIZE control frame.
func (a *App) Resize(cols, rows int) error {
	return a.sendJSON(wire.FrameResize, wire.Resize{Cols: cols, Rows: rows})
}

func (a *App) send(t wire.FrameType, p []byte) error {
	a.mu.Lock()
	conn := a.conn
	a.mu.Unlock()
	if conn == nil {
		return errors.New("not connected")
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return wire.WriteFrame(conn, t, p)
}

func (a *App) sendJSON(t wire.FrameType, v any) error {
	a.mu.Lock()
	conn := a.conn
	a.mu.Unlock()
	if conn == nil {
		return errors.New("not connected")
	}
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return wire.WriteJSON(conn, t, v)
}

// dialOrSpawn dials the hived socket. On failure, attempts to spawn a
// new hived as a detached child and retries with backoff for up to ~3s.
func dialOrSpawn(sock string, cols, rows int) (net.Conn, error) {
	if c, err := net.Dial("unix", sock); err == nil {
		return c, nil
	}
	// Spawn.
	if err := spawnHived(sock, cols, rows); err != nil {
		return nil, fmt.Errorf("spawn hived: %w", err)
	}
	// Backoff dial.
	delays := []time.Duration{100, 200, 400, 800, 1600}
	for _, ms := range delays {
		time.Sleep(ms * time.Millisecond)
		c, err := net.Dial("unix", sock)
		if err == nil {
			return c, nil
		}
	}
	return nil, fmt.Errorf("hived did not come up at %s", sock)
}
