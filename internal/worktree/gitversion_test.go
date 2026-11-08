// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package worktree

import (
	"errors"
	"testing"
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
