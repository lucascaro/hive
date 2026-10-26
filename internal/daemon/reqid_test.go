// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package daemon

import (
	"encoding/json"
	"net"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/acp"
	"github.com/lucascaro/hive/internal/acp/acptest"
	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
)

// Request/reply correlation (spec 528): any control request may carry a
// req_id; ERRORs echo it and requests with no reply frame are ACKed.

// fakeACPDir is useFakeACP that also returns the fake's store directory,
// where a test releases a blocked adapter.
func fakeACPDir(t *testing.T, flags ...string) string {
	t.Helper()
	skipOnWindows(t)
	dir := t.TempDir()
	t.Cleanup(registry.SetACPCommandForTest(func(agent.Def) ([]string, []string) {
		return []string{os.Args[0]}, append(acp.AdapterEnv(os.Environ(), ""), acptest.Env(dir, flags...)...)
	}))
	return dir
}

// frame is one frame read off a connection.
type frame struct {
	ft      wire.FrameType
	payload []byte
}

func (f frame) ack(t *testing.T) wire.AckMsg {
	t.Helper()
	var a wire.AckMsg
	if err := json.Unmarshal(f.payload, &a); err != nil {
		t.Fatal(err)
	}
	return a
}

func (f frame) err(t *testing.T) wire.Error {
	t.Helper()
	var e wire.Error
	if err := json.Unmarshal(f.payload, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

// readFrameWhere reads frames until pred holds, failing on timeout.
func readFrameWhere(t *testing.T, c net.Conn, what string, pred func(frame) bool) frame {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		_ = c.SetReadDeadline(deadline)
		ft, p, err := wire.ReadFrame(c)
		if err != nil {
			t.Fatalf("waiting for %s: %v", what, err)
		}
		if f := (frame{ft, p}); pred(f) {
			return f
		}
	}
}

func isFrame(ft wire.FrameType) func(frame) bool { return func(f frame) bool { return f.ft == ft } }

func send(t *testing.T, c net.Conn, ft wire.FrameType, v any) {
	t.Helper()
	if err := wire.WriteJSON(c, ft, v); err != nil {
		t.Fatal(err)
	}
}

func phaseOf(d *Daemon, id string) (phase string, found bool) {
	for _, s := range d.reg.List() {
		if s.ID == id {
			return s.Phase, true
		}
	}
	return "", false
}

func waitReady(t *testing.T, d *Daemon, id string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if p, ok := phaseOf(d, id); ok && p == wire.PhaseReady {
			if e := d.reg.Get(id); e != nil {
				if m, err := d.reg.AcpTranscript(id); err == nil && m.Epoch >= 0 {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("session %s never became ready", id)
}

func guiConn(t *testing.T, d *Daemon) net.Conn { return pluginTestControl(t, d, "test-gui") }

func acpSpec(extra string) json.RawMessage {
	return json.RawMessage(`{"kind":"acp","agent":"claude"` + extra + `}`)
}

func TestReqID_EchoedOnError(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := guiConn(t, d)

	send(t, c, wire.FramePromptAcp, wire.PromptAcpReq{SessionID: "nope", Text: "x", ReqID: 7})
	e := readFrameWhere(t, c, "ERROR", isFrame(wire.FrameError)).err(t)
	if e.Code != "no_such_session" || e.ReqID != 7 {
		t.Errorf("error = %+v, want no_such_session echoing req_id 7", e)
	}

	// A payload that does not decode still echoes the id it carried.
	if err := wire.WriteFrame(c, wire.FrameKillSession, []byte(`{"req_id":3,"session_id":5}`)); err != nil {
		t.Fatal(err)
	}
	e = readFrameWhere(t, c, "ERROR", isFrame(wire.FrameError)).err(t)
	if e.Code != "bad_payload" || e.ReqID != 3 {
		t.Errorf("error = %+v, want bad_payload echoing req_id 3", e)
	}

	// No req_id, no echo.
	send(t, c, wire.FramePromptAcp, wire.PromptAcpReq{SessionID: "nope", Text: "x"})
	if e := readFrameWhere(t, c, "ERROR", isFrame(wire.FrameError)).err(t); e.ReqID != 0 {
		t.Errorf("error = %+v, want no req_id", e)
	}
}

func TestReqID_AsyncCreateErrorEchoed(t *testing.T) {
	fakeACPDir(t)
	d := startTestDaemon(t)
	c := guiConn(t, d)
	// The refusal is raised from the goroutine the create runs on.
	send(t, c, wire.FrameCreateSession, acpSpec(`,"acp_mode":"bypassPermissions","req_id":9`))
	f := readFrameWhere(t, c, "ERROR", func(f frame) bool { return f.ft == wire.FrameError || f.ft == wire.FrameAck })
	if f.ft != wire.FrameError {
		t.Fatalf("got %s, want ERROR", f.ft)
	}
	if e := f.err(t); e.Code != wire.ErrCodeACPModeAboveCeiling || e.ReqID != 9 {
		t.Errorf("error = %+v, want acp_mode_above_ceiling echoing req_id 9", e)
	}
}

func TestReqID_CreateAckedBeforeAdapterStarts(t *testing.T) {
	dir := fakeACPDir(t, acptest.FlagSlowInit)
	d := startTestDaemon(t)
	c := guiConn(t, d)
	send(t, c, wire.FrameCreateSession, acpSpec(`,"req_id":11`))
	ack := readFrameWhere(t, c, "ACK", isFrame(wire.FrameAck)).ack(t)
	if ack.ReqID != 11 || ack.SessionID == "" {
		t.Fatalf("ack = %+v", ack)
	}
	// The adapter is still in initialize: the session exists but is not
	// ready, so this ACK did not wait for the create.
	if p, ok := phaseOf(d, ack.SessionID); !ok || p == wire.PhaseReady {
		t.Errorf("at ACK: phase %q found %v, want a session that is still starting", p, ok)
	}
	if err := acptest.Release(dir); err != nil {
		t.Fatal(err)
	}
	waitReady(t, d, ack.SessionID)
}

func TestReqID_CreateAckMatchesAddedEvent(t *testing.T) {
	fakeACPDir(t)
	d := startTestDaemon(t)
	c := guiConn(t, d)
	send(t, c, wire.FrameCreateSession, acpSpec(`,"req_id":12`))
	// The ACK and the broadcast race; take them in either order.
	var ackID, addedID string
	for ackID == "" || addedID == "" {
		f := readFrameWhere(t, c, "ACK or added", func(f frame) bool {
			return f.ft == wire.FrameAck || f.ft == wire.FrameSessionEvent
		})
		if f.ft == wire.FrameAck {
			ackID = f.ack(t).SessionID
			continue
		}
		var ev wire.SessionEvent
		_ = json.Unmarshal(f.payload, &ev)
		if ev.Kind == wire.SessionEventAdded && ev.Session.Kind == wire.KindACP {
			addedID = ev.Session.ID
		}
	}
	if ackID != addedID {
		t.Errorf("ACK names %q, SESSION_EVENT(added) names %q", ackID, addedID)
	}
}

func TestReqID_KilledMidCreateErrors(t *testing.T) {
	dir := fakeACPDir(t, acptest.FlagSlowInit)
	d := startTestDaemon(t)
	c := guiConn(t, d)
	send(t, c, wire.FrameCreateSession, acpSpec(`,"req_id":13`))
	id := readFrameWhere(t, c, "ACK", isFrame(wire.FrameAck)).ack(t).SessionID
	if err := d.reg.Kill(id, true); err != nil {
		t.Fatal(err)
	}
	_ = acptest.Release(dir)
	// Never silence: the requester got an ACK, so it hears how it ended.
	e := readFrameWhere(t, c, "ERROR", isFrame(wire.FrameError)).err(t)
	if e.ReqID != 13 || e.SessionID != id {
		t.Errorf("error = %+v, want req_id 13 for session %s", e, id)
	}
}

func TestReqID_AbsentMeansNoAck(t *testing.T) {
	fakeACPDir(t)
	d := startTestDaemon(t)
	c := guiConn(t, d)
	send(t, c, wire.FrameCreateSession, acpSpec(``))
	added := readFrameWhere(t, c, "added", func(f frame) bool {
		var ev wire.SessionEvent
		return f.ft == wire.FrameSessionEvent && json.Unmarshal(f.payload, &ev) == nil && ev.Kind == wire.SessionEventAdded && ev.Session.Kind == wire.KindACP
	})
	var ev wire.SessionEvent
	_ = json.Unmarshal(added.payload, &ev)
	waitReady(t, d, ev.Session.ID)
	send(t, c, wire.FrameCancelAcp, wire.CancelAcpReq{SessionID: ev.Session.ID})
	send(t, c, wire.FrameListSessions, struct{}{})
	// Every frame up to the LIST_SESSIONS answer: none is an ACK.
	readFrameWhere(t, c, "SESSIONS", func(f frame) bool {
		if f.ft == wire.FrameAck {
			t.Errorf("unexpected ACK: %s", f.payload)
		}
		return f.ft == wire.FrameSessions
	})
}

func TestReqID_PromptAckCarriesPromptID(t *testing.T) {
	fakeACPDir(t)
	d := startTestDaemon(t)
	c := guiConn(t, d)
	e := createACPVia(t, d, (&recordOps{}).ops(), `{"kind":"acp","agent":"claude"}`)
	send(t, c, wire.FramePromptAcp, wire.PromptAcpReq{SessionID: e.ID, Text: "hi", ReqID: 20})
	ack := readFrameWhere(t, c, "ACK", isFrame(wire.FrameAck)).ack(t)
	msg, err := d.reg.AcpTranscript(e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ack.ReqID != 20 || ack.SessionID != e.ID || ack.PromptID == 0 || ack.PromptID != msg.PromptID || ack.Epoch != msg.Epoch {
		t.Errorf("ack = %+v, transcript says prompt %d epoch %d", ack, msg.PromptID, msg.Epoch)
	}
}

func TestReqID_RestartAcked(t *testing.T) {
	fakeACPDir(t)
	d := startTestDaemon(t)
	c := guiConn(t, d)
	e := createACPVia(t, d, (&recordOps{}).ops(), `{"kind":"acp","agent":"claude"}`)
	send(t, c, wire.FrameRestartSession, wire.RestartSessionReq{SessionID: e.ID, ReqID: 31})
	ack := readFrameWhere(t, c, "ACK", isFrame(wire.FrameAck)).ack(t)
	if ack.ReqID != 31 || ack.SessionID != e.ID {
		t.Errorf("ack = %+v", ack)
	}
	// A failed restart is restart_failed carrying the id.
	send(t, c, wire.FrameRestartSession, wire.RestartSessionReq{SessionID: "nope", ReqID: 32})
	if er := readFrameWhere(t, c, "ERROR", isFrame(wire.FrameError)).err(t); er.Code != "restart_failed" || er.ReqID != 32 {
		t.Errorf("error = %+v, want restart_failed echoing 32", er)
	}
}

func TestReqID_KillAndAnswerPermissionAcked(t *testing.T) {
	fakeACPDir(t, acptest.FlagPermission)
	d := startTestDaemon(t)
	c := guiConn(t, d)
	e := createACPVia(t, d, (&recordOps{}).ops(), `{"kind":"acp","agent":"claude"}`)
	if err := d.reg.PromptACP(e.ID, "go", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	var perm *wire.AcpPermission
	for deadline := time.Now().Add(10 * time.Second); perm == nil && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		m, _ := d.reg.AcpTranscript(e.ID)
		perm = m.Permission
	}
	if perm == nil {
		t.Fatal("no permission request")
	}
	send(t, c, wire.FrameAnswerPermission, wire.AnswerPermissionReq{SessionID: e.ID, RequestID: perm.RequestID, OptionID: "allow", ReqID: 40})
	if a := readFrameWhere(t, c, "ACK", isFrame(wire.FrameAck)).ack(t); a.ReqID != 40 || a.SessionID != e.ID {
		t.Errorf("answer ack = %+v", a)
	}
	// A stale answer is an ERROR carrying the id, not an ACK.
	send(t, c, wire.FrameAnswerPermission, wire.AnswerPermissionReq{SessionID: e.ID, RequestID: perm.RequestID, OptionID: "allow", ReqID: 41})
	if er := readFrameWhere(t, c, "ERROR", isFrame(wire.FrameError)).err(t); er.Code != "permission_stale" || er.ReqID != 41 {
		t.Errorf("stale answer error = %+v", er)
	}
	send(t, c, wire.FrameKillSession, wire.KillSessionReq{SessionID: e.ID, Force: true, ReqID: 42})
	if a := readFrameWhere(t, c, "ACK", isFrame(wire.FrameAck)).ack(t); a.ReqID != 42 || a.SessionID != e.ID {
		t.Errorf("kill ack = %+v", a)
	}
}

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestWelcome_BootIDStablePerProcess(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	w1 := handshake(t, dial(t, d), wire.Hello{Mode: wire.ModeControl})
	w2 := handshake(t, dial(t, d), wire.Hello{Mode: wire.ModeControl})
	if !hex32.MatchString(w1.BootID) || w1.BootID != w2.BootID {
		t.Errorf("boot ids %q, %q: want the same 16 random bytes in hex", w1.BootID, w2.BootID)
	}
	// An attach WELCOME carries it too.
	w3 := handshake(t, dial(t, d), wire.Hello{Mode: wire.ModeAttach, SessionID: firstSessionID(t, d)})
	if w3.BootID != w1.BootID {
		t.Errorf("attach boot id %q, want %q", w3.BootID, w1.BootID)
	}
}

func TestListAcpAgents(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := guiConn(t, d)
	send(t, c, wire.FrameListAcpAgents, wire.ListAcpAgentsReq{ReqID: 50})
	var msg wire.AcpAgentsMsg
	if err := json.Unmarshal(readFrameWhere(t, c, "ACP_AGENTS", isFrame(wire.FrameAcpAgents)).payload, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.ReqID != 50 {
		t.Errorf("req_id = %d, want 50", msg.ReqID)
	}
	byID := map[string]wire.AcpAgentInfo{}
	for _, a := range msg.Agents {
		byID[a.ID] = a
	}
	claude, ok := byID["claude"]
	if !ok || len(claude.Modes) == 0 || claude.Ceiling == "" || claude.DefaultMode == "" {
		t.Fatalf("claude = %+v (present %v), want modes, a default and a ceiling", claude, ok)
	}
	if !claude.Available && claude.Reason == "" {
		t.Errorf("an unavailable agent must say why: %+v", claude)
	}
	if pi, ok := byID["pi"]; !ok || !pi.NoApprovalGate {
		t.Errorf("pi = %+v (present %v), want no_approval_gate", pi, ok)
	}
	if _, shell := byID["aider"]; shell {
		t.Error("aider cannot run as ACP and must not be listed")
	}
	for _, a := range msg.Agents {
		for _, m := range a.Modes {
			if m.ID == "" {
				t.Errorf("%s has a mode with no id", a.ID)
			}
		}
	}
}
