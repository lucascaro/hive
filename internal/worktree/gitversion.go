// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

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

var gitVersionRe = regexp.MustCompile(`^git version (\d+)\.(\d+)(?:[.\s]|$)`)

// gitProbeTimeout bounds `git version`: a wedged git wrapper must not hold
// gitMu (and with it daemon shutdown) forever.
const gitProbeTimeout = 10 * time.Second

var (
	gitProbeMu   sync.Mutex
	gitProbeDone bool
	gitProbeErr  error
	gitProbeKey  string // identity of the git binary the cached answer is for
)

// gitSupported probes `git version`. A definitive answer is cached; a
// probe failure (timeout, missing git) is not, so a later call retries.
func gitSupported() error {
	return gitSupportedCtx(context.Background())
}

func gitSupportedCtx(ctx context.Context) error {
	gitProbeMu.Lock()
	defer gitProbeMu.Unlock()
	key := gitBinaryKey()
	if gitProbeDone && gitProbeKey == key {
		return gitProbeErr
	}
	ctx, cancel := context.WithTimeout(ctx, gitProbeTimeout)
	defer cancel()
	cmd := proc.CommandContext(ctx, "git", "version")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git version: %w", err)
	}
	gitProbeDone, gitProbeKey = true, key
	gitProbeErr = checkGitVersionOutput(string(out))
	return gitProbeErr
}

// gitBinaryKey identifies the git on PATH (path, size, mtime) so an
// upgrade or PATH change while the daemon runs re-runs the probe.
func gitBinaryKey() string {
	path, err := exec.LookPath("git")
	if err != nil {
		return ""
	}
	fi, err := os.Stat(path)
	if err != nil {
		return path
	}
	return fmt.Sprintf("%s|%d|%d", path, fi.Size(), fi.ModTime().UnixNano())
}

// checkGitVersionOutput judges `git version` output; anything but a
// plain "git version X.Y..." line is refused (fail closed): a wrapper's
// banner could hide the unsupported release.
func checkGitVersionOutput(out string) error {
	out = strings.TrimSpace(out)
	m := gitVersionRe.FindStringSubmatch(out)
	if m == nil {
		return fmt.Errorf("%w: cannot parse %q", ErrGitUnsupported, out)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return checkGitVersion(major, minor)
}

func requireSupportedGit(ctx context.Context) error { return gitSupportedCtx(ctx) }

// EnsureSupportedGit warms the version probe. Callers run it before
// taking the repo's git lock, so a stalled probe never holds that lock.
func EnsureSupportedGit(ctx context.Context) error { return gitSupportedCtx(ctx) }
