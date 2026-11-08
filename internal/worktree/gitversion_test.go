// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package worktree

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCheckGitVersion(t *testing.T) {
	for _, tc := range []struct {
		maj, min int
		bad      bool
	}{
		{2, 39, false}, {2, 53, false}, {2, 54, true}, {2, 55, false}, {2, 60, false}, {3, 0, false},
	} {
		if err := checkGitVersion(tc.maj, tc.min); tc.bad != errors.Is(err, ErrGitUnsupported) {
			t.Errorf("git %d.%d: err = %v, want refusal=%v", tc.maj, tc.min, err, tc.bad)
		}
	}
}

// The git on this machine is one the workflow paths accept (CI pins it).
func TestInstalledGitIsSupported(t *testing.T) {
	skipNoGit(t)
	if err := gitSupported(); err != nil {
		t.Errorf("installed git refused: %v", err)
	}
}

// A cancelled caller context bounds the probe and the failure is not cached.
func TestGitSupported_CancelledContextNotCached(t *testing.T) {
	skipNoGit(t)
	gitProbeMu.Lock()
	done, perr := gitProbeDone, gitProbeErr
	gitProbeDone = false
	gitProbeMu.Unlock()
	t.Cleanup(func() { gitProbeMu.Lock(); gitProbeDone, gitProbeErr = done, perr; gitProbeMu.Unlock() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := gitSupportedCtx(ctx); err == nil {
		t.Fatal("cancelled context: want probe error")
	}
	if time.Since(start) > gitProbeTimeout {
		t.Error("probe not bounded by context")
	}
	if err := gitSupported(); err != nil {
		t.Errorf("failure was cached: %v", err)
	}
}

func TestCheckGitVersionOutput(t *testing.T) {
	for out, bad := range map[string]bool{
		"git version 2.53.0":                 false,
		"git version 2.54.1":                 true,
		"git version 2.39.5 (Apple Git-154)": false,
		"git version 2.55.0.windows.1":       false,
		"git version nonsense":               true,
		"":                                   true,
	} {
		if err := checkGitVersionOutput(out); bad != errors.Is(err, ErrGitUnsupported) {
			t.Errorf("%q: err = %v, want refusal=%v", out, err, bad)
		}
	}
}
