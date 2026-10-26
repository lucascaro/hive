// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package daemon

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/acp/acptest"
	"github.com/lucascaro/hive/internal/wire"
)

// Provenance (spec 528): a plugin's work carries "plugin:<id>/workflow:
// <run>", composed by the daemon from its own principal and the run id
// the plugin sends. An integrity label, not a boundary.

func infoOf(t *testing.T, d *Daemon, id string) wire.SessionInfo {
	t.Helper()
	for _, s := range d.reg.List() {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("session %s not found", id)
	return wire.SessionInfo{}
}

func TestPluginPrincipal_ComposedWithRun(t *testing.T) {
	fakeACPDir(t)
	d := startTestDaemon(t)
	pc, _ := pluginConn(t, d, "wf", "p")
	// spawned_by and orchestrator in the JSON are the client's claims and
	// carry no weight: the first is never decoded, the second is forced off.
	send(t, pc, wire.FrameCreateSession, acpSpec(`,"req_id":1,"workflow_run_id":"r-1","spawned_by":"plugin:other","orchestrator":true`))
	ack := readFrameWhere(t, pc, "ACK", isFrame(wire.FrameAck)).ack(t)
	info := infoOf(t, d, ack.SessionID)
	if info.SpawnedBy != "plugin:wf/workflow:r-1" {
		t.Errorf("SpawnedBy = %q, want plugin:wf/workflow:r-1", info.SpawnedBy)
	}
	if plugin, run := wire.ParsePrincipal(info.SpawnedBy); plugin != "wf" || run != "r-1" {
		t.Errorf("ParsePrincipal(%q) = (%q, %q)", info.SpawnedBy, plugin, run)
	}
	if info.Orchestrator {
		t.Error("a plugin's session arrived with the orchestrator grant (#524)")
	}
	waitReady(t, d, ack.SessionID)

	// Without a run id the label is the plain plugin principal.
	send(t, pc, wire.FrameCreateSession, acpSpec(`,"req_id":2`))
	ack2 := readFrameWhere(t, pc, "ACK", isFrame(wire.FrameAck)).ack(t)
	if got := infoOf(t, d, ack2.SessionID).SpawnedBy; got != "plugin:wf" {
		t.Errorf("SpawnedBy = %q, want plugin:wf", got)
	}
}

func TestPluginPrincipal_CannotClaimUserOrOtherPlugin(t *testing.T) {
	fakeACPDir(t)
	d := startTestDaemon(t)
	pc, _ := pluginConn(t, d, "wf", "p")
	before := len(d.reg.List())
	for _, run := range []string{"x/../plugin:q", "plugin:q", "a:b", "A", "-a", "a b", "a/workflow:b", ""} {
		if run == "" {
			continue
		}
		payload, _ := json.Marshal(map[string]any{"kind": "acp", "agent": "claude", "workflow_run_id": run, "req_id": 3})
		if err := wire.WriteFrame(pc, wire.FrameCreateSession, payload); err != nil {
			t.Fatal(err)
		}
		e := readFrameWhere(t, pc, "ERROR", isFrame(wire.FrameError)).err(t)
		if e.Code != wire.ErrCodeInvalidWorkflowRunID || e.ReqID != 3 {
			t.Errorf("run %q: error = %+v, want invalid_workflow_run_id", run, e)
		}
	}
	if n := len(d.reg.List()); n != before {
		t.Errorf("a refused run id created %d session(s)", n-before)
	}
}

func TestWorkflowRunID_RefusedFromUser(t *testing.T) {
	fakeACPDir(t)
	d := startTestDaemon(t)
	c := guiConn(t, d)
	before := len(d.reg.List())
	send(t, c, wire.FrameCreateSession, acpSpec(`,"workflow_run_id":"r1","req_id":4`))
	if e := readFrameWhere(t, c, "ERROR", isFrame(wire.FrameError)).err(t); e.Code != wire.ErrCodeModeNotAllowed || e.ReqID != 4 {
		t.Errorf("create error = %+v, want mode_not_allowed", e)
	}
	send(t, c, wire.FramePromptAcp, wire.PromptAcpReq{SessionID: "x", Text: "t", WorkflowRunID: "r1", ReqID: 5})
	if e := readFrameWhere(t, c, "ERROR", isFrame(wire.FrameError)).err(t); e.Code != wire.ErrCodeModeNotAllowed || e.ReqID != 5 {
		t.Errorf("prompt error = %+v, want mode_not_allowed (not no_such_session)", e)
	}
	if n := len(d.reg.List()); n != before {
		t.Errorf("a refused create made %d session(s)", n-before)
	}
}

func TestPromptAcp_OriginCarriesRun(t *testing.T) {
	fakeACPDir(t)
	d := startTestDaemon(t)
	pc, _ := pluginConn(t, d, "wf", "p")
	e := createACPVia(t, d, (&recordOps{}).ops(), `{"kind":"acp","agent":"claude"}`)

	originOf := func(text string) string {
		m, err := d.reg.AcpTranscript(e.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range m.Items {
			if it.Kind == wire.AcpItemUser && it.Text == text {
				return it.Origin
			}
		}
		t.Fatalf("no user item %q", text)
		return ""
	}
	send(t, pc, wire.FramePromptAcp, wire.PromptAcpReq{SessionID: e.ID, Text: "one", WorkflowRunID: "r9", ReqID: 1})
	readFrameWhere(t, pc, "ACK", isFrame(wire.FrameAck))
	if got := originOf("one"); got != "plugin:wf/workflow:r9" {
		t.Errorf("origin = %q, want plugin:wf/workflow:r9", got)
	}
	waitUntil(t, "turn end", func() bool { return infoOf(t, d, e.ID).State == wire.StateWaitingInput })
	send(t, pc, wire.FramePromptAcp, wire.PromptAcpReq{SessionID: e.ID, Text: "two", ReqID: 2})
	readFrameWhere(t, pc, "ACK", isFrame(wire.FrameAck))
	if got := originOf("two"); got != "plugin:wf" {
		t.Errorf("origin = %q, want plugin:wf", got)
	}
	// A bad run id is refused before the turn starts.
	waitUntil(t, "turn end", func() bool { return infoOf(t, d, e.ID).State == wire.StateWaitingInput })
	send(t, pc, wire.FramePromptAcp, wire.PromptAcpReq{SessionID: e.ID, Text: "three", WorkflowRunID: "Bad!", ReqID: 3})
	if er := readFrameWhere(t, pc, "ERROR", isFrame(wire.FrameError)).err(t); er.Code != wire.ErrCodeInvalidWorkflowRunID {
		t.Errorf("error = %+v", er)
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestCreateHello_WorkflowRunID(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	_, path := pluginConn(t, d, "wf", "p")
	hello := func(run string) (wire.Welcome, wire.FrameType, []byte, net.Conn) {
		c, err := net.Dial("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		send(t, c, wire.FrameHello, wire.Hello{Version: wire.PROTOCOL_VERSION, Client: "p", Mode: wire.ModeCreate,
			Create: &wire.CreateSpec{Shell: "/bin/sh", WorkflowRunID: run}})
		ft, p, err := wire.ReadFrame(c)
		if err != nil {
			t.Fatal(err)
		}
		var w wire.Welcome
		if ft == wire.FrameWelcome {
			_ = json.Unmarshal(p, &w)
		}
		return w, ft, p, c
	}
	w, ft, p, _ := hello("r2")
	if ft != wire.FrameWelcome || w.SessionID == "" {
		t.Fatalf("create HELLO: %s %s", ft, p)
	}
	if got := infoOf(t, d, w.SessionID).SpawnedBy; got != "plugin:wf/workflow:r2" {
		t.Errorf("SpawnedBy = %q, want plugin:wf/workflow:r2", got)
	}
	_, ft, p, _ = hello("Not Valid")
	var e wire.Error
	_ = json.Unmarshal(p, &e)
	if ft != wire.FrameError || e.Code != wire.ErrCodeInvalidWorkflowRunID {
		t.Errorf("bad run id HELLO: %s %s", ft, p)
	}

	// The user's own create HELLO takes no run id.
	c := dial(t, d)
	defer c.Close()
	send(t, c, wire.FrameHello, wire.Hello{Version: wire.PROTOCOL_VERSION, Client: "gui", Mode: wire.ModeCreate,
		Create: &wire.CreateSpec{Shell: "/bin/sh", WorkflowRunID: "r3"}})
	ft, p, err := wire.ReadFrame(c)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(p, &e)
	if ft != wire.FrameError || e.Code != wire.ErrCodeModeNotAllowed {
		t.Errorf("user create HELLO with a run id: %s %s", ft, p)
	}
}

func TestCreateWithResultSchema(t *testing.T) {
	fakeACPDir(t)
	d := startTestDaemon(t)
	c := guiConn(t, d)
	// Outside the IR subset: refused with its own code, no session.
	before := len(d.reg.List())
	send(t, c, wire.FrameCreateSession, acpSpec(`,"req_id":1,"result_schema":{"oneOf":[]}`))
	if e := readFrameWhere(t, c, "ERROR", isFrame(wire.FrameError)).err(t); e.Code != wire.ErrCodeInvalidResultSchema || e.ReqID != 1 {
		t.Errorf("error = %+v, want invalid_result_schema", e)
	}
	// On a terminal session: not_acp_session.
	send(t, c, wire.FrameCreateSession, json.RawMessage(`{"shell":"/bin/sh","req_id":2,"result_schema":{"type":"object","properties":{},"required":[],"additionalProperties":false}}`))
	if e := readFrameWhere(t, c, "ERROR", isFrame(wire.FrameError)).err(t); e.Code != wire.ErrCodeNotACP {
		t.Errorf("error = %+v, want not_acp_session", e)
	}
	if n := len(d.reg.List()); n != before {
		t.Errorf("refused creates made %d session(s)", n-before)
	}
	// A good one is accepted.
	send(t, c, wire.FrameCreateSession, acpSpec(`,"req_id":3,"result_schema":{"type":"object","properties":{"n":{"type":"number"}},"required":["n"],"additionalProperties":false}`))
	if a := readFrameWhere(t, c, "ACK", isFrame(wire.FrameAck)).ack(t); a.ReqID != 3 || a.SessionID == "" {
		t.Errorf("ack = %+v", a)
	}
}

func TestCancelAcp_PluginAndUser(t *testing.T) {
	fakeACPDir(t, acptest.FlagBlock)
	d := startTestDaemon(t)
	gui := guiConn(t, d)
	pc, _ := pluginConn(t, d, "wf", "p")
	for name, c := range map[string]net.Conn{"plugin": pc, "user": gui} {
		e := createACPVia(t, d, (&recordOps{}).ops(), `{"kind":"acp","agent":"claude"}`)
		waitReady(t, d, e.ID)
		send(t, c, wire.FramePromptAcp, wire.PromptAcpReq{SessionID: e.ID, Text: "work", ReqID: 1})
		readFrameWhere(t, c, "prompt ACK", isFrame(wire.FrameAck))

		send(t, c, wire.FrameCancelAcp, wire.CancelAcpReq{SessionID: e.ID, ReqID: 2})
		a := readFrameWhere(t, c, "cancel ACK", isFrame(wire.FrameAck)).ack(t)
		if a.ReqID != 2 || a.SessionID != e.ID || !a.WasBusy {
			t.Errorf("%s: cancel ack = %+v, want was_busy", name, a)
		}
		waitUntil(t, name+" stop reason", func() bool {
			m, _ := d.reg.AcpTranscript(e.ID)
			return m.StopReason == "cancelled"
		})
		waitUntil(t, name+" idle", func() bool { return infoOf(t, d, e.ID).State == wire.StateWaitingInput })

		// Idempotent: nothing running now, still an ACK, was_busy absent.
		send(t, c, wire.FrameCancelAcp, wire.CancelAcpReq{SessionID: e.ID, ReqID: 3})
		a = readFrameWhere(t, c, "second cancel ACK", isFrame(wire.FrameAck)).ack(t)
		if a.ReqID != 3 || a.WasBusy {
			t.Errorf("%s: idle cancel ack = %+v, want was_busy false", name, a)
		}
	}
	// Unknown session and terminal session are errors, not ACKs.
	send(t, gui, wire.FrameCancelAcp, wire.CancelAcpReq{SessionID: "nope", ReqID: 4})
	if e := readFrameWhere(t, gui, "ERROR", isFrame(wire.FrameError)).err(t); e.Code != "no_such_session" || e.ReqID != 4 {
		t.Errorf("error = %+v", e)
	}
}

func TestCancelAcp_RefusedOnSessionConn(t *testing.T) {
	d := newFrameTestDaemon(t)
	rec := &recordOps{}
	ops := rec.ops()
	ops.restricted, ops.ownSessionID = true, "s1"
	d.handleControlFrame(t.Context(), ops, wire.FrameCancelAcp, []byte(`{"session_id":"s1","req_id":1}`))
	d.handleControlFrame(t.Context(), ops, wire.FrameListAcpAgents, []byte(`{}`))
	if len(rec.errs) != 2 || rec.errs[0].Code != wire.ErrCodeModeNotAllowed || rec.errs[1].Code != wire.ErrCodeModeNotAllowed {
		t.Errorf("errors = %+v, want mode_not_allowed twice", rec.errs)
	}
}
