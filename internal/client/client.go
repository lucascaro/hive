// Package client implements the hive terminal CLI: it speaks the
// internal/wire protocol to a running hived over its Unix socket,
// listing and attaching to sessions from a plain terminal.
package client

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// ClientName is the wire Hello.Client identifier for this binary.
func ClientName() string { return "hive-cli/0.1" }

// Dial connects to a running hived at socketPath. Unlike the GUI, it
// never spawns a daemon — if nothing is listening it returns a friendly
// error pointing at the most likely cause.
func Dial(socketPath string) (net.Conn, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("cannot reach hived at %s — is Hive running? (%w)", socketPath, err)
	}
	return conn, nil
}

// List opens a control-mode conversation on conn and returns the
// daemon's current sessions. conn must be freshly dialed (no prior
// handshake). It is consumed by this call.
func List(conn net.Conn) ([]wire.SessionInfo, error) {
	if err := wire.WriteJSON(conn, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION,
		Client:  ClientName(),
		Mode:    wire.ModeControl,
	}); err != nil {
		return nil, fmt.Errorf("control hello: %w", err)
	}
	var welcome wire.Welcome
	ft, err := wire.ReadJSON(conn, &welcome)
	if err != nil {
		return nil, fmt.Errorf("control welcome: %w", err)
	}
	if ft != wire.FrameWelcome {
		return nil, fmt.Errorf("control: expected WELCOME, got %s", ft)
	}
	if err := wire.WriteJSON(conn, wire.FrameListSessions, wire.ListSessionsReq{}); err != nil {
		return nil, fmt.Errorf("list request: %w", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetReadDeadline(time.Time{}) //nolint:errcheck
	for {
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			return nil, fmt.Errorf("list read: %w", err)
		}
		if ft != wire.FrameSessions {
			continue
		}
		var resp wire.SessionsResp
		if len(payload) > 0 {
			if err := jsonUnmarshal(payload, &resp); err != nil {
				return nil, fmt.Errorf("list decode: %w", err)
			}
		}
		return resp.Sessions, nil
	}
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
