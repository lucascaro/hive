package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/buildinfo"
)

// runGitFn is a package-level seam so tests can drive the branches
// below without a real repository, mirroring looksLikeHivedFn in
// restart_unix.go. Production always uses runGit.
var runGitFn = runGit

// gitTimeout bounds every git invocation. `fetch` talks to the network,
// so this is generous; the rest return in milliseconds.
var gitTimeout = 60 * time.Second

// runGit runs one git command in dir and returns its trimmed stdout.
// Stderr is folded into the error so a failure says *why* rather than
// just "exit status 128".
func runGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// checkLatest reports whether the checkout's upstream branch carries a
// commit the running binary does not.
//
// The comparison is against the *running build*, not against HEAD,
// because "pulled but never rebuilt" is the common state on this
// channel and leaves the user on stale binaries with a clean, up to
// date tree. When the build id can't be located in the repo (a "dev"
// build, a dirty build, a commit that was force-pushed away) we fall
// back to comparing HEAD, which is the best signal left.
func checkLatest(repo string) (UpdateInfo, error) {
	info := UpdateInfo{
		Channel: ChannelLatest,
		Current: buildinfo.BuildID(),
		Stage:   StageIdle,
	}

	upstream, err := runGitFn(repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		// No upstream is a configuration state, not a failure: say so
		// and skip rather than raising an error banner every 6 hours.
		info.Skipped = true
		info.Message = "checkout has no upstream branch to track"
		return info, nil
	}

	// Fetch before comparing, or the answer is however stale the last
	// manual fetch was.
	if _, err := runGitFn(repo, "fetch", "--quiet"); err != nil {
		return info, err
	}

	latest, err := runGitFn(repo, "rev-parse", "--short", upstream)
	if err != nil {
		return info, err
	}
	info.Latest = latest

	base := strings.TrimSuffix(info.Current, "-dirty")
	if base == "" || base == "dev" || !isHex(base) {
		base = "HEAD"
	} else if _, err := runGitFn(repo, "cat-file", "-e", base+"^{commit}"); err != nil {
		// The running build's commit isn't in this repo — different
		// checkout, or history rewritten. HEAD is the honest fallback.
		base = "HEAD"
	}

	out, err := runGitFn(repo, "rev-list", "--count", base+".."+upstream)
	if err != nil {
		return info, err
	}
	n, err := strconv.Atoi(out)
	if err != nil {
		return info, fmt.Errorf("git rev-list --count returned %q", out)
	}
	if n > 0 {
		info.Available = true
		info.Stage = StageAvailable
		info.Message = fmt.Sprintf("%d commit(s) behind %s", n, upstream)
	}
	return info, nil
}

func isHex(s string) bool {
	if len(s) < 7 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
