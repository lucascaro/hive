// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import (
	"sync/atomic"
	"testing"
	"time"
)

func testMenu(run runner) *Menu {
	m := NewMenu()
	m.run = run
	m.gui = "/x/hivegui"
	m.app = "/x/hivebar.app"
	m.notify = func(string, string) {}
	return m
}

// A slow toggle (an exec with a 5 s deadline, an open alert) must not stop
// Quit Menu Bar from being heard: each click is handled on its own goroutine.
func TestSlowToggleDoesNotBlockFooterLoop(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	m := testMenu((&fakeRun{fn: func(string, []string) (string, error) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return sentinel("enabled"), nil
	}}).run)
	defer close(release)
	m.loginStatus = "not-registered"
	var quit atomic.Bool
	m.quitFn = func() { quit.Store(true) }

	returned := make(chan struct{})
	go func() { m.dispatch(clickLoginItem); close(returned) }()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("dispatch blocked on a slow handler; the footer loop would stop hearing clicks")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("toggle never ran")
	}
	m.dispatch(clickQuitBar)

	deadline := time.Now().Add(time.Second)
	for !quit.Load() {
		if time.Now().After(deadline) {
			t.Fatal("Quit Menu Bar was not handled while a toggle was in flight")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSecondToggleClickIsDroppedWhileOneIsInFlight(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	m := testMenu((&fakeRun{fn: func(string, []string) (string, error) {
		calls.Add(1)
		<-release
		return sentinel("enabled"), nil
	}}).run)
	m.loginStatus = "not-registered"

	done := make(chan struct{}, 2)
	go func() { m.toggleLogin(); done <- struct{}{} }()
	for calls.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	m.toggleLogin() // returns at once: the first is still running
	close(release)
	<-done
	if n := calls.Load(); n != 1 {
		t.Errorf("exec ran %d times, want 1", n)
	}
}

// After a failed toggle the item must be usable again BEFORE the alert is
// dismissed: display dialog blocks until OK.
func TestToggleFailureAlertsAfterReleasingTheFlag(t *testing.T) {
	m := testMenu((&fakeRun{fn: func(_ string, args []string) (string, error) {
		if args[0] == loginItemFlag+"enable" {
			return "", errFake("Invalid argument")
		}
		return sentinel("not-registered"), nil
	}}).run)
	m.loginStatus = "not-registered"
	m.notify = func(title, body string) {
		m.mu.Lock()
		held := m.toggling
		m.mu.Unlock()
		if held {
			t.Error("toggling still set while the alert is up; the item is locked behind the dialog")
		}
		if body != "Invalid argument" {
			t.Errorf("alert body = %q", body)
		}
	}
	m.toggleLogin()
}

func TestNoAlertOnceQuitting(t *testing.T) {
	m := testMenu((&fakeRun{fn: func(string, []string) (string, error) { return "", nil }}).run)
	called := false
	m.notify = func(string, string) { called = true }
	m.quitFn = func() {}
	m.quitApp()
	m.alert("t", "b")
	if called {
		t.Error("alert shown after quit began; osascript would be orphaned")
	}
}

// systray's open signal is a non-blocking send, so a probe that is already
// running must not swallow a request: it queues exactly one more.
func TestOpenSignalDuringProbeTriggersOneMoreProbe(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 8)
	var probes atomic.Int32
	m := testMenu((&fakeRun{fn: func(string, []string) (string, error) {
		probes.Add(1)
		entered <- struct{}{}
		<-release
		return sentinel("enabled"), nil
	}}).run)
	go m.probeWorker()
	defer close(m.probeReq)

	m.requestProbe()
	<-entered
	for i := 0; i < 5; i++ {
		m.requestProbe() // all mid-probe; must coalesce into one
	}
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for probes.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond) // a wrong implementation would keep going
	if n := probes.Load(); n != 2 {
		t.Errorf("probes = %d, want exactly 2 (the running one plus one queued)", n)
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }
