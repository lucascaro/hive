//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestParseTasklistRow(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantAlive bool
		wantHived bool
	}{
		{
			name:      "hived match",
			in:        `"hived.exe","1234","Console","1","12,345 K"`,
			wantAlive: true,
			wantHived: true,
		},
		{
			name:      "other process",
			in:        `"notepad.exe","5678","Console","1","8,000 K"`,
			wantAlive: true,
			wantHived: false,
		},
		{
			name:      "no match info line",
			in:        "INFO: No tasks are running which match the specified criteria.",
			wantAlive: false,
			wantHived: false,
		},
		{
			name:      "empty",
			in:        "",
			wantAlive: false,
			wantHived: false,
		},
		{
			name:      "case-insensitive image",
			in:        `"Hived.EXE","42","Services","0","4,000 K"`,
			wantAlive: true,
			wantHived: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			alive, hived := parseTasklistRow(tc.in)
			if alive != tc.wantAlive || hived != tc.wantHived {
				t.Fatalf("parseTasklistRow(%q) = (%v,%v), want (%v,%v)",
					tc.in, alive, hived, tc.wantAlive, tc.wantHived)
			}
		})
	}
}

// stubProbe swaps probeFn for the duration of a test. Restoring it in
// a Cleanup keeps tests independent of evaluation order.
func stubProbe(t *testing.T, fn func(int) (bool, bool, error)) {
	t.Helper()
	prev := probeFn
	probeFn = fn
	t.Cleanup(func() { probeFn = prev })
}

func TestKillRunningHived_NoPidfile(t *testing.T) {
	stubProbe(t, func(int) (bool, bool, error) {
		t.Fatal("probe must not be called when no pidfile exists")
		return false, false, nil
	})
	dir := t.TempDir()
	sock := filepath.Join(dir, "hived.sock")
	if err := killRunningHived(sock); err != nil {
		t.Fatalf("killRunningHived with missing pidfile: %v", err)
	}
}

func TestKillRunningHived_StalePidfileRemoved(t *testing.T) {
	stubProbe(t, func(pid int) (bool, bool, error) {
		// Recycled pid: alive but not hived.exe.
		return true, false, nil
	})
	dir := t.TempDir()
	sock := filepath.Join(dir, "hived.sock")
	pidPath := sock + ".pid"
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(99999)), 0o600); err != nil {
		t.Fatalf("write stale pidfile: %v", err)
	}
	if err := killRunningHived(sock); err != nil {
		t.Fatalf("killRunningHived with stale pidfile: %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("stale pidfile not removed: stat err = %v", err)
	}
}

func TestKillRunningHived_DeadPidNoError(t *testing.T) {
	stubProbe(t, func(int) (bool, bool, error) {
		return false, false, nil // pid no longer exists
	})
	dir := t.TempDir()
	sock := filepath.Join(dir, "hived.sock")
	if err := os.WriteFile(sock+".pid", []byte("12345"), 0o600); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	if err := killRunningHived(sock); err != nil {
		t.Fatalf("killRunningHived for already-dead pid: %v", err)
	}
}

func TestKillRunningHived_ProbeFailurePropagates(t *testing.T) {
	wantErr := errors.New("tasklist exec failed")
	stubProbe(t, func(int) (bool, bool, error) {
		return false, false, wantErr
	})
	dir := t.TempDir()
	sock := filepath.Join(dir, "hived.sock")
	if err := os.WriteFile(sock+".pid", []byte("12345"), 0o600); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	err := killRunningHived(sock)
	if err == nil {
		t.Fatal("expected probe failure to propagate, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("err chain missing probe failure: %v", err)
	}
}

func TestKillRunningHived_InvalidPid(t *testing.T) {
	stubProbe(t, func(int) (bool, bool, error) {
		t.Fatal("probe must not be called for invalid pid")
		return false, false, nil
	})
	dir := t.TempDir()
	sock := filepath.Join(dir, "hived.sock")
	if err := os.WriteFile(sock+".pid", []byte("not-a-number"), 0o600); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	if err := killRunningHived(sock); err == nil {
		t.Fatal("expected error for malformed pidfile, got nil")
	}
}
