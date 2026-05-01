package wire

import (
	"encoding/json"
	"fmt"
	"io"
)

// Hello is the first frame the client sends after connecting.
type Hello struct {
	Version int    `json:"version"`
	Client  string `json:"client"` // free-form, e.g. "hive/0.1.0"
}

// Welcome is the server's response to Hello. It announces the active
// session and the PTY's current dimensions so the client can size its
// terminal widget before live data starts flowing.
type Welcome struct {
	Version   int    `json:"version"`
	SessionID string `json:"session_id"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
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
