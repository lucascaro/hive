// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package daemon

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// PLUGIN_MESSAGE (spec 528): plugin → every UI-capable client, UI → the
// named plugin, nothing else.

func readPluginMessage(t *testing.T, c net.Conn) wire.PluginMessage {
	t.Helper()
	var m wire.PluginMessage
	if err := json.Unmarshal(readFrameWhere(t, c, "PLUGIN_MESSAGE", isFrame(wire.FramePluginMessage)).payload, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// barrier sends a request every connection kind answers with an ERROR
// echoing its req_id, and reads up to that answer. A PLUGIN_MESSAGE that
// was relayed to c before the barrier was sent has arrived by then, so
// seeing one fails the test.
func assertNoPluginMessage(t *testing.T, c net.Conn, why string) {
	t.Helper()
	const id = 777
	send(t, c, wire.FramePromptAcp, wire.PromptAcpReq{SessionID: "nope", Text: "x", ReqID: id})
	readFrameWhere(t, c, "barrier", func(f frame) bool {
		if f.ft == wire.FramePluginMessage {
			t.Errorf("%s: got PLUGIN_MESSAGE %s", why, f.payload)
		}
		return f.ft == wire.FrameError
	})
	// The ERROR is written by the read loop and a relayed message by the
	// fan-out goroutine, so the ERROR alone proves nothing about the
	// message: keep listening a little after it.
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		_ = c.SetReadDeadline(deadline)
		ft, p, err := wire.ReadFrame(c)
		if err != nil {
			break
		}
		if ft == wire.FramePluginMessage {
			t.Errorf("%s: got PLUGIN_MESSAGE %s", why, p)
		}
	}
	_ = c.SetReadDeadline(time.Time{})
}

func TestPluginMessage_PluginToGUIStampsID(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	gui := guiConn(t, d)
	other := pluginTestControl(t, d, "hived-ws-bridge/test")
	pc, _ := pluginConn(t, d, "wf", "claims-to-be-hivegui")

	// plugin_id in the frame is the sender's claim and carries no weight.
	send(t, pc, wire.FramePluginMessage, wire.PluginMessage{PluginID: "someone-else", Topic: "run.state", Data: json.RawMessage(`{"n":1}`)})
	for name, c := range map[string]net.Conn{"gui": gui, "bridge": other} {
		m := readPluginMessage(t, c)
		if m.PluginID != "wf" || m.Topic != "run.state" || string(m.Data) != `{"n":1}` {
			t.Errorf("%s got %+v, want plugin_id wf stamped by the daemon", name, m)
		}
	}
}

func TestPluginMessage_GUIToPlugin(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	gui := guiConn(t, d)
	pc, _ := pluginConn(t, d, "wf", "p")
	pc2, _ := pluginConn(t, d, "wf", "p") // a second connection of the same plugin

	send(t, gui, wire.FramePluginMessage, wire.PluginMessage{PluginID: "wf", Topic: "ui.cancel", Data: json.RawMessage(`"nonce-1"`)})
	for _, c := range []net.Conn{pc, pc2} {
		m := readPluginMessage(t, c)
		if m.PluginID != "wf" || m.Topic != "ui.cancel" || string(m.Data) != `"nonce-1"` {
			t.Errorf("plugin got %+v", m)
		}
	}
	// The sending GUI does not hear its own message back.
	assertNoPluginMessage(t, gui, "sender")
}

func TestPluginMessage_TargetNotRunning(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	gui := guiConn(t, d)
	send(t, gui, wire.FramePluginMessage, wire.PluginMessage{PluginID: "ghost", Topic: "t"})
	e := readFrameWhere(t, gui, "ERROR", isFrame(wire.FrameError)).err(t)
	if e.Code != wire.ErrCodePluginNotRunning {
		t.Errorf("error = %+v, want plugin_not_running", e)
	}
}

func TestPluginMessage_NotRelayedToOtherPlugins(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	gui := guiConn(t, d)
	a, _ := pluginConn(t, d, "a", "p")
	b, _ := pluginConn(t, d, "b", "p")

	send(t, a, wire.FramePluginMessage, wire.PluginMessage{Topic: "t"})
	readPluginMessage(t, gui) // relayed to the GUI, so it has gone everywhere it will
	assertNoPluginMessage(t, b, "other plugin")
	assertNoPluginMessage(t, a, "sender")

	// A GUI message to a is not seen by b either.
	send(t, gui, wire.FramePluginMessage, wire.PluginMessage{PluginID: "a", Topic: "t"})
	readPluginMessage(t, a)
	assertNoPluginMessage(t, b, "other plugin")
}

func TestPluginMessage_NotRelayedToHivebarOrSession(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	gui := guiConn(t, d)
	bar := pluginTestControl(t, d, "hivebar/1")
	sess := sessionConn(t, d, bootstrapSessionID(t, d))
	pc, _ := pluginConn(t, d, "wf", "p")

	send(t, pc, wire.FramePluginMessage, wire.PluginMessage{Topic: "t"})
	readPluginMessage(t, gui)
	assertNoPluginMessage(t, bar, "hivebar")
	assertNoPluginMessage(t, sess, "session connection")

	// And neither can address a plugin.
	send(t, bar, wire.FramePluginMessage, wire.PluginMessage{PluginID: "wf", Topic: "t"})
	if e := readFrameWhere(t, bar, "ERROR", isFrame(wire.FrameError)).err(t); e.Code != wire.ErrCodeModeNotAllowed {
		t.Errorf("hivebar: error = %+v, want mode_not_allowed", e)
	}
	assertNoPluginMessage(t, pc, "plugin, from hivebar")
}

func TestPluginMessage_RefusedOnSessionConn(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	pc, _ := pluginConn(t, d, "wf", "p")
	sess := sessionConn(t, d, bootstrapSessionID(t, d))
	send(t, sess, wire.FramePluginMessage, wire.PluginMessage{PluginID: "wf", Topic: "t"})
	if e := readFrameWhere(t, sess, "ERROR", isFrame(wire.FrameError)).err(t); e.Code != wire.ErrCodeModeNotAllowed {
		t.Errorf("error = %+v, want mode_not_allowed", e)
	}
	assertNoPluginMessage(t, pc, "plugin, from a session connection")
}

func TestPluginMessage_TopicAndShape(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	gui := guiConn(t, d)
	pc, _ := pluginConn(t, d, "wf", "p")
	for _, topic := range []string{"", "Upper", "1abc", "has space", "a/b", strings.Repeat("a", 65), "é"} {
		send(t, pc, wire.FramePluginMessage, wire.PluginMessage{Topic: topic})
		if e := readFrameWhere(t, pc, "ERROR", isFrame(wire.FrameError)).err(t); e.Code != wire.ErrCodeInvalidPluginMessage {
			t.Errorf("topic %q: error = %+v, want invalid_plugin_message", topic, e)
		}
	}
	if err := wire.WriteFrame(pc, wire.FramePluginMessage, []byte(`{"topic":5}`)); err != nil {
		t.Fatal(err)
	}
	if e := readFrameWhere(t, pc, "ERROR", isFrame(wire.FrameError)).err(t); e.Code != "bad_payload" {
		t.Errorf("error = %+v, want bad_payload", e)
	}
	assertNoPluginMessage(t, gui, "gui, after refused messages")
}

func TestPluginMessage_SizeCap64KiB(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	gui := guiConn(t, d)
	pc, _ := pluginConn(t, d, "wf", "p")

	// pad data so the whole payload is exactly n bytes
	payloadOf := func(n int) []byte {
		base := len(`{"plugin_id":"","topic":"t","data":""}`)
		b, _ := json.Marshal(wire.PluginMessage{Topic: "t", Data: json.RawMessage(`"` + strings.Repeat("x", n-base) + `"`)})
		if len(b) != n {
			t.Fatalf("built %d bytes, want %d", len(b), n)
		}
		return b
	}
	// The cap is on the frame as relayed, with the stamped plugin id.
	if err := wire.WriteFrame(pc, wire.FramePluginMessage, payloadOf(wire.MaxPluginMessage-len(`wf`))); err != nil {
		t.Fatal(err)
	}
	if m := readPluginMessage(t, gui); m.PluginID != "wf" || len(m.Data) < wire.MaxPluginMessage-100 {
		t.Errorf("a payload at the cap was not relayed intact: plugin %q, %d bytes of data", m.PluginID, len(m.Data))
	}
	if err := wire.WriteFrame(pc, wire.FramePluginMessage, payloadOf(wire.MaxPluginMessage-len(`wf`)+1)); err != nil {
		t.Fatal(err)
	}
	if e := readFrameWhere(t, pc, "ERROR", isFrame(wire.FrameError)).err(t); e.Code != wire.ErrCodePluginMessageTooLarge {
		t.Errorf("error = %+v, want plugin_message_too_large", e)
	}
	// Escaping grows a payload: 20000 "<" is 20000 bytes in, 120000 out.
	big := json.RawMessage(`"` + strings.Repeat("<", 20000) + `"`)
	send(t, pc, wire.FramePluginMessage, wire.PluginMessage{Topic: "t", Data: big})
	if e := readFrameWhere(t, pc, "ERROR", isFrame(wire.FrameError)).err(t); e.Code != wire.ErrCodePluginMessageTooLarge {
		t.Errorf("escaped growth: error = %+v, want plugin_message_too_large", e)
	}
	assertNoPluginMessage(t, gui, "gui, after an oversize message")
}

// A GUI that stops reading must not stall the plugin that talks to it;
// it is hung up on like any slow client (slow-client-policy.md), and a
// reconnected GUI works again.
func TestPluginMessage_SlowGUIDoesNotStallPlugin(t *testing.T) {
	skipOnWindows(t)
	setLimits(t, 300*time.Millisecond, 1<<20)
	old := backpressureMax
	backpressureMax = 20 * time.Millisecond
	t.Cleanup(func() { backpressureMax = old })
	d := startTestDaemon(t)

	stalled := dial(t, d)
	t.Cleanup(func() { _ = stalled.Close() })
	handshake(t, stalled, wire.Hello{Mode: wire.ModeControl, Client: "hivegui/stalled"})
	pc, _ := pluginConn(t, d, "wf", "p")

	// ~6 MB at the cap: far past both socket buffers and the queue.
	data := json.RawMessage(`"` + strings.Repeat("x", wire.MaxPluginMessage-200) + `"`)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			if err := wire.WriteJSON(pc, wire.FramePluginMessage, wire.PluginMessage{Topic: "flood", Data: data}); err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the plugin's sends stalled behind a GUI that stopped reading")
	}
	// The plugin is still served.
	send(t, pc, wire.FramePromptAcp, wire.PromptAcpReq{SessionID: "nope", Text: "x", ReqID: 5})
	readFrameWhere(t, pc, "ERROR", isFrame(wire.FrameError))

	awaitHangUp(t, stalled, 10*time.Second)
}

func TestPluginMessage_GUIRecoversAfterSlowClientDisconnect(t *testing.T) {
	skipOnWindows(t)
	setLimits(t, 300*time.Millisecond, 1<<20)
	old := backpressureMax
	backpressureMax = 20 * time.Millisecond
	t.Cleanup(func() { backpressureMax = old })
	d := startTestDaemon(t)

	stalled := dial(t, d)
	t.Cleanup(func() { _ = stalled.Close() })
	handshake(t, stalled, wire.Hello{Mode: wire.ModeControl, Client: "hivegui/stalled"})
	pc, _ := pluginConn(t, d, "wf", "p")

	data := json.RawMessage(`"` + strings.Repeat("x", wire.MaxPluginMessage-200) + `"`)
	for range 100 {
		send(t, pc, wire.FramePluginMessage, wire.PluginMessage{Topic: "flood", Data: data})
	}
	awaitHangUp(t, stalled, 10*time.Second)

	// The reconnected GUI re-asks the plugin and gets the answer.
	gui := guiConn(t, d)
	send(t, gui, wire.FramePluginMessage, wire.PluginMessage{PluginID: "wf", Topic: "replay"})
	deadline := time.Now().Add(10 * time.Second)
	for {
		m := readPluginMessage(t, pc)
		if m.Topic == "replay" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the plugin never received the reconnected GUI's message")
		}
	}
	send(t, pc, wire.FramePluginMessage, wire.PluginMessage{Topic: "state", Data: json.RawMessage(`{"ok":true}`)})
	// Part of the flood may still be in flight; the answer follows it.
	for {
		m := readPluginMessage(t, gui)
		if m.Topic == "state" {
			if m.PluginID != "wf" || string(m.Data) != `{"ok":true}` {
				t.Errorf("reconnected GUI got %+v", m)
			}
			break
		}
	}
}
