package acp

import (
	"encoding/json"
	"maps"
	"slices"
)

// SubmitToolName is the tool Hive's per-session MCP server (`hived
// mcp-submit`) and its Pi extension expose for a typed result.
const SubmitToolName = "submit_result"

// SubmitIdentities is every structured identity under which an adapter
// reports the submit tool of the MCP server named server: ACP's `name`
// as Claude's adapter fills it (`mcp__<server>__<tool>`), and the
// `rawInput` server+tool pair (`<server>.<tool>`). Both embed the
// session's own server name, which carries its nonce, so a user's MCP
// server that happens to be called "hive" never matches.
func SubmitIdentities(server string) []string {
	return []string{"mcp__" + server + "__" + SubmitToolName, server + "." + SubmitToolName}
}

// toolRef is the part of a tool call that can identify the tool. Every
// field is reported by the adapter, never written by the model: a title
// is free text the model controls (a Bash call can be *titled*
// "submit_result"), so it is never consulted.
type toolRef struct {
	Name     string
	ToolName string
	Meta     json.RawMessage
	RawInput json.RawMessage
}

// ToolIdentity returns the structured identity of the tool a
// permission request is about, from the request's own tool call and
// then from earlier, the tool_call update with the same ToolCallID (nil
// when none was seen). A port of scripts/acp-probe/probe.mjs's
// toolIdentity. ok is false when neither carries one.
func ToolIdentity(tc PermissionToolCall, earlier *Update) (string, bool) {
	refs := []toolRef{{Name: tc.Name, ToolName: tc.ToolName, Meta: tc.Meta, RawInput: tc.RawInput}}
	if earlier != nil {
		refs = append(refs, toolRef{Name: earlier.Name, ToolName: earlier.ToolName, Meta: earlier.Meta, RawInput: earlier.RawInput})
	}
	for _, r := range refs {
		if id, ok := r.identity(); ok {
			return id, true
		}
	}
	return "", false
}

func (r toolRef) identity() (string, bool) {
	// ACP's own tool-call name first, then a vendor _meta namespace.
	if r.Name != "" {
		return r.Name, true
	}
	var meta map[string]json.RawMessage
	if json.Unmarshal(r.Meta, &meta) == nil {
		// Sorted, so the answer never depends on map order when two
		// namespaces both name the tool.
		for _, ns := range slices.Sorted(maps.Keys(meta)) {
			var v map[string]json.RawMessage
			if json.Unmarshal(meta[ns], &v) != nil {
				continue
			}
			for _, k := range []string{"toolName", "tool_name", "name"} {
				var s string
				if json.Unmarshal(v[k], &s) == nil && s != "" {
					return s, true
				}
			}
		}
	}
	if r.ToolName != "" {
		return r.ToolName, true
	}
	var ri struct {
		Server string `json:"server"`
		Tool   string `json:"tool"`
	}
	if json.Unmarshal(r.RawInput, &ri) == nil && ri.Server != "" && ri.Tool != "" {
		return ri.Server + "." + ri.Tool, true
	}
	return "", false
}

// AutoAllowOption returns the option to select, without asking the
// user, for a permission request — and ok only when the request's
// structured identity is exactly one of allowed and it offers an allow
// option. Everything else (another tool, a request with no identity, a
// request with no allow option) is ok == false: it goes to the user.
func AutoAllowOption(req PermissionRequest, earlier *Update, allowed []string) (optionID string, ok bool) {
	id, found := ToolIdentity(req.ToolCall, earlier)
	if !found || !slices.Contains(allowed, id) {
		return "", false
	}
	for _, kind := range []string{"allow_once", "allow_always"} {
		for _, o := range req.Options {
			if o.Kind == kind {
				return o.OptionID, true
			}
		}
	}
	return "", false
}
