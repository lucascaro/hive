// App methods for per-session attach connections: opening a session's
// PTY stream, the per-attach read loop, stdin/resize/replay, and
// teardown. Split out of app.go; see app.go for the App type itself.
package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/lucascaro/hive/internal/wire"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ----------------------------- attach conns -----------------------------

// AttachInfo is what the frontend gets back from OpenSession.
type AttachInfo struct {
	SessionID string `json:"sessionId"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

// OpenSession opens an attach connection to the given session. The
// frontend should call this once per session it wants to render.
// PTY bytes arrive as "pty:data" events tagged with the session id.
func (a *App) OpenSession(id string, cols, rows int) (*AttachInfo, error) {
	// Serialize across all in-flight OpenSession calls so the dial +
	// handshake below can't race against itself for the same id and
	// register two daemon subscribers. See openMu's doc for why.
	a.openMu.Lock()
	defer a.openMu.Unlock()

	a.mu.Lock()
	if _, ok := a.attaches[id]; ok {
		a.mu.Unlock()
		return &AttachInfo{SessionID: id, Cols: cols, Rows: rows}, nil // already open
	}
	a.mu.Unlock()

	dialStart := time.Now()
	cs, err := a.dialHandshake(wire.Hello{
		Client:    "hivegui/0.2",
		Mode:      wire.ModeAttach,
		SessionID: id,
	}, attachDialBudget)
	if err != nil {
		return nil, fmt.Errorf("attach failed: %w", err)
	}
	welcome := cs.Welcome()
	// Startup-latency probe: dial+handshake is a network round-trip to
	// hived per session. On a many-session grid launch these run behind
	// openMu (serialized), so a slow daemon shows here as the sum that
	// stalls startup. Logged to hivegui.log next to the frontend probes.
	log.Printf("hivegui[fe]: OpenSession id=%s dialHandshake=%dms", id, time.Since(dialStart).Milliseconds())

	a.mu.Lock()
	a.attaches[id] = cs
	a.mu.Unlock()
	go a.attachReadLoop(id, cs)

	// Issue the frontend's preferred size right after the handshake;
	// the daemon's WELCOME reports its current size which may differ.
	if cols > 0 && rows > 0 && (cols != welcome.Cols || rows != welcome.Rows) {
		_ = cs.WriteJSON(wire.FrameResize, wire.Resize{Cols: cols, Rows: rows})
	}

	return &AttachInfo{
		SessionID: id, Cols: welcome.Cols, Rows: welcome.Rows,
	}, nil
}

func (a *App) attachReadLoop(id string, cs *wire.Client) {
	defer func() {
		a.mu.Lock()
		if a.attaches[id] == cs {
			delete(a.attaches, id)
		}
		a.mu.Unlock()
		_ = cs.Close()
		wruntime.EventsEmit(a.ctx, "pty:disconnect", id)
	}()
	// Startup-flood probe: sum the FrameData bytes in the first second
	// after attach and log once. The initial subscribe replays the
	// session's scrollback ring (up to a few MB); many sessions doing
	// this at once floods pty:data events into the webview and can stall
	// its main thread. This quantifies the initial burst per session.
	loopStart := time.Now()
	var initBytes int
	var initFrames int
	initLogged := false
	logInitBurst := func() {
		if initLogged {
			return
		}
		initLogged = true
		log.Printf("hivegui[fe]: attach id=%s initial burst frames=%d bytes=%d in %dms",
			id, initFrames, initBytes, time.Since(loopStart).Milliseconds())
	}
	for {
		ft, payload, err := cs.ReadFrame()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("hivegui: attach %s read: %v", id, err)
			}
			return
		}
		if !initLogged {
			if ft == wire.FrameData {
				initFrames++
				initBytes += len(payload)
			}
			// Flush the burst summary once the initial replay settles
			// (1s quiet-ish window) or on the first non-data frame.
			if time.Since(loopStart) > time.Second || ft == wire.FrameEvent {
				logInitBurst()
			}
		}
		name, ok := wire.AttachEventName(ft)
		switch {
		case !ok:
			log.Printf("hivegui: attach %s unexpected frame %s", id, ft)
		case ft == wire.FrameData:
			wruntime.EventsEmit(a.ctx, name, id, base64.StdEncoding.EncodeToString(payload))
		default:
			wruntime.EventsEmit(a.ctx, name, id, string(payload))
		}
	}
}

// CloseAttach drops the GUI's attach connection without killing the
// underlying session. Equivalent to "stop rendering this tab" — useful
// once we have N sessions and want to free the connection slot.
func (a *App) CloseAttach(id string) error {
	a.mu.Lock()
	cs, ok := a.attaches[id]
	if ok {
		delete(a.attaches, id)
	}
	a.mu.Unlock()
	if !ok {
		return nil
	}
	return cs.Close()
}

// WriteStdin forwards keystrokes to the attached session.
func (a *App) WriteStdin(id, b64 string) error {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return err
	}
	cs, err := a.attachFor(id)
	if err != nil {
		return err
	}
	return cs.WriteFrame(wire.FrameData, data)
}

// ResizeSession sends a RESIZE control frame on the attach connection.
func (a *App) ResizeSession(id string, cols, rows int) error {
	cs, err := a.attachFor(id)
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameResize, wire.Resize{Cols: cols, Rows: rows})
}

// RequestScrollbackReplay asks the daemon to re-stream the session's
// scrollback byte ring. The GUI uses this after a width-changing
// resize (single ↔ grid transitions) because xterm.js does not reflow
// scrollback when its column count changes — replaying the raw bytes
// into a freshly-reset terminal gets the history rendered at the new
// width. The daemon serializes the replay against live PTY fanout, so
// the client sees a clean Begin/bytes/Done sequence even under heavy
// streaming.
//
// Distinct from RenderSnapshot / SubscribeAtomicSnapshot — the bytes
// streamed back are the raw PTY ring, not the vt10x-synthesized
// repaint.
func (a *App) RequestScrollbackReplay(id string) error {
	cs, err := a.attachFor(id)
	if err != nil {
		return err
	}
	return cs.WriteFrame(wire.FrameRequestReplay, nil)
}

func (a *App) attachFor(id string) (*wire.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cs, ok := a.attaches[id]
	if !ok {
		return nil, fmt.Errorf("not attached to %s", id)
	}
	return cs, nil
}
