// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/lucascaro/hive/internal/acp"
	"github.com/lucascaro/hive/internal/wire"
)

// `hived mcp-pin` is the stdio MCP server that gives Claude a `pin`
// tool (spec 566): the agent pins a response it wants the user to be
// able to recall, and Hive shows it in the session's inspector. The
// session's HIVE_SESSION_ID, HIVE_SOCKET and HIVE_SESSION_TOKEN come in
// through its environment, never through the tool's arguments, so a
// pin can only ever land in the session that started the server.

// pinTool is the tool's MCP definition. Its description carries the
// "when to pin" guidance too, because an ACP session has no SessionStart
// hook to deliver the nudge PTY Claude gets.
var pinTool = map[string]any{
	"name": acp.PinToolName,
	"description": "Pin an important response so the user can find it again in Hive's sidebar. " +
		"Pin sparingly and on your own initiative: a decision and its reasons, a root cause, " +
		"a command recipe, a summary the user will want later. Not routine progress. " +
		"The title says what the pin is; the body is the content, in markdown.",
	"inputSchema": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": map[string]any{"type": "string", "description": "A short title naming what this is (at most 256 bytes)."},
			"body":  map[string]any{"type": "string", "description": "The content to keep, in markdown (at most 64 KiB)."},
		},
		"required":             []string{"title", "body"},
		"additionalProperties": false,
	},
}

// runMCPPin serves the pin tool on in/out until in closes. pin delivers
// one pin; it is pinOverSocket outside tests.
func runMCPPin(in io.Reader, out io.Writer, pin func(title, body string) error) int {
	return serveMCP(in, out, []any{pinTool}, func(name string, args json.RawMessage) (string, bool, bool) {
		if name != acp.PinToolName {
			return "", false, false
		}
		var a struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		if err := json.Unmarshal(args, &a); err != nil || strings.TrimSpace(a.Title) == "" || strings.TrimSpace(a.Body) == "" {
			return "pin needs a non-empty title and body", true, true
		}
		if err := pin(a.Title, a.Body); err != nil {
			return "Hive did not pin it: " + err.Error(), true, true
		}
		return "Pinned.", false, true
	})
}

// pinOverSocket sends one ADD_PIN for the session named in env.
func pinOverSocket(env func(string) string) func(title, body string) error {
	return func(title, body string) error {
		_, err := sessionRoundTrip(env, "hived-mcp-pin", wire.FrameAddPin,
			wire.AddPinReq{Title: title, Body: body}, wire.FrameAddPinOK)
		return err
	}
}

func runMCPPinMain() int {
	return runMCPPin(os.Stdin, os.Stdout, pinOverSocket(os.Getenv))
}
