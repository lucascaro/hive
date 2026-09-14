//go:build darwin || windows

package main

// The release-download half of the update staging path.
//
// This is only reachable where an in-app update can install one, which is
// macOS and Windows. Linux ships no release artifact (see
// update_apply_other.go), so nothing there references any of it: left in
// the untagged file it compiled on the Linux leg with no caller at all,
// which is a staticcheck U1000 failure.
//
// The genuinely portable helpers - the staging directory layout and the
// checksum manifest - stay in update_apply_common.go, where the Linux leg
// still compiles and tests them.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// maxDownloadBytes caps what we will pull from a release asset. The
// macOS zip is tens of megabytes; this is a sanity bound so a wrong or
// hostile Content-Length can't fill the user's disk. Var, like the two
// timeouts below, so a test can shrink it — asserting the cap by
// actually serving half a gigabyte is not a test anyone will keep.
var maxDownloadBytes int64 = 512 << 20

// downloadTimeout bounds a whole staging download.
var downloadTimeout = 15 * time.Minute

// buildTimeout bounds a latest-channel `./build.sh`. A cold universal
// Wails build is minutes, not seconds.
var buildTimeout = 30 * time.Minute

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

func fetchReleaseAssets(ctx context.Context) ([]releaseAsset, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateReleasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases: HTTP %d", resp.StatusCode)
	}
	var rel struct {
		Assets []releaseAsset `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	return rel.Assets, nil
}

// assetURL finds one asset by name and enforces the same URL allowlist
// the banner's Download button uses. A download URL pointing anywhere
// other than this repo's releases is refused outright: unlike the
// browser hand-off, what comes back here gets unpacked and executed.
func assetURL(assets []releaseAsset, name string) (string, error) {
	for _, a := range assets {
		if a.Name != name {
			continue
		}
		if !strings.HasPrefix(a.URL, updateURLPrefix) {
			return "", fmt.Errorf("release asset %s has an unexpected download URL", name)
		}
		return a.URL, nil
	}
	return "", fmt.Errorf("release has no %s asset", name)
}

func download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", filepath.Base(dest), resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	// +1 so a body that is exactly at the cap is still detected as over
	// it rather than silently truncated into a checksum failure.
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return fmt.Errorf("download %s: %w", filepath.Base(dest), err)
	}
	if n > maxDownloadBytes {
		return fmt.Errorf("download %s: larger than %d bytes", filepath.Base(dest), maxDownloadBytes)
	}
	return f.Close()
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// preflightCheckout is every refusal stageLatest can make before the
// checkout moves or the build starts, in the order they are cheapest
// to explain. A build is minutes long, and a failure the updater could
// have predicted from the start should not cost the user those minutes
// with the button stuck on "Updating…" — nor should it surface as raw
// git stderr with the command line pasted in front of it.
//
// A dirty tree is refused first: `git pull` on top of uncommitted work
// is how you lose it, and this button is meant to be safe to press
// without thinking. A detached HEAD has no upstream to fast-forward
// from. The remote is pinned before anything is fetched, pulled or
// executed. And a branch that has wandered off its upstream cannot
// fast-forward at all, which git reports only after the fetch, in its
// own words — so that check comes last and is the only one that
// fetches.
func preflightCheckout(repo string) error {
	dirty, err := runGitFn(repo, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(dirty) != "" {
		return fmt.Errorf("%s has uncommitted changes — commit or stash them first", repo)
	}
	branch, err := runGitFn(repo, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return fmt.Errorf("%s has a detached HEAD — check out a branch first", repo)
	}
	upstream, err := verifyUpstreamRemote(repo)
	if err != nil {
		return err
	}
	return verifyFastForwardable(repo, branch, upstream)
}

// verifyFastForwardable refuses a truly diverged branch: one that is both
// ahead of and behind its upstream, so `git pull --ff-only` cannot land the
// update. A branch that is only ahead is fine — the pull is a no-op there
// and the updater builds HEAD as-is. A branch that is only behind is also
// fine; the pull fast-forwards normally.
//
// The common way to end up diverged is an integration or feature branch
// left checked out with its upstream still set to main: local commits pile
// up while upstream also moves. The fix depends on whether the branch is
// itself the tracked branch (e.g. main tracking origin/main) — there is no
// "other" branch to check out, so the advice is to push or move the local
// commits — or a differently named branch, where checking out the branch
// it tracks is the way out.
//
// The counts come from the remote-tracking ref, which is only as fresh
// as the last fetch — checkLatest's, up to updateCheckInterval ago.
// `pull --ff-only` fetches before it decides, so this fetches first
// too; otherwise "only ahead" here can be "diverged" by the time the
// pull looks, and the banner shows git's words after all. A fetch
// touches nothing but remote-tracking refs, and the remote it talks
// to is the one verifyUpstreamRemote just pinned.
func verifyFastForwardable(repo, branch, upstream string) error {
	if _, err := runGitFn(repo, "fetch", "--quiet"); err != nil {
		return fmt.Errorf("%s: couldn't fetch %s to check whether %q can fast-forward — check your network or credentials and update again: %w",
			repo, upstream, branch, err)
	}
	out, err := runGitFn(repo, "rev-list", "--left-right", "--count", upstream+"...HEAD")
	if err != nil {
		return err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return fmt.Errorf("cannot count local commits on %q: git rev-list said %q", branch, out)
	}
	behind, errBehind := strconv.Atoi(fields[0])
	ahead, errAhead := strconv.Atoi(fields[1])
	if errBehind != nil || errAhead != nil {
		return fmt.Errorf("cannot count local commits on %q: git rev-list said %q", branch, out)
	}
	if behind == 0 || ahead == 0 {
		return nil
	}
	_, tracked, _ := strings.Cut(upstream, "/")
	if branch == tracked {
		return fmt.Errorf("%s is on branch %q, which has %d local %s that %s does not and is %d %s behind it — it has diverged. "+
			"The updater only fast-forwards. Push those commits upstream or move them to another branch before updating",
			repo, branch, ahead, commitNoun(ahead), upstream, behind, commitNoun(behind))
	}
	return fmt.Errorf("%s is on branch %q, which has %d local %s that %s does not and is %d %s behind it — it has diverged. "+
		"The updater only fast-forwards. Check out the branch it tracks (git checkout %s) and update again; anything worth keeping from %q needs to land upstream first",
		repo, branch, ahead, commitNoun(ahead), upstream, behind, commitNoun(behind), tracked, branch)
}

// commitNoun is "commit" or "commits" to follow n in a sentence.
func commitNoun(n int) string {
	if n == 1 {
		return "commit"
	}
	return "commits"
}

// verifyUpstreamRemote refuses a checkout whose tracked branch does not
// come from this project's own repository, and returns that tracked
// branch (remote/name) for the checks that follow.
//
// The remote URL is matched on host and path whole (see
// remoteIsUpstream), so both SSH (git@github.com:lucascaro/hive.git)
// and HTTPS spellings pass and a trailing .git or slash is tolerated,
// while a host that merely contains "github.com" does not. A fork
// would be rejected — that is the intended trade: this button pulls
// and *executes*, so "close enough" is not the bar.
func verifyUpstreamRemote(repo string) (string, error) {
	upstream, err := runGitFn(repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return "", fmt.Errorf("%s has no upstream branch to pull from", repo)
	}
	remote, _, found := strings.Cut(upstream, "/")
	if !found || remote == "" {
		return "", fmt.Errorf("cannot tell which remote %q tracks", upstream)
	}
	remoteURL, err := runGitFn(repo, "remote", "get-url", remote)
	if err != nil {
		return "", err
	}
	if !remoteIsUpstream(remoteURL) {
		return "", fmt.Errorf("refusing to build from %s: remote %q is %s, not %s", repo, remote, remoteURL, updateRepo)
	}
	return upstream, nil
}

// plainProgressLine reduces one line of build output to what a terminal
// would actually show, as plain text. build.sh drives npm/vite/wails,
// which colour their output and redraw progress with carriage returns —
// and the update banner renders its message as text, so anything left
// in here reaches the user as literal `ESC[32m` garbage.
//
// Only what that output really contains is handled: CR redraws, CSI and
// OSC sequences, and any other C0 control byte. A full VT parser lives
// in internal/session for the terminal; the banner has one short line.
func plainProgressLine(s string) string {
	// A CR redraw means everything before the last CR was overwritten.
	if i := strings.LastIndexByte(s, '\r'); i >= 0 {
		s = s[i+1:]
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == 0x1B && i+1 < len(s) && s[i+1] == '[':
			// CSI: parameter/intermediate bytes, then a final byte in 0x40–0x7E.
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7E) {
				i++
			}
			if i < len(s) {
				i++
			}
		case c == 0x1B && i+1 < len(s) && s[i+1] == ']':
			// OSC: terminated by BEL or ST (ESC \).
			i += 2
			for i < len(s) {
				if s[i] == 0x07 {
					i++
					break
				}
				if s[i] == 0x1B && i+1 < len(s) && s[i+1] == '\\' {
					i += 2
					break
				}
				i++
			}
		case c == 0x1B:
			// Any other escape: drop ESC and the byte it introduces.
			i += 2
		case c < 0x20 && c != '\t', c == 0x7F:
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return strings.TrimSpace(b.String())
}
