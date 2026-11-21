// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// fakeHandy records calls in order. toggleErr fails the next Toggle.
type fakeHandy struct {
	mu        sync.Mutex
	available bool
	calls     []string
	toggleErr error
	cancelErr error // every Cancel fails with it
}

func (f *fakeHandy) Available() bool { return f.available }

func (f *fakeHandy) Toggle() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "toggle")
	err := f.toggleErr
	f.toggleErr = nil
	return err
}

func (f *fakeHandy) Cancel() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "cancel")
	return f.cancelErr
}

func (f *fakeHandy) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// fakeTimers holds AfterFunc callbacks until a test fires them.
type fakeTimers struct {
	pending []*fakeTimer
}

type fakeTimer struct {
	d       time.Duration
	f       func()
	stopped bool
}

func (ft *fakeTimers) afterFunc(d time.Duration, f func()) func() bool {
	t := &fakeTimer{d: d, f: f}
	ft.pending = append(ft.pending, t)
	return func() bool { t.stopped = true; return true }
}

// fire runs the newest timer that was not stopped, reporting its delay.
func (ft *fakeTimers) fire(t *testing.T) time.Duration {
	t.Helper()
	for i := len(ft.pending) - 1; i >= 0; i-- {
		if tm := ft.pending[i]; !tm.stopped {
			tm.stopped = true
			tm.f()
			return tm.d
		}
	}
	t.Fatal("no live timer to fire")
	return 0
}

// fireStale runs every timer, stopped or not, as a late AfterFunc would.
func (ft *fakeTimers) fireStale() {
	for _, tm := range ft.pending {
		tm.f()
	}
}

func newHandyHUD(t *testing.T) (*HUD, *fakeView, *fakeSender, *fakeHandy, *fakeTimers) {
	t.Helper()
	h, v, s := newTestHUD(sess("a", 0, 1))
	fh, ft := &fakeHandy{available: true}, &fakeTimers{}
	h.handy = fh
	h.afterFunc = ft.afterFunc
	h.SetHandyAutoStart(true)
	return h, v, s, fh, ft
}

// drain waits for every Handy call queued so far, and its result, to
// finish: the worker runs them in order, so a marker queued now runs
// last.
func drain(h *HUD) {
	done := make(chan struct{})
	h.goHandy(func() { close(done) })
	<-done
}

func wantCalls(t *testing.T, h *HUD, fh *fakeHandy, want ...string) {
	t.Helper()
	drain(h)
	if got := fh.got(); !slices.Equal(got, want) {
		t.Fatalf("Handy calls = %v, want %v", got, want)
	}
}

func TestHUDAutoStartTogglesHandyOnOpen(t *testing.T) {
	h, v, _, fh, _ := newHandyHUD(t)
	h.Open()
	wantCalls(t, h, fh, "toggle")
	if st := v.last(); st.Status != statusRecording || st.Hints != hintsRecord {
		t.Fatalf("panel = %q / %q, want recording", st.Status, st.Hints)
	}
}

func TestHUDAutoStartOffNeverTouchesHandy(t *testing.T) {
	h, _, s, fh, _ := newHandyHUD(t)
	h.SetHandyAutoStart(false)
	h.Open()
	h.TextChanged("hello", false)
	h.Submit("hello")
	h.Open()
	h.Cancel()
	wantCalls(t, h, fh)
	if len(s.sends) != 1 {
		t.Fatalf("sends = %+v, want the one typed send", s.sends)
	}
}

// Handy not installed or not running: the HUD behaves exactly as spec 553.
func TestHUDAutoStartSkippedWhenHandyUnavailable(t *testing.T) {
	h, v, s, fh, _ := newHandyHUD(t)
	fh.available = false
	h.Open()
	if st := v.last(); st.Status == statusRecording || st.Hints != hintsEditing {
		t.Fatalf("panel = %q / %q, want the plain editing state", st.Status, st.Hints)
	}
	h.Submit("hello")
	h.Cancel()
	wantCalls(t, h, fh)
	if len(s.sends) != 1 || s.sends[0].text != "hello" {
		t.Fatalf("sends = %+v", s.sends)
	}
}

func TestHUDRefocusDoesNotToggleAgain(t *testing.T) {
	h, _, _, fh, _ := newHandyHUD(t)
	h.Open()
	h.Open()
	wantCalls(t, h, fh, "toggle")
}

// The spec's Enter path: stop Handy, wait for the paste, send it.
func TestHUDEnterWhileRecordingStopsAndSendsOnText(t *testing.T) {
	h, v, s, fh, ft := newHandyHUD(t)
	h.Open()
	h.Submit("")
	wantCalls(t, h, fh, "toggle", "toggle")
	if v.last().Status != statusAwaiting || v.last().CanSend {
		t.Fatalf("panel while waiting = %+v", v.last())
	}
	if len(s.sends) != 0 {
		t.Fatal("sent before the transcription arrived")
	}
	h.TextChanged("hello from handy", true)
	if d := ft.fire(t); d != handyQuiet {
		t.Fatalf("fired a %v timer, want the %v quiet wait", d, handyQuiet)
	}
	if len(s.sends) != 1 || s.sends[0] != (sent{"a", "hello from handy", false}) {
		t.Fatalf("sends = %+v", s.sends)
	}
	h.OnSent("a")
	// The close after the send cancels Handy: a harmless no-op when it
	// is idle, and the end of any recording a race started.
	wantCalls(t, h, fh, "toggle", "toggle", "cancel")
}

func TestHUDEnterWhileRecordingTimesOutSendsNothing(t *testing.T) {
	h, v, s, _, ft := newHandyHUD(t)
	h.Open()
	h.TextChanged("t", false) // typed, so not Handy's delivery
	h.Submit("t")
	if d := ft.fire(t); d != handyWait {
		t.Fatalf("fired a %v timer, want the %v wait", d, handyWait)
	}
	if len(s.sends) != 0 {
		t.Fatalf("sent with no transcription: %+v", s.sends)
	}
	if st := v.last(); st.Status != statusNoText || !st.CanSend {
		t.Fatalf("after timeout = %+v", st)
	}
	// The text stays, and Enter now sends it without touching Handy.
	h.Submit("t")
	if len(s.sends) != 1 || s.sends[0].text != "t" {
		t.Fatalf("sends = %+v", s.sends)
	}
}

// Handy stopped with its own key pastes its transcription; Enter then
// sends it without another toggle, which would start a new recording.
func TestHUDPasteEndsRecordingSoEnterDoesNotToggle(t *testing.T) {
	h, v, s, fh, _ := newHandyHUD(t)
	h.Open()
	h.TextChanged("x", true)
	if v.last().Status == statusRecording {
		t.Fatal("still showing a recording after the paste")
	}
	h.Submit("x")
	wantCalls(t, h, fh, "toggle")
	if len(s.sends) != 1 {
		t.Fatalf("sends = %+v", s.sends)
	}
}

func TestHUDMultiCharInsertionCountsAsDelivery(t *testing.T) {
	h, _, s, fh, _ := newHandyHUD(t)
	h.Open()
	h.TextChanged("hi there", false) // Ctrl+Shift+V or direct input: many at once
	h.Submit("hi there")
	wantCalls(t, h, fh, "toggle")
	if len(s.sends) != 1 {
		t.Fatalf("sends = %+v", s.sends)
	}
}

func TestHUDSingleCharTypingIsNotDelivery(t *testing.T) {
	h, _, s, fh, _ := newHandyHUD(t)
	h.Open()
	for _, text := range []string{"h", "hi", "hi!"} {
		h.TextChanged(text, false)
	}
	h.Submit("hi!")
	wantCalls(t, h, fh, "toggle", "toggle")
	if len(s.sends) != 0 {
		t.Fatalf("typing while Handy listens was taken as its delivery: %+v", s.sends)
	}
}

func TestHUDEscWhileRecordingCancelsHandy(t *testing.T) {
	h, _, s, fh, _ := newHandyHUD(t)
	h.Open()
	h.Cancel()
	wantCalls(t, h, fh, "toggle", "cancel")
	if len(s.sends) != 0 {
		t.Fatal("Esc sent something")
	}
}

func TestHUDEscWhileAwaitingCancelsHandy(t *testing.T) {
	h, _, s, fh, ft := newHandyHUD(t)
	h.Open()
	h.Submit("")
	h.Cancel()
	wantCalls(t, h, fh, "toggle", "toggle", "cancel")
	ft.fireStale() // the wait outliving the panel must do nothing
	h.TextChanged("late paste", true)
	if len(s.sends) != 0 {
		t.Fatalf("sent after Esc: %+v", s.sends)
	}
}

func TestHUDEnterWhileAwaitingIgnored(t *testing.T) {
	h, _, s, fh, _ := newHandyHUD(t)
	h.Open()
	h.Submit("")
	h.Submit("")
	h.Move(1)
	wantCalls(t, h, fh, "toggle", "toggle")
	if len(s.sends) != 0 {
		t.Fatalf("sends = %+v", s.sends)
	}
}

// A transcription typed in pieces is sent once the field goes quiet,
// and each piece pushes the wait back.
func TestHUDDirectInputDeliveryDebounced(t *testing.T) {
	h, _, s, _, ft := newHandyHUD(t)
	h.Open()
	h.Submit("")
	h.TextChanged("hel", false)
	h.TextChanged("hello wor", false)
	h.TextChanged("hello world", false)
	ft.fire(t)
	if len(s.sends) != 1 || s.sends[0].text != "hello world" {
		t.Fatalf("sends = %+v", s.sends)
	}
	live := 0
	for _, tm := range ft.pending {
		if !tm.stopped {
			live++
		}
	}
	if live != 0 {
		t.Fatalf("%d timers still armed after the send", live)
	}
}

func TestHUDWaitReArmsOnChangeUpToCap(t *testing.T) {
	h, _, s, _, _ := newHandyHUD(t)
	h.Open()
	h.Submit("")
	h.mu.Lock()
	h.awaitStart = time.Now().Add(-handyCap)
	h.mu.Unlock()
	h.TextChanged("still typing", false)
	if len(s.sends) != 1 || s.sends[0].text != "still typing" {
		t.Fatalf("past the cap a change sends at once; sends = %+v", s.sends)
	}
}

func TestHUDLatePasteAfterTimeoutIsNotSent(t *testing.T) {
	h, _, s, _, ft := newHandyHUD(t)
	h.Open()
	h.Submit("")
	ft.fire(t)
	h.TextChanged("late transcription", true)
	if len(s.sends) != 0 {
		t.Fatalf("a paste after the wait was sent unasked: %+v", s.sends)
	}
}

func TestHUDHandyToggleErrorShowsStatus(t *testing.T) {
	h, v, _, fh, _ := newHandyHUD(t)
	fh.toggleErr = errors.New("boom")
	h.Open()
	drain(h)
	if st := v.last(); st.Status != statusStartFailed || st.Hints != hintsEditing {
		t.Fatalf("after a failed start = %q / %q", st.Status, st.Hints)
	}
	h.Cancel()
	// Cancelled anyway: harmless on an idle Handy, and the only safe
	// answer when Hive cannot know whether a later toggle started one.
	wantCalls(t, h, fh, "toggle", "cancel")
}

// A start that fails after Enter is already waiting: there is no
// transcription coming, so the panel goes back to editing.
func TestHUDStartFailureWhileAwaitingReturnsToEditing(t *testing.T) {
	h, v, s, fh, ft := newHandyHUD(t)
	var held []func()
	h.goHandy = func(f func()) { held = append(held, f) }
	fh.toggleErr = errors.New("boom")
	h.Open()
	h.Submit("")
	held[0]() // the start fails now, after Enter
	if st := v.last(); st.Status != statusStartFailed || !st.CanSend {
		t.Fatalf("after a late start failure = %+v", st)
	}
	ft.fireStale()
	if v.last().Status != statusStartFailed || len(s.sends) != 0 {
		t.Fatal("the wait still ran after the start failed")
	}
	held[1]() // the stop-toggle Enter queued, which may have started one
	h.Cancel()
	if len(held) != 3 {
		t.Fatalf("closing queued %d Handy calls in all, want start, stop and cancel", len(held))
	}
}

func TestHUDStopToggleFailureReturnsToEditing(t *testing.T) {
	h, v, s, fh, ft := newHandyHUD(t)
	h.Open()
	drain(h)
	fh.toggleErr = errors.New("boom")
	h.Submit("")
	drain(h)
	if st := v.last(); st.Status != statusStopFailed || !st.CanSend {
		t.Fatalf("after a failed stop = %+v", st)
	}
	ft.fireStale()
	if v.last().Status != statusStopFailed {
		t.Fatal("the stopped wait still fired")
	}
	// Still recording as far as the HUD knows: Enter tries to stop again.
	h.Submit("")
	wantCalls(t, h, fh, "toggle", "toggle", "toggle")
	if len(s.sends) != 0 {
		t.Fatal("sent something")
	}
}

func TestHUDCloseAlwaysCancelsHandyItStarted(t *testing.T) {
	t.Run("send", func(t *testing.T) {
		h, _, _, fh, _ := newHandyHUD(t)
		h.Open()
		h.TextChanged("pasted words", true)
		h.Submit("pasted words")
		h.OnSent("a")
		wantCalls(t, h, fh, "toggle", "cancel")
	})
	t.Run("disconnect", func(t *testing.T) {
		h, v, _, fh, _ := newHandyHUD(t)
		h.Open()
		h.Disconnected()
		wantCalls(t, h, fh, "toggle", "cancel")
		if v.last().Status != "Lost Hive's daemon; Handy was stopped." {
			t.Fatalf("status = %q", v.last().Status)
		}
		h.Cancel()
		wantCalls(t, h, fh, "toggle", "cancel") // already cancelled once
	})
	t.Run("not started", func(t *testing.T) {
		h, _, _, fh, _ := newHandyHUD(t)
		h.SetHandyAutoStart(false)
		h.Open()
		h.Cancel()
		h.Open()
		h.Disconnected()
		wantCalls(t, h, fh)
	})
}

// A result from an earlier opening must not touch the current one.
func TestHUDStaleHandyResultIgnoredAfterReopen(t *testing.T) {
	h, v, _, fh, _ := newHandyHUD(t)
	var held []func()
	h.goHandy = func(f func()) { held = append(held, f) }
	fh.toggleErr = errors.New("boom")
	h.Open()
	h.Cancel()
	h.Open()
	held[0]() // the first opening's failed start lands now
	if v.last().Status == statusStartFailed {
		t.Fatal("a stale start failure changed the new panel")
	}
}

func TestHUDTimerAfterCloseAndReopenIgnored(t *testing.T) {
	h, v, s, _, ft := newHandyHUD(t)
	h.Open()
	h.Submit("")
	h.Cancel()
	h.Open()
	ft.fireStale()
	if v.last().Status == statusNoText || len(s.sends) != 0 {
		t.Fatalf("an old wait reached the new panel: %q, %+v", v.last().Status, s.sends)
	}
}

// blockingHandy's Toggle waits for release, so a test can prove the HUD
// returns while Handy has not answered.
type blockingHandy struct {
	fakeHandy
	release chan struct{}
}

func (b *blockingHandy) Toggle() error {
	<-b.release
	return b.fakeHandy.Toggle()
}

func TestHUDHandyCallsNeverBlockTheLock(t *testing.T) {
	h, _, _ := newTestHUD(sess("a", 0, 1))
	bh := &blockingHandy{fakeHandy: fakeHandy{available: true}, release: make(chan struct{})}
	h.handy = bh
	h.SetHandyAutoStart(true)
	done := make(chan struct{})
	go func() {
		h.Open()
		h.Submit("")
		h.Cancel()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Open/Submit/Cancel waited on Handy")
	}
	close(bh.release)
}

// Calls run in the order they were made, on one worker: an Esc right
// after opening must cancel after the start, or Handy would be left
// recording with the panel gone.
func TestHUDHandyCallsRunInOrder(t *testing.T) {
	h, _, _ := newTestHUD(sess("a", 0, 1))
	bh := &blockingHandy{fakeHandy: fakeHandy{available: true}, release: make(chan struct{})}
	h.handy = bh
	h.SetHandyAutoStart(true)
	h.Open()
	h.Submit("")
	h.Cancel()
	close(bh.release)
	deadline := time.Now().Add(2 * time.Second)
	for len(bh.got()) < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got, want := bh.got(), []string{"toggle", "toggle", "cancel"}; !slices.Equal(got, want) {
		t.Fatalf("Handy calls ran as %v, want %v", got, want)
	}
}

var _ handyCtl = (*execHandy)(nil)

func TestExecHandyRunsFlags(t *testing.T) {
	var ran [][]string
	h := &execHandy{
		run: func(_ time.Duration, name string, args ...string) (string, error) {
			ran = append(ran, append([]string{name}, args...))
			return "", nil
		},
		paths:   func() []string { return []string{"/nonexistent", "/bin/sh"} },
		running: func() bool { return true },
	}
	if !h.Available() {
		t.Fatal("unavailable with a binary and a running app")
	}
	_ = h.Toggle()
	_ = h.Cancel()
	want := [][]string{{"/bin/sh", "--toggle-transcription"}, {"/bin/sh", "--cancel"}}
	if !slices.EqualFunc(ran, want, slices.Equal) {
		t.Fatalf("ran %v, want %v", ran, want)
	}
	h.running = func() bool { return false }
	if h.Available() {
		t.Error("available while Handy is not running; Hive must never launch it")
	}
	ran = nil
	if h.Toggle() == nil || h.Cancel() == nil || len(ran) != 0 {
		t.Errorf("ran %v with Handy stopped; either flag would launch it", ran)
	}
	h.paths = func() []string { return []string{"/nonexistent"} }
	h.running = func() bool { return true }
	if h.Available() || h.Toggle() == nil {
		t.Error("no binary, yet available or toggled")
	}
}

// Review of PR 574, finding 1: Enter had already stopped Handy, so after
// the target ends the next ↩ must send, not start a new recording.
func TestHUDTargetLossWhileAwaitingEndsRecording(t *testing.T) {
	h, v, s, fh, _ := newHandyHUD(t)
	h.SetSessions([]wire.ProjectInfo{{ID: "p", Name: "proj"}}, []wire.SessionInfo{sess("a", 0, 1), sess("b", 1, 0)})
	h.Open()
	h.Submit("")
	gone := sess("a", 0, 1)
	gone.Alive = false
	h.SetSessions([]wire.ProjectInfo{{ID: "p", Name: "proj"}}, []wire.SessionInfo{gone, sess("b", 1, 0)})
	if v.last().Hints == hintsRecord {
		t.Fatal("still offering to stop a Handy that Enter already stopped")
	}
	h.Move(1)
	h.Submit("late words")
	wantCalls(t, h, fh, "toggle", "toggle")
	if len(s.sends) != 1 || s.sends[0] != (sent{"b", "late words", false}) {
		t.Fatalf("sends = %+v", s.sends)
	}
}

// Finding 2: whether the text arrived matters more than Handy stopping.
func TestHUDDisconnectWhileSendingKeepsWarning(t *testing.T) {
	h, v, _, fh, _ := newHandyHUD(t)
	h.Open()
	h.TextChanged("pasted words", true)
	h.Submit("pasted words") // sending; no reply yet
	h.Disconnected()
	if !strings.Contains(v.last().Status, "may have arrived") {
		t.Fatalf("status = %q; the may-have-arrived warning was lost", v.last().Status)
	}
	wantCalls(t, h, fh, "toggle", "cancel")
}

// Finding 3: no microphone when nothing could be sent.
func TestHUDNoAutoStartWhenNothingCanBeSent(t *testing.T) {
	t.Run("no live session", func(t *testing.T) {
		h, _, _, fh, _ := newHandyHUD(t)
		dead := sess("a", 0, 1)
		dead.Alive = false
		h.SetSessions(nil, []wire.SessionInfo{dead})
		h.Open()
		wantCalls(t, h, fh)
	})
	t.Run("no daemon", func(t *testing.T) {
		h, _, _, fh, _ := newHandyHUD(t)
		h.Disconnected()
		h.Open()
		wantCalls(t, h, fh)
	})
}

// Finding 4: quitting the menu bar with the HUD open cancels Handy, and
// waits for it.
func TestHUDShutdownCancelsHandyItStarted(t *testing.T) {
	h, _, _, fh, _ := newHandyHUD(t)
	h.Open()
	drain(h)
	h.Shutdown()
	if got := fh.got(); !slices.Equal(got, []string{"toggle", "cancel"}) {
		t.Fatalf("Shutdown left %v; it must cancel before returning", got)
	}
	h.Cancel()
	wantCalls(t, h, fh, "toggle", "cancel") // nothing left to cancel

	h2, _, _, fh2, _ := newHandyHUD(t)
	h2.SetHandyAutoStart(false)
	h2.Open()
	h2.Shutdown()
	wantCalls(t, h2, fh2)
}

// Review of PR 574: a field emptied while Enter waits sends nothing, and
// the panel must leave the waiting state it showed rather than keep it.
func TestHUDEmptiedWhileAwaitingRendersEditing(t *testing.T) {
	h, v, s, _, ft := newHandyHUD(t)
	h.Open()
	h.Submit("draft")
	h.TextChanged("", false)
	ft.fire(t)
	if len(s.sends) != 0 {
		t.Fatalf("an empty field was sent: %+v", s.sends)
	}
	if st := v.last(); st.Status == statusAwaiting || st.Hints == hintsAwait || !st.CanSend {
		t.Fatalf("panel still shows the wait: %+v", st)
	}
}

// Review round 2, finding 2: queuing never blocks, so a hung Handy can
// never back up into the HUD's lock and the main thread.
func TestSerialWorkerNeverBlocksTheCaller(t *testing.T) {
	run := serialWorker()
	release := make(chan struct{})
	var mu sync.Mutex
	var order []int
	run(func() { <-release })
	done := make(chan struct{})
	go func() {
		for i := range 500 {
			run(func() {
				mu.Lock()
				order = append(order, i)
				mu.Unlock()
			})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("queuing blocked behind a stuck call")
	}
	close(release)
	finished := make(chan struct{})
	run(func() { close(finished) })
	<-finished
	if len(order) != 500 || !slices.IsSorted(order) {
		t.Fatalf("ran %d calls, sorted=%v; want all 500 in order", len(order), slices.IsSorted(order))
	}
}

// Finding 1: the quit-time cancel queues behind a start still in flight,
// so it lands after it, and Shutdown waits for it.
func TestHUDShutdownCancelsAfterQueuedStart(t *testing.T) {
	h, _, _ := newTestHUD(sess("a", 0, 1))
	bh := &blockingHandy{fakeHandy: fakeHandy{available: true}, release: make(chan struct{})}
	h.handy = bh
	h.SetHandyAutoStart(true)
	h.Open()
	done := make(chan struct{})
	go func() {
		h.Shutdown()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("Shutdown returned before its cancel ran")
	case <-time.After(50 * time.Millisecond):
	}
	close(bh.release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown never returned")
	}
	if got := bh.got(); !slices.Equal(got, []string{"toggle", "cancel"}) {
		t.Fatalf("Handy calls ran as %v, want toggle then cancel", got)
	}
}

func TestHUDShutdownGivesUpOnAHungHandy(t *testing.T) {
	h, _, _ := newTestHUD(sess("a", 0, 1))
	bh := &blockingHandy{fakeHandy: fakeHandy{available: true}, release: make(chan struct{})}
	defer close(bh.release)
	h.handy = bh
	h.shutdownWait = 50 * time.Millisecond
	h.SetHandyAutoStart(true)
	h.Open()
	start := time.Now()
	h.Shutdown()
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Shutdown waited %v on a hung Handy", d)
	}
}

// Finding 3: one stray key while waiting for Handy's text sends nothing.
func TestHUDOneKeystrokeWhileAwaitingDoesNotSend(t *testing.T) {
	h, _, s, _, ft := newHandyHUD(t)
	h.Open()
	h.Submit("draft")
	h.TextChanged("draftx", false)
	for _, tm := range ft.pending {
		if !tm.stopped && tm.d == handyQuiet {
			t.Fatal("one keystroke armed the send")
		}
	}
	h.TextChanged("draftx and the transcription", true)
	ft.fire(t)
	if len(s.sends) != 1 || s.sends[0].text != "draftx and the transcription" {
		t.Fatalf("sends = %+v", s.sends)
	}
}

// Finding 4: a close-time cancel that fails twice leaves a recording
// nothing tracks; the next opening must not toggle it and says why.
func TestHUDFailedCloseCancelWarnsNextOpen(t *testing.T) {
	h, v, _, fh, _ := newHandyHUD(t)
	fh.cancelErr = errors.New("boom")
	h.Open()
	h.Cancel()
	wantCalls(t, h, fh, "toggle", "cancel", "cancel")
	h.Open()
	wantCalls(t, h, fh, "toggle", "cancel", "cancel")
	if v.last().Status != statusOrphaned {
		t.Fatalf("status = %q, want the orphaned-recording warning", v.last().Status)
	}
	h.Cancel() // nothing started this time, so nothing to cancel
	fh.cancelErr = nil
	h.Open() // warned once; back to normal
	wantCalls(t, h, fh, "toggle", "cancel", "cancel", "toggle")
}

// A Handy that quit is not an orphan: there is no recording to cancel.
func TestHUDCancelOnStoppedHandyIsNotOrphan(t *testing.T) {
	h, v, _, fh, _ := newHandyHUD(t)
	fh.cancelErr = errHandyNotRunning
	h.Open()
	h.Cancel()
	wantCalls(t, h, fh, "toggle", "cancel")
	h.Open()
	if v.last().Status == statusOrphaned {
		t.Fatal("a stopped Handy was reported as still recording")
	}
}

// Finding 5: the target ending while the stop is in flight assumed it
// worked; when it fails, the HUD is back to recording, so ↩ stops Handy
// again rather than sending while it may still listen.
func TestHUDStopFailureAfterTargetLossRestoresRecording(t *testing.T) {
	h, v, s, fh, _ := newHandyHUD(t)
	var held []func()
	h.goHandy = func(f func()) { held = append(held, f) }
	h.SetSessions([]wire.ProjectInfo{{ID: "p", Name: "proj"}}, []wire.SessionInfo{sess("a", 0, 1), sess("b", 1, 0)})
	h.Open()
	h.Submit("")
	gone := sess("a", 0, 1)
	gone.Alive = false
	h.SetSessions([]wire.ProjectInfo{{ID: "p", Name: "proj"}}, []wire.SessionInfo{gone, sess("b", 1, 0)})
	held[0]() // the start succeeds
	fh.toggleErr = errors.New("boom")
	held[1]() // the stop fails
	if st := v.last(); st.Status != statusStopFailed || st.Hints != hintsRecord {
		t.Fatalf("after the failed stop = %q / %q", st.Status, st.Hints)
	}
	h.Move(1)
	h.Submit("words")
	if len(s.sends) != 0 {
		t.Fatalf("sent while Handy may still be recording: %+v", s.sends)
	}
	if len(held) != 3 {
		t.Fatalf("↩ queued %d Handy calls in all, want a second stop", len(held))
	}
}
