// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package daemon

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
)

// tokenFixture is a running daemon with two live sessions in one
// project: boss, granted the orchestrator capability, and worker, not.
type tokenFixture struct {
	d            *Daemon
	boss, worker *registry.Entry
}

func newTokenFixture(t *testing.T) *tokenFixture {
	t.Helper()
	d := startTestDaemon(t)
	mk := func(name string, grant bool) *registry.Entry {
		e, err := d.Registry().Create(context.Background(), wire.CreateSpec{
			Name: name, Agent: "claude", Cmd: []string{"/bin/cat"},
			Cols: 80, Rows: 24, Orchestrator: grant,
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		waitFor(t, 3*time.Second, func() bool { return sessionToken(d, e.ID) != "" })
		return e
	}
	return &tokenFixture{d: d, boss: mk("boss", true), worker: mk("worker", false)}
}

// helloOn dials sock and writes a HELLO in mode, claiming id with tok.
func helloOn(t *testing.T, sock string, mode wire.Mode, id, tok string) net.Conn {
	t.Helper()
	c, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial %s: %v", sock, err)
	}
	if err := wire.WriteJSON(c, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "test/0",
		Mode: mode, SessionID: id, SessionToken: tok,
	}); err != nil {
		t.Fatal(err)
	}
	return c
}

// expectAuthRefused reads the one ERROR a refused HELLO gets and checks
// the connection is then closed. It returns the message, so callers can
// check every refusal reads the same.
func expectAuthRefused(t *testing.T, c net.Conn) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	var e wire.Error
	ft, err := wire.ReadJSON(c, &e)
	if err != nil || ft != wire.FrameError || e.Code != wire.ErrCodeSessionAuthFailed {
		t.Fatalf("got %s %+v (err %v), want ERROR %s", ft, e, err, wire.ErrCodeSessionAuthFailed)
	}
	assertConnClosed(t, c)
	return e.Message
}

// Session and event modes, on both sockets, refuse any HELLO whose
// token is not the named session's own — and every refusal reads the
// same, so it does not say whether the id exists.
func TestSessionTokenRefusesUnverifiedHello(t *testing.T) {
	skipOnWindows(t)
	f := newTokenFixture(t)
	bossTok, workerTok := sessionToken(f.d, f.boss.ID), sessionToken(f.d, f.worker.ID)
	if bossTok == workerTok {
		t.Fatal("two sessions share a token")
	}
	cases := []struct{ name, id, tok string }{
		{"no token", f.boss.ID, ""},
		{"wrong token", f.boss.ID, flipLast(bossTok)},
		{"another session's token", f.boss.ID, workerTok},
		{"unknown id, no token", "no-such-session", ""},
		{"unknown id, a real token", "no-such-session", bossTok},
	}
	var msg string
	socks := map[string]string{"events": f.d.EventSocketPath(), "control": f.d.SocketPath()}
	for sockName, sock := range socks {
		modes := []wire.Mode{wire.ModeSession, wire.ModeEvent}
		if sockName == "events" {
			// plan_review is events-socket only. Review is off here, and
			// the refusal still comes first: an unverified caller never
			// learns whether review is on.
			modes = append(modes, wire.ModePlanReview)
		}
		for _, mode := range modes {
			for _, tc := range cases {
				t.Run(sockName+"/"+string(mode)+"/"+tc.name, func(t *testing.T) {
					c := helloOn(t, sock, mode, tc.id, tc.tok)
					defer c.Close()
					got := expectAuthRefused(t, c)
					if msg == "" {
						msg = got
					} else if got != msg {
						t.Errorf("refusal message %q differs from %q", got, msg)
					}
				})
			}
		}
	}
}

// flipLast returns tok with its last hex digit changed.
func flipLast(tok string) string {
	last := byte('0')
	if tok[len(tok)-1] == '0' {
		last = '1'
	}
	return tok[:len(tok)-1] + string(last)
}

// The right token is accepted: a session connection gets its WELCOME,
// and an event connection's report lands on its own session.
func TestSessionTokenAcceptsOwnToken(t *testing.T) {
	skipOnWindows(t)
	f := newTokenFixture(t)
	for _, sock := range []string{f.d.EventSocketPath(), f.d.SocketPath()} {
		c := helloOn(t, sock, wire.ModeSession, f.worker.ID, sessionToken(f.d, f.worker.ID))
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		var w wire.Welcome
		if ft, err := wire.ReadJSON(c, &w); err != nil || ft != wire.FrameWelcome {
			t.Fatalf("%s: handshake: %s %v, want WELCOME", sock, ft, err)
		}
		_ = c.Close()
	}
	c := helloOn(t, f.d.EventSocketPath(), wire.ModeEvent, f.worker.ID, sessionToken(f.d, f.worker.ID))
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameAgentEvent, wire.AgentEvent{
		SessionID: f.worker.ID, Kind: wire.AgentEventPrompt, Source: wire.StateSourceHook, Text: "mine",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool { return findSession(f.d, f.worker.ID).LastPrompt == "mine" })
}

// An event connection verified as worker cannot report state for boss:
// the event is refused, the connection dropped, boss untouched.
func TestSessionTokenRefusesForgedEventForOtherSession(t *testing.T) {
	skipOnWindows(t)
	f := newTokenFixture(t)
	before := findSession(f.d, f.boss.ID)
	c := helloOn(t, f.d.EventSocketPath(), wire.ModeEvent, f.worker.ID, sessionToken(f.d, f.worker.ID))
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameAgentEvent, wire.AgentEvent{
		SessionID: f.boss.ID, Kind: wire.AgentEventPrompt, Source: wire.StateSourceHook, Text: "forged",
	}); err != nil {
		t.Fatal(err)
	}
	assertConnClosed(t, c)
	if got := findSession(f.d, f.boss.ID); got.LastPrompt == "forged" || got.State != before.State {
		t.Fatalf("boss = %+v; a forged event was applied", got)
	}
}

// The impersonation the token exists to stop: worker, ungranted, claims
// boss's id to send as the orchestrator. Refused at HELLO. Boss itself,
// with its own token, still can.
func TestSessionTokenStopsOrchestratorImpersonation(t *testing.T) {
	skipOnWindows(t)
	f := newTokenFixture(t)

	forged := helloOn(t, f.d.EventSocketPath(), wire.ModeSession, f.boss.ID, sessionToken(f.d, f.worker.ID))
	defer forged.Close()
	expectAuthRefused(t, forged)

	real := helloOn(t, f.d.EventSocketPath(), wire.ModeSession, f.boss.ID, sessionToken(f.d, f.boss.ID))
	defer real.Close()
	if err := wire.WriteJSON(real, wire.FrameSendToSession, wire.SendToSessionReq{
		SessionID: f.worker.ID, Text: "hello", Queue: true,
	}); err != nil {
		t.Fatal(err)
	}
	_ = real.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		ft, payload, err := wire.ReadFrame(real)
		if err != nil {
			t.Fatalf("boss's own send: %v", err)
		}
		if ft == wire.FrameError {
			t.Fatalf("boss's own send refused: %s", payload)
		}
		if ft == wire.FrameSent {
			return
		}
	}
}

// A plan review may only be parked on the requester's own session:
// claiming another session's id in the HELLO is refused at the
// handshake, and a verified requester naming another session in the
// request gets invalid. Neither parks anything. The session's own
// request still parks.
func TestSessionTokenRefusesForgedPlanReview(t *testing.T) {
	skipOnWindows(t)
	planReviewEnv(t)
	f := newTokenFixture(t)
	enableReview(t, f.d, "")
	gui := reviewUI(t, f.d)
	defer gui.Close()

	claimed := requestPlanReviewAs(t, f.d, f.boss.ID, sessionToken(f.d, f.worker.ID), claudeReq(f.boss.ID))
	defer claimed.Close()
	expectAuthRefused(t, claimed)

	crossed := requestPlanReview(t, f.d, claudeReq(f.worker.ID))
	defer crossed.Close()
	crossed2 := requestPlanReviewAs(t, f.d, f.worker.ID, sessionToken(f.d, f.worker.ID), claudeReq(f.boss.ID))
	defer crossed2.Close()
	if dec := readDecision(t, crossed2, 2*time.Second); dec.Status != wire.PlanReviewInvalid {
		t.Fatalf("cross-session request: status %q, want invalid", dec.Status)
	}
	if p := pendingReview(f.d, f.boss.ID); p != nil {
		t.Fatalf("a forged review was parked on boss: %+v", p)
	}
	// The legitimate one (worker's own, sent first) is parked.
	waitPending(t, f.d, f.worker.ID)
}
