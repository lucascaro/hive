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
}

func (f *fakeGit) run(_ string, args ...string) (string, error) {
	joined := strings.Join(args, " ")
	f.calls = append(f.calls, joined)
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
		"rev-parse --abbrev-ref --symbolic-full-name @{upstream}": "origin/main",
		"rev-parse --short origin/main":                           "def5678",
		"cat-file -e abc1234^{commit}":                            "",
		"rev-list --count abc1234..origin/main":                   "3",
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
		"rev-parse --abbrev-ref --symbolic-full-name @{upstream}": "origin/main",
		"rev-parse --short origin/main":                           "abc1234",
		"rev-list --count abc1234..origin/main":                   "0",
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
		"rev-parse --abbrev-ref --symbolic-full-name @{upstream}": "origin/main",
		"rev-parse --short origin/main":                           "def5678",
		"rev-list --count HEAD..origin/main":                      "1",
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

func TestCheckLatestSkipsWithoutUpstream(t *testing.T) {
	g := &fakeGit{errs: map[string]error{
		"rev-parse --abbrev-ref": fmt.Errorf("no upstream configured"),
	}}
	g.install(t)

	info, err := checkLatest("/repo")
	if err != nil {
		t.Fatalf("checkLatest = error for a branch with no upstream, want a skip: %v", err)
	}
	if !info.Skipped {
		t.Error("Skipped = false with no upstream, want true")
	}
	if info.Available {
		t.Error("Available = true with no upstream, want false")
	}
	if g.ran("fetch") {
		t.Error("checkLatest fetched despite having no upstream to compare against")
	}
}
