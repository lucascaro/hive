// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// The timeout lives in one wrapper over the real exec so it can be proven
// against a real slow command; a fake runner could never catch it being
// removed.
func TestRunWithTimeoutKillsSlowCommand(t *testing.T) {
	start := time.Now()
	_, err := runWithTimeout(50*time.Millisecond, "sleep", "5")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a deadline error", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v; the command was not killed at the deadline", d)
	}
}

func TestRunWithTimeoutReturnsStdoutAndStderr(t *testing.T) {
	out, err := runWithTimeout(5*time.Second, "sh", "-c", "echo hello")
	if err != nil || out != "hello\n" {
		t.Errorf("out=%q err=%v", out, err)
	}
	_, err = runWithTimeout(5*time.Second, "sh", "-c", "echo broke >&2; exit 1")
	if err == nil || err.Error() != "broke" {
		t.Errorf("err = %v, want the stderr text", err)
	}
}

func TestRelaunchCommandUsesNewInstanceFlag(t *testing.T) {
	got := relaunchArgs("/Applications/Hive.app/Contents/Library/LoginItems/hivebar.app")
	want := []string{"-n", "-a", "/Applications/Hive.app/Contents/Library/LoginItems/hivebar.app", "--args", "-restart"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

// Quitting after a failed relaunch would leave no menu bar until hived or
// hivegui next respawn one.
func TestRestartQuitsOnlyWhenSpawnSucceeds(t *testing.T) {
	var quits, alerts int
	quit := func() { quits++ }
	notify := func(string, string) { alerts++ }

	ok := &fakeRun{fn: func(string, []string) (string, error) { return "", nil }}
	if err := restartMenuBar(ok.run, "/x/hivebar.app", quit, notify); err != nil || quits != 1 || alerts != 0 {
		t.Fatalf("success: err=%v quits=%d alerts=%d", err, quits, alerts)
	}
	if calls := ok.got(); len(calls) != 1 || calls[0] != "open -n -a /x/hivebar.app --args -restart" {
		t.Errorf("calls = %v", calls)
	}

	bad := &fakeRun{fn: func(string, []string) (string, error) { return "", errors.New("open failed") }}
	if err := restartMenuBar(bad.run, "/x/hivebar.app", quit, notify); err == nil || quits != 1 || alerts != 1 {
		t.Errorf("failure: err=%v quits=%d (want still 1) alerts=%d (want 1)", err, quits, alerts)
	}
	if err := restartMenuBar(ok.run, "", quit, notify); err == nil || quits != 1 || alerts != 2 {
		t.Errorf("no bundle: err=%v quits=%d alerts=%d", err, quits, alerts)
	}
}

// Killing the child at the deadline is not enough if a grandchild inherited
// the stdout pipe: cmd.Run then waits for the pipe to close. An older hivegui
// that ignores --menubar-login-item and starts a whole GUI is that case.
func TestRunWithTimeoutIsBoundedWhenAGrandchildHoldsThePipe(t *testing.T) {
	start := time.Now()
	_, err := runWithTimeout(50*time.Millisecond, "sh", "-c", "sleep 5 & sleep 5")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a deadline error", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %v; a grandchild holding the pipe stalled the caller past the deadline", d)
	}
}
