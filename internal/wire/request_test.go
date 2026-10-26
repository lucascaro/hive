// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package wire

import (
	"encoding/json"
	"testing"
)

func TestParsePrincipal(t *testing.T) {
	for _, c := range []struct{ in, plugin, run string }{
		{"", "", ""},
		{"user", "", ""},
		{"plugin:engine", "engine", ""},
		{"plugin:engine/workflow:r1", "engine", "r1"},
		{"plugin:engine/workflow:run-2-a", "engine", "run-2-a"},
		{"plugin:", "", ""},
		{"plugin:/workflow:r1", "", ""},
		{"plugin:engine/workflow:", "", ""},
		{"plugin:engine/workflow:R1", "", ""},
		{"plugin:x/../plugin:q", "", ""},
		{"plugin:a:b", "", ""},
		{"session:abc", "", ""},
		{"workflow:r1", "", ""},
	} {
		p, r := ParsePrincipal(c.in)
		if p != c.plugin || r != c.run {
			t.Errorf("ParsePrincipal(%q) = (%q, %q), want (%q, %q)", c.in, p, r, c.plugin, c.run)
		}
	}
}

func TestValidWorkflowRunID(t *testing.T) {
	good := []string{"a", "0", "run-1", "a1b2c3d4", "x-"}
	for _, s := range good {
		if !ValidWorkflowRunID(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	long := make([]byte, 64)
	for i := range long {
		long[i] = 'a'
	}
	for _, s := range []string{"", "-a", "A", "a_b", "a/b", "a:b", "a b", "a\n", string(long)} {
		if ValidWorkflowRunID(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
	if !ValidWorkflowRunID(string(long[:63])) {
		t.Error("63 chars should be valid")
	}
}

// The new fields are snake_case on the wire (DESIGN.md hard rule).
func TestRequestFieldsAreSnakeCase(t *testing.T) {
	b, _ := json.Marshal(AckMsg{ReqID: 1, SessionID: "s", WasBusy: true, PromptID: 2, Epoch: 3})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"req_id", "session_id", "was_busy", "prompt_id", "epoch"} {
		if _, ok := m[k]; !ok {
			t.Errorf("AckMsg lacks %q: %s", k, b)
		}
	}
	b, _ = json.Marshal(Error{Code: "c", ReqID: 7})
	if string(b) != `{"code":"c","message":""`+`,"req_id":7}` {
		t.Errorf("Error = %s", b)
	}
	b, _ = json.Marshal(CreateSpec{ReqID: 1, WorkflowRunID: "r", ResultSchema: json.RawMessage(`{}`)})
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"req_id", "workflow_run_id", "result_schema"} {
		if _, ok := m[k]; !ok {
			t.Errorf("CreateSpec lacks %q: %s", k, b)
		}
	}
}
