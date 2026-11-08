// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package worktree

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/lucascaro/hive/internal/proc"
)

// ErrGitUnsupported: the installed git is one whose hook behaviour the
// workflow paths cannot prove they suppress.
var ErrGitUnsupported = errors.New("worktree: this git version is not supported for workflow worktrees")

// Config-defined hooks (hook.<name>.command) arrived in git 2.54 and run
// beside the hooks directory, so core.hooksPath alone does not stop them.
// safeGit disables them per event with hook.<event>.enabled=false, which
// is verified (TestSafeGit_ConfigHooksDoNotRun) on 2.55. Before 2.54
// there are no config hooks to suppress; 2.54 is the one release in
// between whose event-level disable is unproven, so it is refused.
func checkGitVersion(major, minor int) error {
	if major == 2 && minor == 54 {
		return fmt.Errorf("%w: git 2.54 runs config-defined hooks that cannot be turned off here; use git 2.55 or newer (or 2.53 or older)", ErrGitUnsupported)
	}
	return nil
}

var gitVersionRe = regexp.MustCompile(`(\d+)\.(\d+)`)

// gitSupported probes `git version` once per process.
var gitSupported = sync.OnceValue(func() error {
	out, err := proc.Command("git", "version").Output()
	if err != nil {
		return fmt.Errorf("git version: %w", err)
	}
	m := gitVersionRe.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return nil // unparseable (a vendor build): do not block
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return checkGitVersion(major, minor)
})

func requireSupportedGit(_ context.Context) error { return gitSupported() }
