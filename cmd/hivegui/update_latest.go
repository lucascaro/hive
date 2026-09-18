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
	"github.com/lucascaro/hive/internal/proc"
)

// runGitFn is a package-level seam so tests can drive the branches
// below without a real repository, mirroring looksLikeHivedFn in
// restart_unix.go. Production always uses runGit.
var runGitFn = runGit

// gitCommandFn, when set, picks the git binary and the environment it
// runs with. macOS resolves both from the login-shell PATH
// (shell_env_darwin.go): an app opened from Spotlight or Finder otherwise
// finds /usr/bin/git — Apple's xcrun shim, which refuses to run until
// the Xcode license is accepted — even when the user's terminal has a
// working git earlier on its PATH. The binary has to be resolved here
// rather than left to cmd.Env, because exec resolves a bare name against
// the process PATH. Nil elsewhere: "git" on the process environment.
var gitCommandFn func() (bin string, env []string)

// gitTimeout bounds every git invocation. `fetch` talks to the network,
// so this is generous; the rest return in milliseconds.
var gitTimeout = 60 * time.Second

// runGit runs one git command in dir and returns its trimmed stdout.
// Stderr is folded into the error so a failure says *why* rather than
// just "exit status 128".
func runGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	bin, env := "git", []string(nil)
	if gitCommandFn != nil {
		bin, env = gitCommandFn()
	}
	cmd := proc.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = env
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
		if !isNoUpstreamErr(err) {
			// git itself failed — missing, or refusing to run until the
			// Xcode license is accepted. Reporting that as "no upstream"
			// sent the user hunting for a branch problem that wasn't there.
			return info, err
		}
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

// isNoUpstreamErr reports whether a failed `rev-parse @{upstream}` failed
// because there is nothing to track: a branch with no upstream, or a
// detached HEAD. runGit folds git's stderr into the error, which is what
// makes this distinguishable from git not running at all.
func isNoUpstreamErr(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "no upstream configured") ||
		strings.Contains(msg, "HEAD does not point to a branch")
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
