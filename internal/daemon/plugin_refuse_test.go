// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package daemon

import (
	"bytes"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/acp/acptest"
	"github.com/lucascaro/hive/internal/wire"
)

// readModeNotAllowed reads until an ERROR and requires mode_not_allowed.
func readModeNotAllowed(t *testing.T, c net.Conn, what string) wire.Error {
	t.Helper()
	var e wire.Error
	if err := json.Unmarshal(readControlFrame(t, c, wire.FrameError, 5*time.Second), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != wire.ErrCodeModeNotAllowed {
		t.Fatalf("%s from a plugin: code %q (%s), want %q", what, e.Code, e.Message, wire.ErrCodeModeNotAllowed)
	}
	return e
}

// A plugin's attach connection is read-only: its keystrokes and resizes
// never reach the PTY, while the user's attach still types.
func TestPluginAttach_InputRefused(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	sid := firstSessionID(t, d)
	_, path := pluginConn(t, d, "typist", "p")

	pa, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer pa.Close()
	handshake(t, pa, wire.Hello{Mode: wire.ModeAttach, SessionID: sid})
	if err := wire.WriteFrame(pa, wire.FrameData, []byte("echo plugin-$((6*7))\n")); err != nil {
		t.Fatal(err)
	}
	if err := wire.WriteJSON(pa, wire.FrameResize, wire.Resize{Cols: 20, Rows: 5}); err != nil {
		t.Fatal(err)
	}
	if e := readModeNotAllowed(t, pa, "DATA"); e.SessionID != sid {
		t.Errorf("refusal names session %q, want %q", e.SessionID, sid)
	}
	// Only the first refused frame is answered: a write loop must not
	// turn into an error flood.
	_ = pa.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	for {
		ft, _, err := wire.ReadFrame(pa)
		if err != nil {
			break
		}
		if ft == wire.FrameError {
			t.Fatal("a second refused frame got its own ERROR")
		}
	}

	ua := dial(t, d)
	defer ua.Close()
	handshake(t, ua, wire.Hello{Mode: wire.ModeAttach, SessionID: sid})
	if err := wire.WriteFrame(ua, wire.FrameData, []byte("echo user-$((7*7))\n")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "user-49") && time.Now().Before(deadline) {
		drainFor(ua, &out, 200*time.Millisecond)
	}
	if !strings.Contains(out.String(), "user-49") {
		t.Fatalf("the user's attach input produced no output; got %q", out.String())
	}
	if strings.Contains(out.String(), "plugin-42") || strings.Contains(out.String(), "plugin-$") {
		t.Fatalf("a plugin's attach input reached the PTY: %q", out.String())
	}
}

// RESIZE alone is refused too: a plugin must not reflow the terminal
// every viewer shares.
func TestPluginAttach_ResizeRefused(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	sid := firstSessionID(t, d)
	_, path := pluginConn(t, d, "resizer", "p")
	pa, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer pa.Close()
	handshake(t, pa, wire.Hello{Mode: wire.ModeAttach, SessionID: sid})
	if err := wire.WriteJSON(pa, wire.FrameResize, wire.Resize{Cols: 23, Rows: 7}); err != nil {
		t.Fatal(err)
	}
	readModeNotAllowed(t, pa, "RESIZE")

	ua := dial(t, d)
	defer ua.Close()
	handshake(t, ua, wire.Hello{Mode: wire.ModeAttach, SessionID: sid})
	if err := wire.WriteFrame(ua, wire.FrameData, []byte("stty size; echo size-$((2*21))\n")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "size-42") && time.Now().Before(deadline) {
		drainFor(ua, &out, 200*time.Millisecond)
	}
	if !strings.Contains(out.String(), "size-42") {
		t.Fatalf("no output from the user's attach; got %q", out.String())
	}
	if strings.Contains(out.String(), "7 23") {
		t.Fatalf("a plugin's RESIZE reached the PTY: %q", out.String())
	}
}

// Create mode ends in the same attach: a plugin's session is spawned,
// but typing into it is still refused.
func TestPluginCreateMode_InputRefused(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	path, closeFn, err := d.listenPlugin("spawner")
	if err != nil {
		t.Fatal(err)
	}
	defer closeFn()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	handshake(t, c, wire.Hello{Mode: wire.ModeCreate, Create: &wire.CreateSpec{}})
	if err := wire.WriteFrame(c, wire.FrameData, []byte("echo plugin-$((6*7))\n")); err != nil {
		t.Fatal(err)
	}
	readModeNotAllowed(t, c, "DATA on a create connection")
}

// ANSWER_PERMISSION from a plugin leaves the request pending; the
// user's connection still answers it.
func TestAnswerPermission_RefusedForPlugin(t *testing.T) {
	fakeACPDir(t, acptest.FlagPermission)
	d := startTestDaemon(t)
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

	pc, _ := pluginConn(t, d, "approver", "p")
	send(t, pc, wire.FrameAnswerPermission, wire.AnswerPermissionReq{SessionID: e.ID, RequestID: perm.RequestID, OptionID: "allow"})
	readModeNotAllowed(t, pc, "ANSWER_PERMISSION")
	if m, _ := d.reg.AcpTranscript(e.ID); m.Permission == nil || m.Permission.RequestID != perm.RequestID {
		t.Fatalf("a refused answer changed the pending permission: %+v", m.Permission)
	}

	c := guiConn(t, d)
	send(t, c, wire.FrameAnswerPermission, wire.AnswerPermissionReq{SessionID: e.ID, RequestID: perm.RequestID, OptionID: "allow", ReqID: 7})
	if a := readFrameWhere(t, c, "ACK", isFrame(wire.FrameAck)).ack(t); a.ReqID != 7 || a.SessionID != e.ID {
		t.Errorf("user's answer ack = %+v", a)
	}
}

// RESOLVE_PLAN_REVIEW from a plugin leaves the review pending; the
// app's connection still decides it.
func TestResolvePlanReview_RefusedForPlugin(t *testing.T) {
	skipOnWindows(t)
	planReviewEnv(t)
	d := startTestDaemon(t)
	enableReview(t, d, "")
	id := bootstrapSessionID(t, d)
	gui := reviewUI(t, d)
	defer gui.Close()
	c := requestPlanReview(t, d, claudeReq(id))
	defer c.Close()
	p := waitPending(t, d, id)

	pc, _ := pluginConn(t, d, "approver", "p")
	send(t, pc, wire.FrameResolvePlanReview, wire.ResolvePlanReviewReq{SessionID: id, ReviewID: p.ReviewID, Decision: wire.PlanReviewApprove})
	readModeNotAllowed(t, pc, "RESOLVE_PLAN_REVIEW")
	if got := pendingReview(d, id); got == nil || got.ReviewID != p.ReviewID {
		t.Fatalf("a refused resolve changed the pending review: %+v", got)
	}

	send(t, gui, wire.FrameResolvePlanReview, wire.ResolvePlanReviewReq{SessionID: id, ReviewID: p.ReviewID, Decision: wire.PlanReviewApprove})
	if dec := readDecision(t, c, 2*time.Second); dec.Status != wire.PlanReviewApprove {
		t.Fatalf("user's decision = %+v, want approve", dec)
	}
}

// Installing, enabling and removing plugins are refused to a plugin and
// leave the plugin set unchanged; the user's connection still does all
// three. LIST_PLUGINS stays available.
func TestPluginManagement_RefusedForPlugin(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	gui := pluginTestControl(t, d, "hivegui/a")
	send(t, gui, wire.FrameInstallPlugin, wire.InstallPluginReq{Source: writeSleepPlugin(t, "sleepy")})
	awaitPluginEvent(t, gui, func(ev wire.PluginEvent) bool { return ev.Kind == wire.PluginEventAdded })
	before := d.plugins.List()

	pc, _ := pluginConn(t, d, "rogue", "p")
	send(t, pc, wire.FrameListPlugins, struct{}{})
	readControlFrame(t, pc, wire.FramePlugins, 5*time.Second)
	for _, tc := range []struct {
		ft wire.FrameType
		v  any
	}{
		{wire.FrameInstallPlugin, wire.InstallPluginReq{Source: writeSleepPlugin(t, "other")}},
		{wire.FrameSetPluginEnabled, wire.SetPluginEnabledReq{ID: "sleepy", Enabled: true}},
		{wire.FrameRemovePlugin, wire.RemovePluginReq{ID: "sleepy"}},
	} {
		send(t, pc, tc.ft, tc.v)
		readModeNotAllowed(t, pc, tc.ft.String())
	}
	d.ops.Wait()
	after := d.plugins.List()
	if len(after) != len(before) {
		t.Fatalf("plugins changed: before %+v, after %+v", before, after)
	}
	for i := range after {
		if after[i].ID != before[i].ID || after[i].Enabled != before[i].Enabled {
			t.Fatalf("plugin %s changed: before %+v, after %+v", after[i].ID, before[i], after[i])
		}
	}

	send(t, gui, wire.FrameSetPluginEnabled, wire.SetPluginEnabledReq{ID: "sleepy", Enabled: true})
	awaitPluginEvent(t, gui, func(ev wire.PluginEvent) bool { return ev.Plugin.ID == "sleepy" && ev.Plugin.Enabled })
	send(t, gui, wire.FrameRemovePlugin, wire.RemovePluginReq{ID: "sleepy"})
	awaitPluginEvent(t, gui, func(ev wire.PluginEvent) bool { return ev.Kind == wire.PluginEventRemoved })
}
