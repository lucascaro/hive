package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/buildinfo"
)

// fakeGit replays canned answers keyed by the first two git args, and
// records every invocation so a test can assert what did NOT run.
type fakeGit struct {
	answers map[string]string
	errs    map[string]error
	calls   []string
	// dirs[i] is the directory calls[i] ran in, for asserting which
	// tree a command touched.
	dirs []string
}

func (f *fakeGit) run(dir string, args ...string) (string, error) {
	joined := strings.Join(args, " ")
	f.calls = append(f.calls, joined)
	f.dirs = append(f.dirs, dir)
	key := args[0]
	if len(args) > 1 {
		key = args[0] + " " + args[1]
	}
	if err, ok := f.errs[key]; ok {
		return "", err
	}
	if out, ok := f.answers[joined]; ok {
		return out, nil
	}
	if out, ok := f.answers[key]; ok {
		return out, nil
	}
	return "", nil
}

// firstCall is the index of the first recorded git invocation that
// contains sub, or -1. Substring, for the same reason fakeGit.ran is.
func firstCall(g *fakeGit, sub string) int {
	for i, c := range g.calls {
		if strings.Contains(c, sub) {
			return i
		}
	}
	return -1
}

func (f *fakeGit) install(t *testing.T) {
	t.Helper()
	prev := runGitFn
	runGitFn = f.run
	t.Cleanup(func() { runGitFn = prev })
}

// ran reports whether any recorded invocation contains sub. Substring,
// not prefix: git calls carry `-c key=value` flags before the
// subcommand, and a prefix match silently stopped detecting `pull` the
// moment core.hooksPath was pinned in front of it — turning a
// "must not pull" assertion into one that could never fail.
func (f *fakeGit) ran(sub string) bool {
	for _, c := range f.calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

func TestCheckLatestReportsBehind(t *testing.T) {
	restore := buildinfo.SetForTest("abc1234")
	t.Cleanup(restore)

	g := &fakeGit{answers: map[string]string{
		"remote":                                "origin",
		"remote get-url origin":                 "git@github.com:" + updateRepo + ".git",
		"rev-parse --short origin/main":         "def5678",
		"cat-file -e abc1234^{commit}":          "",
		"rev-list --count abc1234..origin/main": "3",
	}}
	g.install(t)

	info, err := checkLatest("/repo")
	if err != nil {
		t.Fatalf("checkLatest: %v", err)
	}
	if !info.Available {
		t.Error("Available = false, want true when the upstream is 3 commits ahead")
	}
	if info.Stage != StageAvailable {
		t.Errorf("Stage = %q, want %q", info.Stage, StageAvailable)
	}
	if info.Current != "abc1234" || info.Latest != "def5678" {
		t.Errorf("Current/Latest = %q/%q, want abc1234/def5678", info.Current, info.Latest)
	}
	if info.Channel != ChannelLatest {
		t.Errorf("Channel = %q, want %q", info.Channel, ChannelLatest)
	}
	if !g.ran("fetch") {
		t.Error("checkLatest did not fetch; the comparison would be against stale refs")
	}
}

func TestCheckLatestUpToDate(t *testing.T) {
	restore := buildinfo.SetForTest("abc1234")
	t.Cleanup(restore)

	g := &fakeGit{answers: map[string]string{
		"remote":                                "origin",
		"remote get-url origin":                 "git@github.com:" + updateRepo + ".git",
		"rev-parse --short origin/main":         "abc1234",
		"rev-list --count abc1234..origin/main": "0",
	}}
	g.install(t)

	info, err := checkLatest("/repo")
	if err != nil {
		t.Fatalf("checkLatest: %v", err)
	}
	if info.Available {
		t.Error("Available = true with 0 commits behind, want false")
	}
}

// A checkout whose build id is not a commit in this repo — a "dev"
// build, or a different clone — must fall back to comparing HEAD rather
// than erroring or silently reporting "up to date".
func TestCheckLatestFallsBackToHeadForUnknownBuild(t *testing.T) {
	restore := buildinfo.SetForTest("dev")
	t.Cleanup(restore)

	g := &fakeGit{answers: map[string]string{
		"remote":                             "origin",
		"remote get-url origin":              "git@github.com:" + updateRepo + ".git",
		"rev-parse --short origin/main":      "def5678",
		"rev-list --count HEAD..origin/main": "1",
	}}
	g.install(t)

	info, err := checkLatest("/repo")
	if err != nil {
		t.Fatalf("checkLatest: %v", err)
	}
	if !info.Available {
		t.Error("Available = false for a dev build one commit behind, want true")
	}
	if g.ran("cat-file") {
		t.Error("checkLatest probed git for a non-sha build id")
	}
}

// The comparison is against the pinned remote's main, whatever branch
// the checkout has out and whether or not that branch tracks anything.
// A feature branch with no upstream used to make the check skip; now
// it is simply not consulted.
func TestCheckLatestComparesAgainstThePinnedRemotesMain(t *testing.T) {
	restore := buildinfo.SetForTest("abc1234")
	t.Cleanup(restore)

	g := &fakeGit{
		answers: map[string]string{
			"remote":                                  "origin\nupstream",
			"remote get-url origin":                   "https://github.com/someone-else/hive.git",
			"remote get-url upstream":                 "https://github.com/" + updateRepo,
			"rev-parse --short upstream/main":         "def5678",
			"cat-file -e abc1234^{commit}":            "",
			"rev-list --count abc1234..upstream/main": "2",
		},
		errs: map[string]error{
			"rev-parse --abbrev-ref": fmt.Errorf("no upstream configured"),
		},
	}
	g.install(t)

	info, err := checkLatest("/repo")
	if err != nil {
		t.Fatalf("checkLatest: %v", err)
	}
	if !info.Available || info.Latest != "def5678" {
		t.Errorf("Available/Latest = %v/%q, want true/def5678 from the pinned remote", info.Available, info.Latest)
	}
	if !strings.Contains(info.Message, "upstream/main") {
		t.Errorf("Message = %q, want it to name upstream/main", info.Message)
	}
	if fetch := firstCall(g, "fetch"); fetch < 0 || !strings.Contains(g.calls[fetch], "upstream") {
		t.Errorf("checkLatest did not fetch the pinned remote; calls: %q", g.calls)
	}
	if g.ran("@{upstream}") || g.ran("origin/main") {
		t.Errorf("checkLatest consulted the checkout's branch or the fork; calls: %q", g.calls)
	}
}

func TestCheckLatestSkipsWithoutAPinnedRemote(t *testing.T) {
	g := &fakeGit{answers: map[string]string{
		"remote":                "origin",
		"remote get-url origin": "https://github.com/someone-else/hive.git",
	}}
	g.install(t)

	info, err := checkLatest("/repo")
	if err != nil {
		t.Fatalf("checkLatest = error for a checkout with no pinned remote, want a skip: %v", err)
	}
	if !info.Skipped {
		t.Error("Skipped = false with no pinned remote, want true")
	}
	if !strings.Contains(info.Message, updateRepo) {
		t.Errorf("Message = %q, want it to name %s", info.Message, updateRepo)
	}
	if info.Available {
		t.Error("Available = true with no pinned remote, want false")
	}
	if g.ran("fetch") {
		t.Error("checkLatest fetched from a remote nobody has vouched for")
	}
}
