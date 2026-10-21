// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/acp/acptest"
	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/agentstate"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

func TestSanitizeInput(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
		err            bool
	}{
		{"plain", "rebase onto main", "rebase onto main", false},
		{"keeps indentation", "  a\n    b", "a\n    b", false},
		{"crlf folds", "a\r\nb\rc", "a\nb\nc", false},
		{"tab kept", "a\tb", "a\tb", false},
		{"escape stripped", "hi\x1b[31m there", "hi[31m there", false},
		{"bracketed paste end stripped of ESC", "x\x1b[201~y", "x[201~y", false},
		{"c0 and del stripped", "a\x00\x07\x7fb", "ab", false},
		{"c1 stripped", "a\u0085b", "ab", false},
		{"empty", "", "", true},
		{"only controls and space", "\x1b\x07  ", "", true},
		{"invalid utf8", "a\xffb", "", true},
		{"at the cap", strings.Repeat("a", wire.MaxMessageLen), strings.Repeat("a", wire.MaxMessageLen), false},
		{"over the cap", strings.Repeat("a", wire.MaxMessageLen+1), "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sanitizeInput(tc.in)
			if (err != nil) != tc.err || (err != nil && !errors.Is(err, ErrMessageInvalid)) {
				t.Fatalf("err = %v, want error=%v", err, tc.err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// goIdle drives a live session to idle on the manual clock: paint, see
// it work, let the quiet timer expire.
func goIdle(t *testing.T, r *Registry, e *Entry, sess *session.Session, now time.Time) time.Time {
	t.Helper()
	paint(t, e, sess, "x\n")
	sample(r, e, now)
	if got := r.Get(e.ID).Info().State; got != wire.StateWorking {
		t.Fatalf("state = %q after output, want working", got)
	}
	now = now.Add(agentstate.QuietAfter)
	sample(r, e, now)
	if got := r.Get(e.ID).Info().State; got != wire.StateIdle {
		t.Fatalf("state = %q after quiet, want idle", got)
	}
	return now
}

func goWorking(t *testing.T, r *Registry, e *Entry, sess *session.Session, now time.Time) {
	t.Helper()
	paint(t, e, sess, "y\n")
	sample(r, e, now)
	if got := r.Get(e.ID).Info().State; got != wire.StateWorking {
		t.Fatalf("state = %q, want working", got)
	}
}

func TestSendTypedRequiresIdle(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "t"})

	// A fresh session is idle: typed, with Enter.
	got, err := r.SendToSession(e.ID, "first note", false, wire.OriginUser)
	if err != nil || got != wire.DeliveryTyped {
		t.Fatalf("idle send = %q, %v; want typed", got, err)
	}
	waitFor(t, "first note in the PTY", func() bool { return strings.Contains(ptyText(sess), "first note") })

	// Working: refused, and nothing is typed.
	goWorking(t, r, e, sess, time.Now())
	if _, err := r.SendToSession(e.ID, "second note", false, wire.OriginUser); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("busy send err = %v, want ErrSessionBusy", err)
	}
	time.Sleep(150 * time.Millisecond)
	if strings.Contains(ptyText(sess), "second note") {
		t.Fatal("a refused message reached the PTY")
	}
}

func TestSendQueuedDrainsOneAtATimeOnIdle(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "q"})
	now := time.Now()
	goWorking(t, r, e, sess, now)

	for _, m := range []string{"one", "two"} {
		got, err := r.SendToSession(e.ID, m, true, wire.OriginUser)
		if err != nil || got != wire.DeliveryQueued {
			t.Fatalf("queue %q = %q, %v; want queued", m, got, err)
		}
	}
	time.Sleep(drainGrace + 100*time.Millisecond)
	if strings.Contains(ptyText(sess), "one") {
		t.Fatal("a queued message was typed while the session was working")
	}

	now = now.Add(agentstate.QuietAfter)
	sample(r, e, now) // idle edge arms the timer
	waitFor(t, "first queued message", func() bool { return strings.Contains(ptyText(sess), "one") })
	time.Sleep(drainGrace + 100*time.Millisecond)
	if strings.Contains(ptyText(sess), "two") {
		t.Fatal("two queued messages were typed on a single idle transition")
	}

	// Next idle transition delivers the second.
	goIdle(t, r, e, sess, now.Add(time.Second))
	waitFor(t, "second queued message", func() bool { return strings.Contains(ptyText(sess), "two") })
}

// If the session goes busy again during the grace period the timer
// types nothing: the next idle transition re-arms it.
func TestSendQueueSkippedWhenBusyAtFire(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "q"})
	now := time.Now()
	goWorking(t, r, e, sess, now)
	if _, err := r.SendToSession(e.ID, "later", true, wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	now = now.Add(agentstate.QuietAfter)
	sample(r, e, now) // idle: arms the timer
	goWorking(t, r, e, sess, now.Add(time.Millisecond))
	time.Sleep(drainGrace + 150*time.Millisecond)
	if strings.Contains(ptyText(sess), "later") {
		t.Fatal("queued message typed into a session that went busy during the grace period")
	}
	r.mu.Lock()
	n := len(r.entries[e.ID].pendingInputs)
	r.mu.Unlock()
	if n != 1 {
		t.Fatalf("queue length = %d after a skipped fire, want the message kept", n)
	}
}

func TestSendQueueIsCapped(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "cap"})
	goWorking(t, r, e, sess, time.Now())
	for i := 0; i < maxQueuedMessages; i++ {
		if _, err := r.SendToSession(e.ID, "m", true, wire.OriginUser); err != nil {
			t.Fatalf("queue #%d: %v", i, err)
		}
	}
	if _, err := r.SendToSession(e.ID, "m", true, wire.OriginUser); !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("over-cap queue err = %v, want ErrDeliveryFailed", err)
	}
}

// The exit path never reaches announceStateLocked, so the queue and its
// timer have to be cleaned up there.
func TestSendQueueClearedOnExit(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "x"})
	now := time.Now()
	goWorking(t, r, e, sess, now)
	if _, err := r.SendToSession(e.ID, "doomed", true, wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	// Arm a drain timer too, so the test proves it is stopped.
	sample(r, e, now.Add(agentstate.QuietAfter))
	r.mu.Lock()
	armed := r.entries[e.ID].drainTimer != nil
	r.mu.Unlock()
	if !armed {
		t.Fatal("setup: drain timer was not armed on the idle edge")
	}
	_ = sess.Close()
	waitFor(t, "queue cleared on exit", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		x := r.entries[e.ID]
		return x.sess == nil && len(x.pendingInputs) == 0 && x.pendingBytes == 0 && x.drainTimer == nil
	})
	if _, err := r.SendToSession(e.ID, "after", false, wire.OriginUser); !errors.Is(err, ErrNoLiveSession) {
		t.Fatalf("send to a dead session = %v, want ErrNoLiveSession", err)
	}
}

func TestSendRefusedWhileOpeningPromptPending(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "p"})
	queuePrompt(r, e.ID, "opening prompt", "")
	for _, queue := range []bool{false, true} {
		if _, err := r.SendToSession(e.ID, "hello", queue, wire.OriginUser); !errors.Is(err, ErrSessionBusy) {
			t.Fatalf("queue=%v err = %v, want ErrSessionBusy", queue, err)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(ptyText(sess), "hello") {
		t.Fatal("typed into a session whose opening prompt has not been placed")
	}
}

func TestSendToUnknownSession(t *testing.T) {
	r := freshRegistry(t)
	if _, err := r.SendToSession("nope", "hi", false, wire.OriginUser); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func nextKind(t *testing.T, ch Listener, kind string, within time.Duration) (wire.SessionInfo, bool) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return wire.SessionInfo{}, false
			}
			if ev.Kind == kind {
				return ev.Session, true
			}
		case <-deadline:
			return wire.SessionInfo{}, false
		}
	}
}

func arm(t *testing.T, r *Registry, id string) {
	t.Helper()
	on := true
	if _, err := r.Update(wire.UpdateSessionReq{SessionID: id, NotifyWhenIdle: &on}); err != nil {
		t.Fatal(err)
	}
	if !r.Get(id).Info().NotifyWhenIdle {
		t.Fatal("session did not arm")
	}
}

func TestNotifyWhenIdleFiresOnce(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "bell"})
	ch, unsub := r.Subscribe()
	defer unsub()
	now := time.Now()
	goWorking(t, r, e, sess, now)
	arm(t, r, e.ID)

	sample(r, e, now.Add(agentstate.QuietAfter))
	info, ok := nextKind(t, ch, wire.SessionEventIdlePing, time.Second)
	if !ok {
		t.Fatal("no idle_ping on the working -> idle transition")
	}
	if info.NotifyWhenIdle {
		t.Error("the ping still reports the bell as armed")
	}
	if r.Get(e.ID).Info().NotifyWhenIdle {
		t.Error("the flag did not clear")
	}
	// A second working -> idle must not ping again.
	goIdle(t, r, e, sess, now.Add(time.Second))
	if _, ok := nextKind(t, ch, wire.SessionEventIdlePing, 200*time.Millisecond); ok {
		t.Fatal("a one-shot bell pinged twice")
	}
}

func TestNotifyWhenIdleIgnoresIdleToIdle(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "bell"})
	ch, unsub := r.Subscribe()
	defer unsub()
	arm(t, r, e.ID) // armed while idle
	now := time.Now()
	sample(r, e, now)
	sample(r, e, now.Add(agentstate.QuietAfter))
	if _, ok := nextKind(t, ch, wire.SessionEventIdlePing, 200*time.Millisecond); ok {
		t.Fatal("pinged without the session ever working")
	}
	if !r.Get(e.ID).Info().NotifyWhenIdle {
		t.Error("an idle -> idle sample disarmed the bell")
	}
	_ = sess
}

func TestNotifyWhenIdleFiresOnExit(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "bell"})
	ch, unsub := r.Subscribe()
	defer unsub()
	goWorking(t, r, e, sess, time.Now())
	arm(t, r, e.ID)
	_ = sess.Close()
	info, ok := nextKind(t, ch, wire.SessionEventIdlePing, 5*time.Second)
	if !ok {
		t.Fatal("no idle_ping when the working session exited")
	}
	if info.State != wire.StateExited {
		t.Errorf("ping state = %q, want exited", info.State)
	}
}

func TestArmingADeadSessionIsIgnored(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "dead"})
	_ = sess.Close()
	waitFor(t, "exit", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.entries[e.ID].sess == nil
	})
	on := true
	if _, err := r.Update(wire.UpdateSessionReq{SessionID: e.ID, NotifyWhenIdle: &on}); err != nil {
		t.Fatal(err)
	}
	if r.Get(e.ID).Info().NotifyWhenIdle {
		t.Error("armed a bell on a session with no process")
	}
}

func TestSendACPSubmitsATurn(t *testing.T) {
	dir := useFakeACP(t, acptest.FlagBlock)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	got, err := r.SendToSession(e.ID, "do the thing", false, "plugin:x")
	if err != nil || got != wire.DeliveryACP {
		t.Fatalf("send = %q, %v; want acp", got, err)
	}
	// Busy mid-turn, regardless of queue: ACP has no queue.
	for _, q := range []bool{false, true} {
		if _, err := r.SendToSession(e.ID, "again", q, wire.OriginUser); !errors.Is(err, ErrSessionBusy) {
			t.Fatalf("queue=%v mid-turn err = %v, want ErrSessionBusy", q, err)
		}
	}
	acptest.Release(dir)
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
}

// Only a transition OUT OF working pings. The sampler never produces
// idle -> idle (announceStateLocked returns early on an unchanged
// snapshot), so the rule itself is pinned directly: a waiting state
// ending in idle is the user answering, not the session finishing.
func TestSettleSendStatePingsOnlyOutOfWorking(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, _ := liveSession(t, r, wire.CreateSpec{Name: "rule"})
	ch, unsub := r.Subscribe()
	defer unsub()
	for _, tc := range []struct {
		prev, cur string
		want      bool
	}{
		{wire.StateWorking, wire.StateIdle, true},
		{wire.StateWorking, wire.StateError, true},
		{wire.StateWorking, wire.StateWaitingInput, true}, // hook/ACP turn_end lands here
		{wire.StateWorking, wire.StateWaitingPermission, false},
		{wire.StateWaitingInput, wire.StateIdle, false},
		{wire.StateWaitingPermission, wire.StateIdle, false},
		{wire.StateIdle, wire.StateIdle, false},
		{wire.StateWorking, wire.StateExited, true}, // last: it drops the entry's send state
	} {
		r.mu.Lock()
		e.notifyWhenIdle = true
		r.settleSendStateLocked(e, agentstate.Snapshot{State: tc.prev}, agentstate.Snapshot{State: tc.cur})
		fired := !e.notifyWhenIdle
		r.mu.Unlock()
		_, got := nextKind(t, ch, wire.SessionEventIdlePing, 100*time.Millisecond)
		if got != tc.want || fired != tc.want {
			t.Errorf("%q -> %q: ping=%v disarmed=%v, want %v", tc.prev, tc.cur, got, fired, tc.want)
		}
	}
}

func hookEvent(t *testing.T, r *Registry, id, kind string) {
	t.Helper()
	if err := r.ApplyAgentEvent(id, wire.AgentEvent{SessionID: id, Kind: kind, Source: wire.StateSourceHook}); err != nil {
		t.Fatal(err)
	}
}

// A hook-tier agent ends its turn in waiting_input, never idle. That
// must ping, and must count as settled for the queue.
func TestHookTierTurnEndPingsAndDrains(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "hook"})
	ch, unsub := r.Subscribe()
	defer unsub()
	hookEvent(t, r, e.ID, wire.AgentEventPrompt)
	arm(t, r, e.ID)
	if got, err := r.SendToSession(e.ID, "after", true, wire.OriginUser); err != nil || got != wire.DeliveryQueued {
		t.Fatalf("send while working = %q, %v; want queued", got, err)
	}
	hookEvent(t, r, e.ID, wire.AgentEventTurnEnd)
	if _, ok := nextKind(t, ch, wire.SessionEventIdlePing, time.Second); !ok {
		t.Fatal("no idle_ping when a hook-tier turn ended")
	}
	waitFor(t, "queued note typed at turn end", func() bool { return strings.Contains(ptyText(sess), "after") })
}

// waiting_input from a question on screen is not a turn end: nothing
// is typed at it, queued or direct.
func TestQuestionOnScreenIsNotSettled(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "ask"})
	hookEvent(t, r, e.ID, wire.AgentEventPrompt)
	hookEvent(t, r, e.ID, wire.AgentEventWaitingInput)
	if _, err := r.SendToSession(e.ID, "typed?", false, wire.OriginUser); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("direct send at a question = %v, want ErrSessionBusy", err)
	}
	if got, err := r.SendToSession(e.ID, "held", true, wire.OriginUser); err != nil || got != wire.DeliveryQueued {
		t.Fatalf("queued send at a question = %q, %v", got, err)
	}
	time.Sleep(drainGrace + 150*time.Millisecond)
	if s := ptyText(sess); strings.Contains(s, "typed?") || strings.Contains(s, "held") {
		t.Fatal("text was typed at a question on screen")
	}
}

// A direct send to a hook-tier session that just finished is typed,
// and a second one right after is held back until the agent reports.
func TestSendAfterTurnEndTypesOnceThenHolds(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "done"})
	hookEvent(t, r, e.ID, wire.AgentEventPrompt)
	hookEvent(t, r, e.ID, wire.AgentEventTurnEnd)
	if got, err := r.SendToSession(e.ID, "go on", false, wire.OriginUser); err != nil || got != wire.DeliveryTyped {
		t.Fatalf("send after turn end = %q, %v; want typed", got, err)
	}
	waitFor(t, "note typed", func() bool { return strings.Contains(ptyText(sess), "go on") })
	if _, err := r.SendToSession(e.ID, "again", false, wire.OriginUser); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("second send inside the hold = %v, want ErrSessionBusy", err)
	}
}

// Queued notes go first: a new send to a settled session joins the back.
func TestSendKeepsQueueOrder(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "fifo"})
	now := time.Now()
	goWorking(t, r, e, sess, now)
	if _, err := r.SendToSession(e.ID, "older", true, wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	sample(r, e, now.Add(agentstate.QuietAfter)) // idle: drain timer armed
	got, err := r.SendToSession(e.ID, "newer", true, wire.OriginUser)
	if err != nil || got != wire.DeliveryQueued {
		t.Fatalf("send behind a queue = %q, %v; want queued", got, err)
	}
	waitFor(t, "older note first", func() bool { return strings.Contains(ptyText(sess), "older") })
	if strings.Contains(ptyText(sess), "newer") {
		t.Fatal("newer note typed ahead of, or with, the older one")
	}
}

// An ACP turn ends in waiting_input: the bell must ring there too.
func TestNotifyWhenIdleFiresOnACPTurnEnd(t *testing.T) {
	dir := useFakeACP(t, acptest.FlagBlock)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	ch, unsub := r.Subscribe()
	defer unsub()
	if _, err := r.SendToSession(e.ID, "go", false, wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	arm(t, r, e.ID)
	acptest.Release(dir)
	if _, ok := nextKind(t, ch, wire.SessionEventIdlePing, 5*time.Second); !ok {
		t.Fatal("no idle_ping when an ACP turn ended")
	}
}

// A note queued inside the hold after a direct send must still be
// delivered even though an idle session raises no further transition.
func TestQueuedInsideTypedHoldStillDrains(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "hold"})
	if got, err := r.SendToSession(e.ID, "first", false, wire.OriginUser); err != nil || got != wire.DeliveryTyped {
		t.Fatalf("first = %q, %v", got, err)
	}
	if got, err := r.SendToSession(e.ID, "second", true, wire.OriginUser); err != nil || got != wire.DeliveryQueued {
		t.Fatalf("second = %q, %v; want queued", got, err)
	}
	waitFor(t, "second note after the hold", func() bool { return strings.Contains(ptyText(sess), "second") })
}

// --- Claude inbox (spec 338, phase 2) ---

type inboxCall struct {
	ids  []string
	text string
}

// claudeSession is liveSession pretending to be a Claude entry, with a
// fake inbox recording what it was asked to deliver.
func claudeSession(t *testing.T, r *Registry, name string, send func(c inboxCall) error) (*Entry, *session.Session, *[]inboxCall) {
	t.Helper()
	e, sess := liveSession(t, r, wire.CreateSpec{Name: name})
	r.mu.Lock()
	e.Agent = "claude"
	e.AgentSessionID = "agent-" + e.ID
	r.mu.Unlock()
	var calls []inboxCall
	r.inboxSend = func(ids []string, text string) error {
		c := inboxCall{ids, text}
		calls = append(calls, c)
		if send == nil {
			return nil
		}
		return send(c)
	}
	return e, sess, &calls
}

func TestSendClaudeUsesSocketWhileWorking(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess, calls := claudeSession(t, r, "c1", nil)
	goWorking(t, r, e, sess, time.Now())
	got, err := r.SendToSession(e.ID, "mid-turn note", false, wire.OriginUser)
	if err != nil || got != wire.DeliverySocket {
		t.Fatalf("send = %q, %v; want socket", got, err)
	}
	if len(*calls) != 1 || (*calls)[0].text != "mid-turn note" {
		t.Fatalf("inbox calls = %+v", *calls)
	}
	if ids := (*calls)[0].ids; len(ids) != 2 || ids[0] != "agent-"+e.ID || ids[1] != e.ID {
		t.Fatalf("ids = %v, want agent id then hive id", ids)
	}
	time.Sleep(150 * time.Millisecond)
	if strings.Contains(ptyText(sess), "mid-turn note") {
		t.Fatal("a socket delivery was also typed")
	}
}

func TestSendClaudeFallsBackToTypedWhenInboxFails(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess, _ := claudeSession(t, r, "c2", func(inboxCall) error { return errors.New("dial refused") })
	got, err := r.SendToSession(e.ID, "fall back", false, wire.OriginUser)
	if err != nil || got != wire.DeliveryTyped {
		t.Fatalf("send = %q, %v; want typed", got, err)
	}
	waitFor(t, "typed", func() bool { return strings.Contains(ptyText(sess), "fall back") })
}

func TestSendClaudeFallsBackToQueuedWhenBusyAndInboxDown(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess, _ := claudeSession(t, r, "c3", func(inboxCall) error { return agent.ErrNoInbox })
	goWorking(t, r, e, sess, time.Now())
	if got, err := r.SendToSession(e.ID, "later", true, wire.OriginUser); err != nil || got != wire.DeliveryQueued {
		t.Fatalf("send = %q, %v; want queued", got, err)
	}
	if _, err := r.SendToSession(e.ID, "later2", false, wire.OriginUser); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("err = %v, want ErrSessionBusy", err)
	}
}

// Queued notes go first: the inbox must not overtake them.
func TestSendClaudeSkipsInboxBehindAQueue(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess, calls := claudeSession(t, r, "c4", func(inboxCall) error { return agent.ErrNoInbox })
	goWorking(t, r, e, sess, time.Now())
	if _, err := r.SendToSession(e.ID, "older", true, wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	n := len(*calls)
	r.inboxSend = func(ids []string, text string) error { *calls = append(*calls, inboxCall{ids, text}); return nil }
	got, err := r.SendToSession(e.ID, "newer", true, wire.OriginUser)
	if err != nil || got != wire.DeliveryQueued {
		t.Fatalf("send behind a queue = %q, %v; want queued", got, err)
	}
	if len(*calls) != n {
		t.Fatalf("inbox used while notes were queued: %+v", (*calls)[n:])
	}
}

func TestSendClaudeSocketHoldsTheTypingWindow(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, _, _ := claudeSession(t, r, "c5", nil)
	if got, err := r.SendToSession(e.ID, "one", false, wire.OriginUser); err != nil || got != wire.DeliverySocket {
		t.Fatalf("send = %q, %v", got, err)
	}
	r.mu.Lock()
	held := time.Since(e.typedAt) < typedHold
	r.mu.Unlock()
	if !held {
		t.Fatal("typedAt not set after a socket delivery")
	}
}

func TestSendNonClaudeNeverUsesInbox(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, _, calls := claudeSession(t, r, "sh", nil)
	r.mu.Lock()
	e.Agent = ""
	r.mu.Unlock()
	if got, err := r.SendToSession(e.ID, "plain", false, wire.OriginUser); err != nil || got != wire.DeliveryTyped {
		t.Fatalf("send = %q, %v; want typed", got, err)
	}
	if len(*calls) != 0 {
		t.Fatalf("inbox used for a non-claude session: %+v", *calls)
	}
}

// A dial that never returns must not hold r.mu.
func TestSendClaudeInboxDialDoesNotHoldTheLock(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, _, _ := claudeSession(t, r, "c6", nil)
	entered, release := make(chan struct{}), make(chan struct{})
	r.inboxSend = func([]string, string) error { close(entered); <-release; return nil }
	done := make(chan struct{})
	go func() { _, _ = r.SendToSession(e.ID, "slow", false, wire.OriginUser); close(done) }()
	<-entered
	lock := make(chan struct{})
	go func() { r.Get(e.ID); close(lock) }()
	select {
	case <-lock:
	case <-time.After(2 * time.Second):
		t.Fatal("registry blocked behind a slow inbox dial")
	}
	close(release)
	<-done
}

// The session dies while the dial is in flight: the fallback re-checks
// from scratch rather than trusting what it saw before.
func TestSendClaudeFallbackRechecksAfterDial(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, _, _ := claudeSession(t, r, "c7", nil)
	r.inboxSend = func([]string, string) error {
		r.mu.Lock()
		e.sess = nil
		r.mu.Unlock()
		return errors.New("gone")
	}
	if _, err := r.SendToSession(e.ID, "x", false, wire.OriginUser); !errors.Is(err, ErrNoLiveSession) {
		t.Fatalf("err = %v, want ErrNoLiveSession", err)
	}
}

// A socket send to an idle Claude starts a turn; the hook tier reports it
// and the idle ping still fires exactly once at its end.
func TestNotifyWhenIdleFiresOnceAfterSocketDelivery(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, _, _ := claudeSession(t, r, "c8", nil)
	ch, unsub := r.Subscribe()
	defer unsub()
	arm(t, r, e.ID)
	if got, err := r.SendToSession(e.ID, "go", false, wire.OriginUser); err != nil || got != wire.DeliverySocket {
		t.Fatalf("send = %q, %v", got, err)
	}
	hookEvent(t, r, e.ID, wire.AgentEventPrompt)
	hookEvent(t, r, e.ID, wire.AgentEventTurnEnd)
	if _, ok := nextKind(t, ch, wire.SessionEventIdlePing, 2*time.Second); !ok {
		t.Fatal("no idle ping after the turn the socket note started")
	}
	hookEvent(t, r, e.ID, wire.AgentEventPrompt)
	hookEvent(t, r, e.ID, wire.AgentEventTurnEnd)
	if _, ok := nextKind(t, ch, wire.SessionEventIdlePing, 300*time.Millisecond); ok {
		t.Fatal("idle ping fired twice")
	}
}

// --- Pi extension inbox (spec 338, phase 3) ---

// piSession is liveSession pretending to be a Pi entry, with a fake
// extension inbox recording the socket path and text it was handed.
func piSession(t *testing.T, r *Registry, name string, send func(sock, text string) error) (*Entry, *session.Session, *[][2]string) {
	t.Helper()
	e, sess := liveSession(t, r, wire.CreateSpec{Name: name})
	r.mu.Lock()
	e.Agent = "pi"
	// Short on purpose: PiInboxPath refuses a path that cannot be a
	// unix socket, and t.TempDir is too long on macOS.
	dir, err := os.MkdirTemp("", "hp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	r.socketPath = filepath.Join(dir, "hived.sock")
	r.mu.Unlock()
	var calls [][2]string
	r.piInboxSend = func(sock, text string) error {
		calls = append(calls, [2]string{sock, text})
		if send == nil {
			return nil
		}
		return send(sock, text)
	}
	return e, sess, &calls
}

func TestSendPiUsesExtensionWhileWorking(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess, calls := piSession(t, r, "p1", nil)
	goWorking(t, r, e, sess, time.Now())
	got, err := r.SendToSession(e.ID, "mid-turn note", false, wire.OriginUser)
	if err != nil || got != wire.DeliveryExtension {
		t.Fatalf("send = %q, %v; want extension", got, err)
	}
	r.mu.Lock()
	want := agent.PiInboxPath(r.socketPath, e.ID)
	r.mu.Unlock()
	if len(*calls) != 1 || (*calls)[0] != [2]string{want, "mid-turn note"} || want == "" {
		t.Fatalf("calls = %+v, want one to %q", *calls, want)
	}
	time.Sleep(150 * time.Millisecond)
	if strings.Contains(ptyText(sess), "mid-turn note") {
		t.Fatal("an extension delivery was also typed")
	}
}

// A dead or stale socket (a crashed pi) must fall back, not lose the note.
func TestSendPiFallsBackToTypedWhenInboxFails(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess, _ := piSession(t, r, "p2", func(string, string) error { return errors.New("connection refused") })
	got, err := r.SendToSession(e.ID, "fall back", false, wire.OriginUser)
	if err != nil || got != wire.DeliveryTyped {
		t.Fatalf("send = %q, %v; want typed", got, err)
	}
	waitFor(t, "typed", func() bool { return strings.Contains(ptyText(sess), "fall back") })
}

func TestSendPiSkipsInboxBehindAQueue(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess, calls := piSession(t, r, "p3", func(string, string) error { return agent.ErrNoInbox })
	goWorking(t, r, e, sess, time.Now())
	if _, err := r.SendToSession(e.ID, "older", true, wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	n := len(*calls)
	r.piInboxSend = func(sock, text string) error { *calls = append(*calls, [2]string{sock, text}); return nil }
	if got, err := r.SendToSession(e.ID, "newer", true, wire.OriginUser); err != nil || got != wire.DeliveryQueued {
		t.Fatalf("send behind a queue = %q, %v; want queued", got, err)
	}
	if len(*calls) != n {
		t.Fatalf("inbox used while notes were queued: %+v", (*calls)[n:])
	}
}

// An unrelated agent never reaches either inbox.
func TestSendOtherAgentNeverUsesPiInbox(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, _, calls := piSession(t, r, "p4", nil)
	r.mu.Lock()
	e.Agent = "codex"
	r.mu.Unlock()
	if got, err := r.SendToSession(e.ID, "plain", false, wire.OriginUser); err != nil || got != wire.DeliveryTyped {
		t.Fatalf("send = %q, %v; want typed", got, err)
	}
	if len(*calls) != 0 {
		t.Fatalf("pi inbox used for a non-pi session: %+v", *calls)
	}
}
