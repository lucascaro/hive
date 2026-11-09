// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package wire

import (
	"encoding/json"
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
	// Committed and Head, on a COMMIT_WORKTREE ack, say whether a commit
	// was made (absent = false: the tree was clean, a no-op) and the
	// worktree's HEAD afterwards.
	Committed bool   `json:"committed,omitempty"`
	Head      string `json:"head,omitempty"`
}

// Error codes for provenance and typed results.
const (
	// ErrCodeInvalidWorkflowRunID: workflow_run_id is not
	// ^[a-z0-9][a-z0-9-]{0,62}$.
	ErrCodeInvalidWorkflowRunID = "invalid_workflow_run_id"
	// ErrCodeInvalidResultSchema: result_schema is not a schema in the
	// workflow IR subset, or is too big or too deep.
	ErrCodeInvalidResultSchema = "invalid_result_schema"

	// ErrCodeInvalidPluginMessage: PLUGIN_MESSAGE topic is not
	// ^[a-z][a-z0-9._-]{0,63}$.
	ErrCodeInvalidPluginMessage = "invalid_plugin_message"
	// ErrCodePluginMessageTooLarge: the PLUGIN_MESSAGE payload is over
	// MaxPluginMessage.
	ErrCodePluginMessageTooLarge = "plugin_message_too_large"
	// ErrCodePluginNotRunning: a GUI sent PLUGIN_MESSAGE to a plugin that
	// has no live control connection.
	ErrCodePluginNotRunning = "plugin_not_running"
	// ErrCodeInvalidWorkflowBranch: a PREPARE_WORKTREE / COMMIT_WORKTREE
	// branch is not a valid hive/wf/<run>/... ref name.
	ErrCodeInvalidWorkflowBranch = "invalid_workflow_branch"
	// ErrCodeInvalidBase: PREPARE_WORKTREE base does not resolve to a
	// commit.
	ErrCodeInvalidBase = "invalid_base"
	// ErrCodeGitIdentityMissing: a merge or commit needs the user's git
	// identity and the repo has none configured. There is no fallback.
	ErrCodeGitIdentityMissing = "git_identity_missing"
	// ErrCodeWorktreeNotFound: COMMIT_WORKTREE named a branch no
	// hive-managed worktree has checked out.
	ErrCodeWorktreeNotFound = "worktree_not_found"
	// ErrCodeGitUnsupported: the installed git is a release whose hook
	// suppression cannot be proven (2.54); workflow worktrees refuse it.
	ErrCodeGitUnsupported = "git_unsupported"
)

// MaxPluginMessage caps a PLUGIN_MESSAGE payload, in bytes of JSON. A
// plugin and its GUI exchange small state; anything bigger belongs in a
// file the plugin owns.
const MaxPluginMessage = 64 << 10

var pluginTopicRe = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

// ValidPluginTopic reports whether s is an acceptable PLUGIN_MESSAGE topic.
func ValidPluginTopic(s string) bool { return pluginTopicRe.MatchString(s) }

// PluginMessage is the PLUGIN_MESSAGE payload, in both directions. It
// has no req_id: a sender that needs an answer carries its own nonce in
// Data. From a plugin, PluginID is overwritten with the sender's id; from
// a GUI it names the target plugin.
type PluginMessage struct {
	PluginID string          `json:"plugin_id"`
	Topic    string          `json:"topic"`
	Data     json.RawMessage `json:"data,omitempty"`
}

// PrepareWorktreeReq is the PREPARE_WORKTREE payload: a worktree on the
// run-owned branch Branch (hive/wf/<WorkflowRunID>/...), created from
// Base, with each branch of Merge merged into it in order. Answered by
// PREPARED_WORKTREE, or an ERROR carrying req_id.
type PrepareWorktreeReq struct {
	ReqID         int      `json:"req_id,omitempty"`
	ProjectID     string   `json:"project_id"`
	WorkflowRunID string   `json:"workflow_run_id"`
	Branch        string   `json:"branch"`
	Base          string   `json:"base,omitempty"`
	Merge         []string `json:"merge,omitempty"`
}

// MergeConflict is the typed outcome of a merge that stopped on
// conflicts: the branch that did not merge and the files in conflict.
type MergeConflict struct {
	Branch string   `json:"branch"`
	Files  []string `json:"files"`
}

// PreparedWorktreeMsg is the PREPARED_WORKTREE payload. Path and Branch
// are authoritative. Reattached means Branch already existed and Base
// was ignored. A Conflict leaves the worktree in place, at Base plus the
// merges that succeeded (Merged), for the user to resolve by hand.
type PreparedWorktreeMsg struct {
	ReqID      int    `json:"req_id,omitempty"`
	ProjectID  string `json:"project_id"`
	Path       string `json:"path"`
	Branch     string `json:"branch"`
	Reattached bool   `json:"reattached"`
	// BaseSHA is the commit Base resolved to; empty when Reattached.
	BaseSHA string `json:"base_sha,omitempty"`
	// HeadSHA is the worktree's HEAD after any merges.
	HeadSHA  string         `json:"head_sha"`
	Merged   []string       `json:"merged"`
	Conflict *MergeConflict `json:"conflict,omitempty"`
}

// CommitWorktreeReq is the COMMIT_WORKTREE payload: commit everything in
// the worktree that has Branch checked out. Answered by ACK{committed,
// head}.
type CommitWorktreeReq struct {
	ReqID         int    `json:"req_id,omitempty"`
	ProjectID     string `json:"project_id"`
	WorkflowRunID string `json:"workflow_run_id"`
	Branch        string `json:"branch"`
	Message       string `json:"message"`
}

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
