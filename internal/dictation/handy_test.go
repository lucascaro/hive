// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package dictation

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"testing"
	"time"
)

// Spec 575: the paths Handy 0.10.1's two Windows installers use.
func TestHandyPathsWindows(t *testing.T) {
	env := map[string]string{
		"LOCALAPPDATA": `C:\Users\u\AppData\Local`,
		"ProgramFiles": `C:\Program Files\`,
	}
	got := windowsHandyPaths(func(k string) string { return env[k] })
	want := []string{`C:\Users\u\AppData\Local\Handy\handy.exe`, `C:\Program Files\Handy\handy.exe`}
	if !slices.Equal(got, want) {
		t.Fatalf("paths = %q, want %q", got, want)
	}
	if got := windowsHandyPaths(func(string) string { return "" }); len(got) != 0 {
		t.Fatalf("paths with no environment = %q, want none", got)
	}
}

func TestHandyPathsDarwin(t *testing.T) {
	got := darwinHandyPaths("/Users/u")
	want := []string{"/Applications/Handy.app/Contents/MacOS/handy", "/Users/u/Applications/Handy.app/Contents/MacOS/handy"}
	if !slices.Equal(got, want) {
		t.Fatalf("paths = %q, want %q", got, want)
	}
}

// A Handy that hangs must not stall the Handy worker: the deadline kills
// it, and WaitDelay stops a grandchild holding the pipes from keeping
// Run open past it.
func TestRunWithTimeoutKillsAHungCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	start := time.Now()
	_, err := RunWithTimeout(100*time.Millisecond, "sh", "-c", "sleep 5 & sleep 5")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("returned after %s; the deadline and WaitDelay should bound it near 600ms", d)
	}
}

func TestRunWithTimeoutReportsStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	if _, err := RunWithTimeout(5*time.Second, "sh", "-c", "echo broke >&2; exit 1"); err == nil || err.Error() != "broke" {
		t.Fatalf("err = %v, want the command's stderr", err)
	}
}
