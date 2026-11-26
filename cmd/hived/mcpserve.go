// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/lucascaro/hive/internal/buildinfo"
	"github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/wire"
)

// The stdio MCP servers hived runs inside a session — `mcp-submit`
// (spec 496) and `mcp-pin` (spec 566) — share this loop and the one
// round trip to the daemon each tool call makes.

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// mcpCall runs one tools/call. known is false for a tool this server
// does not have; otherwise text is the result and isErr marks it as a
// failure the model should read.
type mcpCall func(name string, args json.RawMessage) (text string, isErr, known bool)

// serveMCP serves MCP on in/out until in closes, offering tools.
func serveMCP(in io.Reader, out io.Writer, tools []any, call mcpCall) int {
	w := bufio.NewWriter(out)
	send := func(m rpcMsg) {
		m.JSONRPC = "2.0"
		b, _ := json.Marshal(m)
		w.Write(append(b, '\n'))
		w.Flush()
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var m rpcMsg
		if json.Unmarshal(sc.Bytes(), &m) != nil || len(m.ID) == 0 {
			continue // malformed, or a notification (initialized, cancelled)
		}
		switch m.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(m.Params, &p)
			if p.ProtocolVersion == "" {
				p.ProtocolVersion = "2025-06-18"
			}
			send(rpcMsg{ID: m.ID, Result: map[string]any{
				"protocolVersion": p.ProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "hive", "version": buildinfo.Version()},
			}})
		case "ping":
			send(rpcMsg{ID: m.ID, Result: map[string]any{}})
		case "tools/list":
			send(rpcMsg{ID: m.ID, Result: map[string]any{"tools": tools}})
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(m.Params, &p)
			text, isErr, known := call(p.Name, p.Arguments)
			if !known {
				send(rpcMsg{ID: m.ID, Error: &rpcError{Code: -32602, Message: "unknown tool " + p.Name}})
				continue
			}
			send(rpcMsg{ID: m.ID, Result: map[string]any{
				"content": []any{map[string]any{"type": "text", "text": text}},
				"isError": isErr,
			}})
		default:
			send(rpcMsg{ID: m.ID, Error: &rpcError{Code: -32601, Message: "method not found: " + m.Method}})
		}
	}
	return 0
}

const (
	mcpDialTimeout = 3 * time.Second
	// A tool call waits for the daemon's answer before it returns, so
	// the model learns whether it worked.
	mcpRoundTripDeadline = 10 * time.Second
)

// sessionRoundTrip sends one request on a session connection to the
// daemon named in env, as the session named there, and waits for okFrame
// (returning its payload) or an ERROR.
func sessionRoundTrip(env func(string) string, client string, ft wire.FrameType, req any, okFrame wire.FrameType) ([]byte, error) {
	sessionID, sock := env("HIVE_SESSION_ID"), env("HIVE_SOCKET")
	if sessionID == "" || sock == "" {
		return nil, errors.New("not started by Hive (no HIVE_SESSION_ID or HIVE_SOCKET)")
	}
	if err := daemon.CheckSocketDir(sock); err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", sock, mcpDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the daemon: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(mcpRoundTripDeadline))
	c, err := wire.Handshake(conn, wire.Hello{
		Version:      wire.PROTOCOL_VERSION,
		Client:       client + "/" + buildinfo.Version(),
		BuildID:      buildinfo.BuildID(),
		Mode:         wire.ModeSession,
		SessionID:    sessionID,
		SessionToken: env("HIVE_SESSION_TOKEN"),
	})
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(mcpRoundTripDeadline))
	if err := c.WriteJSON(ft, req); err != nil {
		return nil, err
	}
	for {
		got, payload, err := c.ReadFrame()
		if err != nil {
			return nil, err
		}
		switch got {
		case okFrame:
			return payload, nil
		case wire.FrameError:
			return nil, ideaFrameError(payload)
		}
		// Anything else is the session connection's own snapshots.
	}
}
