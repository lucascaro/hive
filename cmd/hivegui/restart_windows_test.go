//go:build windows

package main

import (
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

func TestKillRunningHived_NoPidfile(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "hived.sock")
	if err := killRunningHived(sock); err != nil {
		t.Fatalf("killRunningHived with missing pidfile: %v", err)
	}
}

func TestKillRunningHived_StalePidfileRemoved(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "hived.sock")
	pidPath := sock + ".pid"
	// PID 4 is the System process on Windows: alive but definitely not hived.
	// killRunningHived must NOT terminate it and MUST remove the stale pidfile.
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(4)), 0o600); err != nil {
		t.Fatalf("write stale pidfile: %v", err)
	}
	if err := killRunningHived(sock); err != nil {
		t.Fatalf("killRunningHived with stale pidfile: %v", err)
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("stale pidfile not removed: stat err = %v", err)
	}
}
