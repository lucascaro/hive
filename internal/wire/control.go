package wire

import (
	"encoding/json"
	"fmt"
	"io"
)

// Mode is the connection mode chosen by the client in HELLO. It
// determines the server-side dispatch and which frame types are
// allowed on the connection for the rest of its lifetime.
type Mode string

const (
	ModeControl Mode = "control" // session management; never streams DATA
	ModeAttach  Mode = "attach"  // attach to an existing session by ID
	ModeCreate  Mode = "create"  // create a new session, then behave as attach
)

// CreateSpec is the payload for ModeCreate's create field, and also
// the standalone CREATE_SESSION control frame.
type CreateSpec struct {
	Name  string `json:"name,omitempty"`
	Color string `json:"color,omitempty"`
	Cols  int    `json:"cols,omitempty"`
	Rows  int    `json:"rows,omitempty"`
	Shell string `json:"shell,omitempty"`
	Cwd   string `json:"cwd,omitempty"` // working directory; default = daemon's
	// Cmd, when set, runs in place of the shell. Phase 3 will use this
	// for agent launchers; Phase 2 leaves it empty.
	Cmd []string `json:"cmd,omitempty"`
}

// Hello is the first frame the client sends after connecting.
type Hello struct {
	Version int    `json:"version"`
	Client  string `json:"client"` // free-form, e.g. "hive/0.2.0"

	// v1 fields:
	Mode      Mode        `json:"mode,omitempty"`
	SessionID string      `json:"session_id,omitempty"` // ModeAttach
	Create    *CreateSpec `json:"create,omitempty"`     // ModeCreate
}

// Welcome is the server's response to Hello. For attach/create modes
// it announces the active session and PTY dimensions so the client
// can size its terminal widget before live data flows. For control
// mode SessionID is empty.
type Welcome struct {
	Version   int    `json:"version"`
	Mode      Mode   `json:"mode,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
}

// SessionInfo is the public-facing description of one daemon session.
// It is what the client sees in SESSIONS and SESSION_EVENT payloads.
type SessionInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Color   string `json:"color"`
	Order   int    `json:"order"`
	Created string `json:"created"` // RFC 3339
	Alive   bool   `json:"alive"`
}

// ListSessionsReq is the LIST_SESSIONS payload (currently empty).
type ListSessionsReq struct{}

// SessionsResp is the SESSIONS payload returned in response to LIST_SESSIONS.
type SessionsResp struct {
	Sessions []SessionInfo `json:"sessions"`
}

// KillSessionReq is the KILL_SESSION payload.
type KillSessionReq struct {
	SessionID string `json:"session_id"`
}

// UpdateSessionReq mutates session metadata. Pointer fields are
// "omit if not setting". Order is *int because 0 is a valid value
// and we need to distinguish "no change" from "set to zero".
type UpdateSessionReq struct {
	SessionID string  `json:"session_id"`
	Name      *string `json:"name,omitempty"`
	Color     *string `json:"color,omitempty"`
	Order     *int    `json:"order,omitempty"`
}

// SessionEventKind enumerates the kinds carried by SESSION_EVENT.
const (
	SessionEventAdded   = "added"
	SessionEventRemoved = "removed"
	SessionEventUpdated = "updated"
)

// SessionEvent is the SESSION_EVENT payload, broadcast to every
// control connection on any registry change.
type SessionEvent struct {
	Kind    string      `json:"kind"`
	Session SessionInfo `json:"session"`
}

// Resize is sent by the client whenever its terminal widget changes
// size. The server forwards the new dimensions to the PTY.
type Resize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// Event covers asynchronous server-to-client notifications.
type Event struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Well-known event kinds.
const (
	EventScrollbackReplayDone = "scrollback_replay_done"
	EventSessionExit          = "session_exit"
)

// Error is sent by the server when something goes wrong.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON marshals v and writes it as a frame of type t.
func WriteJSON(w io.Writer, t FrameType, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("wire: marshal %s: %w", t, err)
	}
	return WriteFrame(w, t, b)
}

// ReadJSON reads the next frame and unmarshals its payload into v.
// Returns the actual frame type so the caller can distinguish unexpected
// messages without re-reading.
func ReadJSON(r io.Reader, v any) (FrameType, error) {
	t, payload, err := ReadFrame(r)
	if err != nil {
		return 0, err
	}
	if v != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, v); err != nil {
			return t, fmt.Errorf("wire: unmarshal %s: %w", t, err)
		}
	}
	return t, nil
}
