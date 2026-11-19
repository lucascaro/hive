// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package menubar

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
)

// holdLock writes record into a lock file and, if held, keeps an
// exclusive flock on it the way a running hivebar does.
func holdLock(t *testing.T, record string, held bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), LockFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if _, err := f.WriteString(record); err != nil {
		t.Fatal(err)
	}
	if held {
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestStaleHolder(t *testing.T) {
	cases := []struct {
		name   string
		record string
		held   bool
		stale  bool
	}{
		{"different build is replaced", FormatLockRecord(123, "old"), true, true},
		{"pre-build-id record is replaced", "123\n", true, true},
		{"same build is left alone", FormatLockRecord(123, "new"), true, false},
		// Liveness is isHivebar's job, not the lock's: staleHolder must
		// not take even a shared flock (see its doc comment).
		{"unheld lock still reports the record", FormatLockRecord(123, "old"), false, true},
		{"unparseable record is left alone", "junk", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := holdLock(t, tc.record, tc.held)
			pid, stale := staleHolder(path, "new")
			if stale != tc.stale {
				t.Fatalf("stale = %v, want %v", stale, tc.stale)
			}
			if stale && pid != 123 {
				t.Fatalf("pid = %d, want 123", pid)
			}
		})
	}
	if _, stale := staleHolder(filepath.Join(t.TempDir(), "missing"), "new"); stale {
		t.Error("missing lock file reported stale")
	}
}

// isHivebar is what stands between a stale lock record and SIGTERM to an
// arbitrary process.
func TestIsHivebarRejectsOtherProcesses(t *testing.T) {
	if isHivebar(os.Getpid()) {
		t.Error("the test binary was taken for hivebar")
	}
	if isHivebar(1) {
		t.Error("launchd was taken for hivebar")
	}
}

func TestIsHivebarAcceptsHivebar(t *testing.T) {
	src, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Skip(err)
	}
	bin := filepath.Join(t.TempDir(), "hivebar")
	if err := os.WriteFile(bin, src, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := os.StartProcess(bin, []string{bin, "30"}, &os.ProcAttr{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Kill(); _, _ = p.Wait() })
	if !isHivebar(p.Pid) {
		t.Error("a process named hivebar was not recognised")
	}
}

func TestOpenArgs(t *testing.T) {
	if got := openArgs("/A/hivebar.app", false); !slices.Equal(got, []string{"-a", "/A/hivebar.app"}) {
		t.Errorf("plain start = %v", got)
	}
	want := []string{"-n", "-a", "/A/hivebar.app", "--args", RestartFlag}
	if got := openArgs("/A/hivebar.app", true); !slices.Equal(got, want) {
		t.Errorf("replacement = %v, want %v", got, want)
	}
}

// The old menu bar is stopped only after its replacement launched, and
// only when there is one to replace. Killing after a failed `open` would
// leave no menu bar at all.
func TestAfterOpenKillsOnlyAfterSuccessfulReplace(t *testing.T) {
	cases := []struct {
		name    string
		openErr error
		replace bool
		kill    bool
	}{
		{"replace, open ok", nil, true, true},
		{"replace, open failed", errors.New("open: exit 1"), true, false},
		{"plain start", nil, false, false},
		{"plain start, open failed", errors.New("open: exit 1"), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			kill := func(pid int, sig syscall.Signal) error {
				calls = append(calls, fmt.Sprintf("%d/%v", pid, sig))
				return nil
			}
			afterOpen(tc.openErr, tc.replace, 4242, kill)
			want := []string(nil)
			if tc.kill {
				want = []string{fmt.Sprintf("4242/%v", syscall.SIGTERM)}
			}
			if !slices.Equal(calls, want) {
				t.Errorf("kill calls = %v, want %v", calls, want)
			}
		})
	}
}
