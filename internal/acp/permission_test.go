package acp

import (
	"encoding/json"
	"testing"
)

const testServer = "hive-0123abcd"

func permReq(tc PermissionToolCall) PermissionRequest {
	return PermissionRequest{ToolCall: tc, Options: []PermissionOption{
		{OptionID: "allow-once", Name: "Yes", Kind: "allow_once"},
		{OptionID: "reject", Name: "No", Kind: "reject_once"},
	}}
}

func TestAllowsOnlyExactIdentity(t *testing.T) {
	allowed := SubmitIdentities(testServer)
	for _, tc := range []struct {
		name string
		call PermissionToolCall
		want bool
	}{
		{"claude name", PermissionToolCall{ToolCallID: "a", Name: "mcp__" + testServer + "__submit_result"}, true},
		{"claude meta", PermissionToolCall{ToolCallID: "a", Meta: json.RawMessage(`{"claudeCode":{"toolName":"mcp__` + testServer + `__submit_result"}}`)}, true},
		// rawInput is the tool's arguments, which the model writes: a call
		// to some other tool can carry this server's name and the tool's.
		{"forged rawInput server+tool", PermissionToolCall{ToolCallID: "a", RawInput: json.RawMessage(`{"server":"` + testServer + `","tool":"submit_result"}`)}, false},
		{"rawInput mcp name", PermissionToolCall{ToolCallID: "a", RawInput: json.RawMessage(`{"name":"mcp__` + testServer + `__submit_result"}`)}, false},
		{"other tool same server", PermissionToolCall{ToolCallID: "a", Name: "mcp__" + testServer + "__other"}, false},
		{"other session's server", PermissionToolCall{ToolCallID: "a", Name: "mcp__hive-ffff__submit_result"}, false},
		{"prefix only", PermissionToolCall{ToolCallID: "a", Name: "mcp__" + testServer + "__submit_result_x"}, false},
		// The title is model-controlled free text and is never consulted.
		{"title only", PermissionToolCall{ToolCallID: "a", Title: "mcp__" + testServer + "__submit_result"}, false},
		{"bash titled submit", PermissionToolCall{ToolCallID: "a", Name: "Bash", Title: "submit_result"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opt, ok := AutoAllowOption(permReq(tc.call), nil, allowed)
			if ok != tc.want {
				t.Fatalf("AutoAllowOption ok = %v, want %v", ok, tc.want)
			}
			if ok && opt != "allow-once" {
				t.Errorf("option = %q, want allow-once", opt)
			}
		})
	}
}

// Claude's adapter can put the identity on the tool_call update and
// leave the permission request's tool call bare.
func TestIdentityFromEarlierToolCall(t *testing.T) {
	earlier := &Update{SessionUpdate: UpdateToolCall, ToolCallID: "a", Name: "mcp__" + testServer + "__submit_result"}
	opt, ok := AutoAllowOption(permReq(PermissionToolCall{ToolCallID: "a"}), earlier, SubmitIdentities(testServer))
	if !ok || opt != "allow-once" {
		t.Fatalf("AutoAllowOption = %q, %v; want allow-once, true", opt, ok)
	}
	// The request's own identity wins over the earlier one.
	if _, ok := AutoAllowOption(permReq(PermissionToolCall{ToolCallID: "a", Name: "Bash"}), earlier, SubmitIdentities(testServer)); ok {
		t.Error("a request naming another tool was allowed on the strength of an earlier update")
	}
}

// A user's own MCP server called "hive" exposes the same tool name; it
// must never ride on the session's auto-allow.
func TestUserServerNamedHiveRejected(t *testing.T) {
	for _, name := range []string{"mcp__hive__submit_result", "hive.submit_result"} {
		if _, ok := AutoAllowOption(permReq(PermissionToolCall{ToolCallID: "a", Name: name}), nil, SubmitIdentities(testServer)); ok {
			t.Errorf("%q was auto-allowed", name)
		}
	}
}

func TestMissingIdentityFailsClosed(t *testing.T) {
	if id, ok := ToolIdentity(PermissionToolCall{ToolCallID: "a", Title: "submit_result"}, nil); ok {
		t.Fatalf("ToolIdentity found %q in a title-only call", id)
	}
	if _, ok := AutoAllowOption(permReq(PermissionToolCall{ToolCallID: "a"}), nil, SubmitIdentities(testServer)); ok {
		t.Error("a request with no identity was auto-allowed")
	}
	// No allow option offered: nothing to select, so it goes to the user.
	req := PermissionRequest{ToolCall: PermissionToolCall{ToolCallID: "a", Name: "mcp__" + testServer + "__submit_result"},
		Options: []PermissionOption{{OptionID: "reject", Kind: "reject_once"}}}
	if _, ok := AutoAllowOption(req, nil, SubmitIdentities(testServer)); ok {
		t.Error("auto-allowed a request that offered no allow option")
	}
}
