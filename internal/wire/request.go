// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package wire

import (
	"regexp"
	"strings"
)

// Request/reply correlation and run provenance (spec 528).
//
// Any control request may carry an int req_id. The daemon echoes it on
// every ERROR raised while handling that frame and, for a request with
// no reply frame of its own, answers with an ACK.

// AckMsg is the ACK payload: the success half of a request that has no
// reply frame. It is sent only for a non-zero req_id, to the requester.
type AckMsg struct {
	ReqID     int    `json:"req_id"`
	SessionID string `json:"session_id,omitempty"`
	// WasBusy, on a CANCEL_ACP ack, says a turn was running and was asked
	// to stop. The turn is not over yet: it is over when ACP_TRANSCRIPT
	// carries its stop_reason (and PROMPT_ACP answers acp_busy until
	// then). Absent means false: the cancel was a no-op.
	WasBusy bool `json:"was_busy,omitempty"`
	// PromptID and Epoch, on a PROMPT_ACP ack, name the transcript item
	// of the turn just started and the transcript epoch it is in. Pair
	// them with Welcome.BootID to tie a result to its turn.
	PromptID int `json:"prompt_id,omitempty"`
	Epoch    int `json:"epoch,omitempty"`
}

// Error codes for provenance and typed results.
const (
	// ErrCodeInvalidWorkflowRunID: workflow_run_id is not
	// ^[a-z0-9][a-z0-9-]{0,62}$.
	ErrCodeInvalidWorkflowRunID = "invalid_workflow_run_id"
	// ErrCodeInvalidResultSchema: result_schema is not a schema in the
	// workflow IR subset, or is too big or too deep.
	ErrCodeInvalidResultSchema = "invalid_result_schema"
)

var workflowRunIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidWorkflowRunID reports whether s is an acceptable workflow run id.
func ValidWorkflowRunID(s string) bool { return workflowRunIDRe.MatchString(s) }

// WorkflowPrincipalSep joins a plugin principal and its run id:
// "plugin:<id>/workflow:<run>". Plugin ids and run ids never contain
// "/" or ":", so the composition parses back unambiguously.
const WorkflowPrincipalSep = "/workflow:"

// ParsePrincipal splits a provenance principal ("plugin:<id>" or
// "plugin:<id>/workflow:<run>") into its plugin id and run id. Both are
// empty for any other principal (the user, "session:<id>", …), and run
// is empty for a plain plugin principal. It is the one parser; the
// frontend mirrors it in src/lib/principal.ts.
func ParsePrincipal(s string) (plugin, run string) {
	rest, ok := strings.CutPrefix(s, "plugin:")
	if !ok || rest == "" {
		return "", ""
	}
	id, r, hasRun := strings.Cut(rest, WorkflowPrincipalSep)
	if id == "" || strings.ContainsAny(id, "/:") {
		return "", ""
	}
	if !hasRun {
		return id, ""
	}
	if !ValidWorkflowRunID(r) {
		return "", ""
	}
	return id, r
}

// ListAcpAgentsReq is the LIST_ACP_AGENTS payload.
type ListAcpAgentsReq struct {
	ReqID int `json:"req_id,omitempty"`
}

// AcpAgentMode is one permission mode an adapter offers.
type AcpAgentMode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// AcpAgentInfo is one agent in ACP_AGENTS: what can run as an ACP
// session right now, under the user's settings.
type AcpAgentInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Modes are the adapter's permission modes, least permissive first.
	Modes       []AcpAgentMode `json:"modes"`
	DefaultMode string         `json:"default_mode,omitempty"`
	// Ceiling is the most permissive mode the user allows for the agent.
	Ceiling        string `json:"ceiling,omitempty"`
	NoApprovalGate bool   `json:"no_approval_gate,omitempty"`
	Experimental   bool   `json:"experimental,omitempty"`
	// Available is whether an ACP session can start now; Reason says why
	// not when it cannot.
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// AcpAgentsMsg is the ACP_AGENTS payload.
type AcpAgentsMsg struct {
	ReqID  int            `json:"req_id,omitempty"`
	Agents []AcpAgentInfo `json:"agents"`
}
