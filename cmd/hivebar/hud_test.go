// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

type fakeView struct {
	shown   []hudState
	clears  int
	hidden  int
	visible bool
}

func (v *fakeView) Show(st hudState, clear bool) {
	v.shown = append(v.shown, st)
	v.visible = true
	if clear {
		v.clears++
	}
}

func (v *fakeView) Hide(restore bool) {
	v.hidden++
	v.visible = false
}

func (v *fakeView) last() hudState { return v.shown[len(v.shown)-1] }

type sent struct {
	id, text string
	queue    bool
}

type fakeSender struct {
	sends     []sent
	connected bool
}

func (s *fakeSender) SendToSession(id, text string, queue bool) bool {
	s.sends = append(s.sends, sent{id, text, queue})
	return s.connected
}

func newTestHUD(sessions ...wire.SessionInfo) (*HUD, *fakeView, *fakeSender) {
	v, s := &fakeView{}, &fakeSender{connected: true}
	h := NewHUD(v, s)
	h.goSend = func(f func()) { f() }
	h.SetSessions([]wire.ProjectInfo{{ID: "p", Name: "proj"}}, sessions)
	return h, v, s
}

func sess(id string, order int, focusedAt int64) wire.SessionInfo {
	return wire.SessionInfo{ID: id, Name: id, Title: "t-" + id, Alive: true, ProjectID: "p", Order: order, FocusedAt: focusedAt}
}

func selected(st hudState) string {
	if st.Selected < 0 {
		return ""
	}
	return st.Targets[st.Selected].ID
}

func TestHUDDefaultsToNewestFocused(t *testing.T) {
	h, v, _ := newTestHUD(sess("a", 0, 100), sess("b", 1, 300), sess("c", 2, 200))
	h.Open()
	if got := selected(v.last()); got != "b" {
		t.Fatalf("default target = %q, want b (newest focus)", got)
	}
	if v.clears != 1 {
		t.Fatalf("opening cleared the field %d times, want 1", v.clears)
	}
	if !strings.Contains(v.last().Targets[v.last().Selected].Label, "t-b") {
		t.Errorf("target label %q lacks the title", v.last().Targets[v.last().Selected].Label)
	}
}

func TestHUDDefaultSkipsDeadSessions(t *testing.T) {
	dead := sess("b", 1, 900)
	dead.Alive = false
	h, v, _ := newTestHUD(sess("a", 0, 100), dead)
	h.Open()
	st := v.last()
	if selected(st) != "a" {
		t.Fatalf("default = %q, want a: a dead session cannot receive text", selected(st))
	}
	for _, tg := range st.Targets {
		if tg.ID == "b" {
			t.Fatal("a dead session is offered as a target")
		}
	}
}

// With no focus report since the daemon started there is no default:
// guessing would put dictated text into a session the user did not pick.
func TestHUDNoFocusMeansNoDefault(t *testing.T) {
	h, v, s := newTestHUD(sess("a", 0, 0), sess("b", 1, 0))
	h.Open()
	if selected(v.last()) != "" || v.last().CanSend {
		t.Fatalf("got target %q, CanSend %v; want none", selected(v.last()), v.last().CanSend)
	}
	h.Submit("hello")
	if len(s.sends) != 0 {
		t.Fatalf("sent with no target: %+v", s.sends)
	}
}

func TestHUDNoSessionsEnterInert(t *testing.T) {
	h, v, s := newTestHUD()
	h.Open()
	if !strings.Contains(v.last().Status, "No live sessions") || v.last().CanSend {
		t.Fatalf("empty state = %+v", v.last())
	}
	h.Submit("hello")
	h.Move(1)
	if len(s.sends) != 0 {
		t.Fatalf("sent with no sessions: %+v", s.sends)
	}
}

func TestHUDEscSendsNothing(t *testing.T) {
	h, v, s := newTestHUD(sess("a", 0, 1))
	h.Open()
	h.Cancel()
	if len(s.sends) != 0 || v.visible || v.hidden != 1 {
		t.Fatalf("after Esc: sends %+v, visible %v, hidden %d", s.sends, v.visible, v.hidden)
	}
	h.Submit("late")
	if len(s.sends) != 0 {
		t.Fatal("a submit after Esc sent text")
	}
}

func TestHUDEnterSendsOnceToTarget(t *testing.T) {
	h, v, s := newTestHUD(sess("a", 0, 1), sess("b", 1, 2))
	h.Open()
	h.Submit("fix the tests")
	if len(s.sends) != 1 || s.sends[0] != (sent{"b", "fix the tests", false}) {
		t.Fatalf("sends = %+v, want one to b without queue", s.sends)
	}
	if !v.visible {
		t.Fatal("panel closed before the daemon answered")
	}
	h.OnSent("b")
	if v.visible {
		t.Fatal("panel still up after SENT")
	}
}

func TestHUDBlankTextIgnored(t *testing.T) {
	h, _, s := newTestHUD(sess("a", 0, 1))
	h.Open()
	h.Submit("  \n ")
	if len(s.sends) != 0 {
		t.Fatalf("blank text sent: %+v", s.sends)
	}
}

func TestHUDSecondEnterWhileSendingIgnored(t *testing.T) {
	h, _, s := newTestHUD(sess("a", 0, 1))
	h.Open()
	h.Submit("one")
	h.Submit("one")
	h.Move(1)
	if len(s.sends) != 1 {
		t.Fatalf("sends = %d, want 1: a second Enter must not double-send", len(s.sends))
	}
}

func TestHUDSwitchTargetSendsToNew(t *testing.T) {
	h, v, s := newTestHUD(sess("a", 0, 1), sess("b", 1, 2), sess("c", 2, 0))
	h.Open()
	h.Move(1) // b → c
	if selected(v.last()) != "c" {
		t.Fatalf("after ↓ target = %q, want c", selected(v.last()))
	}
	h.Move(1) // wraps to a
	h.Move(-1)
	h.Move(-1) // a → c → b
	h.Select(0)
	h.Submit("x")
	if len(s.sends) != 1 || s.sends[0].id != "a" {
		t.Fatalf("sends = %+v, want one to a", s.sends)
	}
}

func TestHUDBusyThenEnterQueues(t *testing.T) {
	h, v, s := newTestHUD(sess("a", 0, 1))
	h.Open()
	h.Submit("hello")
	h.OnError("a", wire.ErrCodeSessionBusy, "busy")
	if !v.visible || v.last().Hints != hintsBusy || !v.last().CanSend {
		t.Fatalf("busy state = %+v", v.last())
	}
	h.Submit("hello")
	if len(s.sends) != 2 || !s.sends[1].queue || s.sends[1].id != "a" {
		t.Fatalf("sends = %+v, want the second to queue to a", s.sends)
	}
	h.OnSent("a")
	if v.visible {
		t.Fatal("panel still up after the queued send")
	}
}

func TestHUDBusyACPCannotQueue(t *testing.T) {
	acp := sess("a", 0, 1)
	acp.Kind = wire.KindACP
	h, v, s := newTestHUD(acp)
	h.Open()
	h.Submit("hello")
	h.OnError("a", wire.ErrCodeSessionBusy, "busy")
	if v.last().Hints == hintsBusy || !strings.Contains(v.last().Status, "Try again") {
		t.Fatalf("ACP busy state = %+v", v.last())
	}
	h.Submit("hello")
	if len(s.sends) != 2 || s.sends[1].queue {
		t.Fatalf("sends = %+v, an ACP resend must not ask to queue", s.sends)
	}
}

func TestHUDDeadTargetKeepsPanel(t *testing.T) {
	h, v, s := newTestHUD(sess("a", 0, 1))
	h.Open()
	h.Submit("hello")
	h.OnError("a", wire.ErrCodeSessionDead, "dead")
	if !v.visible || !strings.Contains(v.last().Status, "not running") {
		t.Fatalf("after session_dead: visible %v, status %q", v.visible, v.last().Status)
	}
	if v.clears != 1 {
		t.Fatal("an error cleared the text field")
	}
	if len(s.sends) != 1 {
		t.Fatalf("sends = %+v", s.sends)
	}
}

func TestHUDDisconnectedReports(t *testing.T) {
	h, v, s := newTestHUD(sess("a", 0, 1))
	s.connected = false
	h.Open()
	h.Submit("hello")
	if !v.visible || !strings.Contains(v.last().Status, "not running") || !v.last().CanSend {
		t.Fatalf("state = %+v", v.last())
	}
}

func TestHUDOversizeRefused(t *testing.T) {
	h, v, s := newTestHUD(sess("a", 0, 1))
	h.Open()
	h.Submit(strings.Repeat("x", wire.MaxMessageLen+1))
	if len(s.sends) != 0 || !strings.Contains(v.last().Status, "Too long") {
		t.Fatalf("sends %d, status %q", len(s.sends), v.last().Status)
	}
}

func TestHUDEscWhileSendingIgnoresLateSent(t *testing.T) {
	h, v, _ := newTestHUD(sess("a", 0, 1))
	h.Open()
	h.Submit("hello")
	h.Cancel()
	shows, hides := len(v.shown), v.hidden
	h.OnSent("a")
	h.OnError("a", wire.ErrCodeSessionBusy, "")
	if len(v.shown) != shows || v.hidden != hides || v.visible {
		t.Fatal("a reply after Esc touched the panel")
	}
}

// A live update while the panel is open (a title change, a new session)
// must keep the text and the target the user chose.
func TestHUDKeepsTextAndManualTargetAcrossEvents(t *testing.T) {
	h, v, _ := newTestHUD(sess("a", 0, 1), sess("b", 1, 2))
	h.Open()
	h.Move(-1) // b → a, by hand
	// A GUI focus report now makes b newest again, and a session appears.
	b := sess("b", 1, 50)
	h.SetSessions([]wire.ProjectInfo{{ID: "p", Name: "proj"}}, []wire.SessionInfo{sess("a", 0, 1), b, sess("c", 2, 0)})
	if selected(v.last()) != "a" {
		t.Fatalf("target moved to %q; the user picked a", selected(v.last()))
	}
	if v.clears != 1 || len(v.last().Targets) != 3 {
		t.Fatalf("clears %d, targets %d", v.clears, len(v.last().Targets))
	}
}

func TestHUDTargetKilledWhileOpen(t *testing.T) {
	h, v, s := newTestHUD(sess("a", 0, 1), sess("b", 1, 2))
	h.Open()
	h.SetSessions(nil, []wire.SessionInfo{sess("a", 0, 1)})
	if selected(v.last()) != "" || !strings.Contains(v.last().Status, "ended") {
		t.Fatalf("after the target died: target %q, status %q", selected(v.last()), v.last().Status)
	}
	h.Submit("hello")
	if len(s.sends) != 0 {
		t.Fatalf("text went to %+v after its target died", s.sends)
	}
}

func TestHUDListsAllLiveSessions(t *testing.T) {
	var ss []wire.SessionInfo
	for i := 0; i < maxSessionRows+6; i++ {
		ss = append(ss, sess(fmt.Sprintf("s%02d", i), i, 0))
	}
	h, v, _ := newTestHUD(ss...)
	h.Open()
	if got := len(v.last().Targets); got != len(ss) {
		t.Fatalf("targets = %d, want %d (not capped at the menu's pool)", got, len(ss))
	}
}

func TestHUDSecondOpenKeepsText(t *testing.T) {
	h, v, _ := newTestHUD(sess("a", 0, 1))
	h.Open()
	h.Open()
	if v.clears != 1 {
		t.Fatalf("a second hotkey press cleared the field (%d clears)", v.clears)
	}
}
