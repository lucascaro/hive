// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"

	"github.com/lucascaro/hive/internal/acp"
	"github.com/lucascaro/hive/internal/wire"
)

// `hived mcp-submit` is the stdio MCP server an ACP session's adapter
// starts for it (spec 496): one tool, submit_result, whose arguments are
// the session's typed result. The daemon names the server per start and
// passes the session's HIVE_SESSION_ID, HIVE_SOCKET and HIVE_SUBMIT_NONCE
// in its environment only — never in the tool's schema — so the model
// is not handed the nonce, and a process that merely holds HIVE_SOCKET
// (another session's agent, a copied environment) cannot put a result
// in this session. It is not a boundary against a process running as
// the same user, which can read another process's environment; Hive is
// not a sandbox (docs/design-docs/acp-session-kind.md). A port of scripts/acp-probe/submit-mcp.mjs, which wrote to a
// file; this one reports over the session's events socket.

// submitTool is the tool's MCP definition. The nonce is deliberately
// absent: the model never sees it.
var submitTool = map[string]any{
	"name":        acp.SubmitToolName,
	"description": "Submit the typed result of this task to Hive. Call it exactly once, when the task is done (status ok) or cannot be done (status error).",
	"inputSchema": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status":  map[string]any{"type": "string", "enum": []string{"ok", "error"}},
			"summary": map[string]any{"type": "string", "description": "One or two sentences on the outcome."},
			"data":    map[string]any{"type": "object", "description": "Structured result fields, if the task asked for any."},
		},
		"required":             []string{"status"},
		"additionalProperties": false,
	},
}

// toolFor is submitTool, with the session's result schema (when it has
// one) as the schema of `data`. The daemon's Registry.SubmitResult is the
// authority; showing it here lets the model write the right shape first
// time.
func toolFor(schema *acp.Schema) map[string]any {
	if schema == nil {
		return submitTool
	}
	tool := maps.Clone(submitTool)
	in := maps.Clone(submitTool["inputSchema"].(map[string]any))
	props := maps.Clone(in["properties"].(map[string]any))
	props["data"] = schema.JSON()
	in["properties"] = props
	tool["inputSchema"] = in
	tool["description"] = submitTool["description"].(string) + " With status ok, data is required and must match its schema."
	return tool
}

// runMCPSubmit serves MCP on in/out until in closes. submit delivers one
// result; it is submitOverSocket outside tests.
func runMCPSubmit(in io.Reader, out io.Writer, submit func(json.RawMessage) error) int {
	return runMCPSubmitSchema(in, out, submit, nil)
}

// runMCPSubmitSchema is runMCPSubmit for a session with a result schema
// (HIVE_SUBMIT_SCHEMA): nil means the plain tool.
func runMCPSubmitSchema(in io.Reader, out io.Writer, submit func(json.RawMessage) error, schema *acp.Schema) int {
	return serveMCP(in, out, []any{toolFor(schema)}, func(name string, args json.RawMessage) (string, bool, bool) {
		if name != acp.SubmitToolName {
			return "", false, false
		}
		if err := checkSubmitArgs(args, schema); err != nil {
			return err.Error(), true, true
		}
		if err := submit(args); err != nil {
			return "Hive did not record the result: " + err.Error(), true, true
		}
		return "Result recorded.", false, true
	})
}

// checkSubmitArgs enforces the schema's one hard rule — and, for a
// session with a result schema, the same data check the daemon makes — so
// a model that ignores the schema is told so and can call again.
func checkSubmitArgs(args json.RawMessage, schema *acp.Schema) error {
	if _, err := acp.CanonicalEnvelope(schema, args); err != nil {
		return fmt.Errorf("%v; call %s again", err, acp.SubmitToolName)
	}
	return nil
}

// submitOverSocket sends one SUBMIT_RESULT on a session connection to
// the daemon named in env, and waits for its ack.
func submitOverSocket(env func(string) string) func(json.RawMessage) error {
	return func(result json.RawMessage) error {
		sessionID, sock, nonce := env("HIVE_SESSION_ID"), env("HIVE_SOCKET"), env("HIVE_SUBMIT_NONCE")
		if sessionID == "" || sock == "" || nonce == "" {
			return errors.New("not started by Hive (no HIVE_SESSION_ID, HIVE_SOCKET or HIVE_SUBMIT_NONCE)")
		}
		_, err := sessionRoundTrip(env, "hived-mcp-submit", wire.FrameSubmitResult,
			wire.SubmitResultReq{Nonce: nonce, Result: result}, wire.FrameSubmitResultOK)
		return err
	}
}

func runMCPSubmitMain() int {
	var schema *acp.Schema
	if raw := os.Getenv("HIVE_SUBMIT_SCHEMA"); raw != "" {
		s, err := acp.ParseSchema(json.RawMessage(raw))
		if err != nil {
			// The daemon checked it at create; running without it only
			// loses the pre-check, and the daemon still enforces it.
			fmt.Fprintf(os.Stderr, "hived mcp-submit: ignoring HIVE_SUBMIT_SCHEMA: %v\n", err)
		}
		schema = s
	}
	return runMCPSubmitSchema(os.Stdin, os.Stdout, submitOverSocket(os.Getenv), schema)
}
