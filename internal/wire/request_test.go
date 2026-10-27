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

// Every payload spec 528 added survives an encode/decode round trip with
// every field populated.
func TestWorkflowPayloadsRoundTrip(t *testing.T) {
	roundTrip := func(name string, in, out any) {
		t.Helper()
		b, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, _ := json.Marshal(out)
		if string(got) != string(b) {
			t.Errorf("%s: round trip changed the payload\n in: %s\nout: %s", name, b, got)
		}
	}
	in1 := AckMsg{ReqID: 1, SessionID: "s", WasBusy: true, PromptID: 2, Epoch: 3}
	var out1 AckMsg
	roundTrip("AckMsg", in1, &out1)
	if out1 != in1 {
		t.Errorf("AckMsg = %+v, want %+v", out1, in1)
	}
	in2 := CancelAcpReq{SessionID: "s", ReqID: 4, PromptID: 5, Epoch: 6}
	var out2 CancelAcpReq
	roundTrip("CancelAcpReq", in2, &out2)
	if out2 != in2 {
		t.Errorf("CancelAcpReq = %+v, want %+v", out2, in2)
	}
	in3 := AcpTranscriptMsg{SessionID: "s", Epoch: 1, PromptID: 2, ResultStatus: AcpResultSubmitted,
		Result: json.RawMessage(`{"status":"ok"}`), StopReason: "cancelled", Usage: json.RawMessage(`{"used":1,"size":2}`)}
	var out3 AcpTranscriptMsg
	roundTrip("AcpTranscriptMsg", in3, &out3)
	if out3.StopReason != "cancelled" || string(out3.Usage) != `{"used":1,"size":2}` {
		t.Errorf("AcpTranscriptMsg = %+v", out3)
	}
	in4 := AcpAgentsMsg{ReqID: 7, Agents: []AcpAgentInfo{{ID: "claude", Name: "Claude", Modes: []AcpAgentMode{{ID: "plan", Label: "Plan"}},
		DefaultMode: "default", Ceiling: "acceptEdits", NoApprovalGate: true, Experimental: true, Available: true, Reason: "r"}}}
	var out4 AcpAgentsMsg
	roundTrip("AcpAgentsMsg", in4, &out4)
	if out4.ReqID != 7 || len(out4.Agents) != 1 || out4.Agents[0].Modes[0].Label != "Plan" || !out4.Agents[0].NoApprovalGate {
		t.Errorf("AcpAgentsMsg = %+v", out4)
	}
	var out5 ListAcpAgentsReq
	roundTrip("ListAcpAgentsReq", ListAcpAgentsReq{ReqID: 9}, &out5)
	if out5.ReqID != 9 {
		t.Errorf("ListAcpAgentsReq = %+v", out5)
	}
	var out6 Welcome
	roundTrip("Welcome", Welcome{Version: 1, BootID: "abc"}, &out6)
	if out6.BootID != "abc" {
		t.Errorf("Welcome = %+v", out6)
	}
	var out7 RestartSessionReq
	roundTrip("RestartSessionReq", RestartSessionReq{SessionID: "s", ReqID: 3}, &out7)
	if out7.ReqID != 3 {
		t.Errorf("RestartSessionReq = %+v", out7)
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
