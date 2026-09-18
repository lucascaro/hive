//go:build darwin || windows

package main

import (
	"fmt"
	"strings"
	"testing"
)

// cleanCheckout is a fakeGit answering as a checkout the updater has
// no reason to refuse: clean tree, on main, tracking origin/main at
// the pinned remote, nothing local. Each case below breaks one thing.
func cleanCheckout() map[string]string {
	return map[string]string{
		"status --porcelain":                                      "",
		"symbolic-ref --quiet --short HEAD":                       "main",
		"rev-parse --abbrev-ref --symbolic-full-name @{upstream}": "origin/main",
		"remote get-url origin":                                   "git@github.com:" + updateRepo + ".git",
		"rev-list --left-right --count origin/main...HEAD":        "0\t0",
	}
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

// preflightCheckout is the whole list of things stageLatest refuses
// before the checkout moves or the build starts, on both platforms.
// Every refusal must name the problem in the user's terms — none of
// these should ever reach the banner as raw git stderr — and none may
// pull. Only the divergence check may fetch, and only once the remote
// has been pinned: the refusals before it must not have talked to a
// remote nobody has vouched for yet.
func TestPreflightCheckoutRefusals(t *testing.T) {
	cases := []struct {
		name    string
		answers map[string]string
		errs    map[string]error
		want    string
		fetches bool
	}{
		{
			name:    "dirty tree",
			answers: map[string]string{"status --porcelain": " M cmd/hivegui/app.go"},
			want:    "uncommitted changes",
		},
		{
			name: "detached HEAD",
			errs: map[string]error{"symbolic-ref --quiet": fmt.Errorf("exit status 1")},
			want: "detached HEAD",
		},
		{
			name: "no upstream",
			errs: map[string]error{"rev-parse --abbrev-ref": fmt.Errorf("fatal: no upstream configured for branch 'main'")},
			want: "no upstream branch",
		},
		{
			// git refusing to run at all is not a missing upstream: the
			// user needs git's own message, not a branch problem.
			name: "git itself fails",
			errs: map[string]error{"rev-parse --abbrev-ref": fmt.Errorf("git rev-parse: You have not agreed to the Xcode license agreements.")},
			want: "Xcode license",
		},
		{
			name:    "foreign remote",
			answers: map[string]string{"remote get-url origin": "https://github.com/someone-else/hive.git"},
			want:    "refusing to build",
		},
		{
			name: "diverged branch",
			answers: map[string]string{
				"symbolic-ref --quiet --short HEAD":                "integ/windows-parity",
				"rev-list --left-right --count origin/main...HEAD": "5\t14",
			},
			want:    `branch "integ/windows-parity", which has 14 local commits that origin/main does not`,
			fetches: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			answers := cleanCheckout()
			for k, v := range tc.answers {
				answers[k] = v
			}
			g := &fakeGit{answers: answers, errs: tc.errs}
			g.install(t)

			err := preflightCheckout("/repo")
			if err == nil {
				t.Fatalf("preflightCheckout = nil error, want a refusal mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to say %q", err, tc.want)
			}
			if g.ran("pull") {
				t.Error("preflightCheckout pulled — it must only look")
			}
			if got := g.ran("fetch"); got != tc.fetches {
				t.Errorf("preflightCheckout fetched = %v, want %v (calls: %q)", got, tc.fetches, g.calls)
			}
		})
	}
}

// The counts are only as fresh as the remote-tracking ref, and the
// periodic check last fetched that up to updateCheckInterval ago.
// `pull --ff-only` fetches before it decides, so the preflight has to
// as well, or "only ahead" here can be "diverged" by the time the pull
// looks — and the banner shows git's words after all, which is the
// failure this check exists to prevent.
func TestPreflightCheckoutFetchesBeforeCounting(t *testing.T) {
	g := &fakeGit{answers: cleanCheckout()}
	g.install(t)
	if err := preflightCheckout("/repo"); err != nil {
		t.Fatalf("preflightCheckout = %v, want nil", err)
	}
	fetch, count := firstCall(g, "fetch"), firstCall(g, "rev-list")
	if fetch < 0 {
		t.Fatalf("preflightCheckout never fetched; calls: %q", g.calls)
	}
	if count < 0 || count < fetch {
		t.Errorf("preflightCheckout counted (call %d) before fetching (call %d); calls: %q", count, fetch, g.calls)
	}
	if pin := firstCall(g, "remote get-url"); pin < 0 || fetch < pin {
		t.Errorf("preflightCheckout fetched (call %d) before pinning the remote (call %d); calls: %q", fetch, pin, g.calls)
	}
}

// A fetch that fails (offline, auth, a remote that went away) leaves
// the counts meaningless, and a meaningless count is a refusal, not a
// pass — the same rule as an unreadable one.
func TestPreflightCheckoutRefusesWhenFetchFails(t *testing.T) {
	g := &fakeGit{
		answers: cleanCheckout(),
		errs:    map[string]error{"fetch --quiet": fmt.Errorf("git fetch --quiet: fatal: unable to access 'https://github.com/'")},
	}
	g.install(t)

	err := preflightCheckout("/repo")
	if err == nil {
		t.Fatal("preflightCheckout = nil error when the fetch failed, want a refusal")
	}
	// In the user's terms, like every other refusal — git's own text
	// stays attached as the cause, not as the message.
	for _, want := range []string{"couldn't fetch origin/main", `"main" can fast-forward`, "check your network or credentials", "unable to access"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
	if strings.HasPrefix(err.Error(), "git ") {
		t.Errorf("error = %q, want a plain-language refusal, not raw git stderr", err)
	}
	if g.ran("rev-list") {
		t.Error("preflightCheckout counted against a ref it had just failed to refresh")
	}
	if g.ran("pull") {
		t.Error("preflightCheckout pulled — it must only look")
	}
}

func TestPreflightCheckoutAcceptsACleanTrackingBranch(t *testing.T) {
	g := &fakeGit{answers: cleanCheckout()}
	g.install(t)
	if err := preflightCheckout("/repo"); err != nil {
		t.Fatalf("preflightCheckout = %v on a clean, up to date checkout, want nil", err)
	}
	if g.ran("pull") {
		t.Error("preflightCheckout pulled — it must only look")
	}
}

// Ahead-only and behind-only are not divergence: a pull either does
// nothing (ahead) or fast-forwards cleanly (behind), so both must pass.
func TestPreflightCheckoutAcceptsAheadOrBehindOnly(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count string
	}{
		{"ahead only", "0\t3"},
		{"behind only", "5\t0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answers := cleanCheckout()
			answers["rev-list --left-right --count origin/main...HEAD"] = tc.count
			g := &fakeGit{answers: answers}
			g.install(t)
			if err := preflightCheckout("/repo"); err != nil {
				t.Fatalf("preflightCheckout = %v, want nil", err)
			}
			if g.ran("pull") {
				t.Error("preflightCheckout pulled — it must only look")
			}
		})
	}
}

// The way out has to be in the message: the user who hits this is
// looking at a banner, not a terminal, and "Not possible to
// fast-forward, aborting." is what they got before. Singular and
// plural both read as English.
func TestDivergedBranchRefusalNamesTheWayOut(t *testing.T) {
	answers := cleanCheckout()
	answers["symbolic-ref --quiet --short HEAD"] = "feat/thing"
	answers["rev-list --left-right --count origin/main...HEAD"] = "2\t1"
	g := &fakeGit{answers: answers}
	g.install(t)

	err := preflightCheckout("/repo")
	if err == nil {
		t.Fatal("preflightCheckout = nil error on a diverged branch, want a refusal")
	}
	for _, want := range []string{"1 local commit that", "2 commits behind it", "git checkout main", `"feat/thing"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "Not possible to fast-forward") {
		t.Errorf("error = %q, want the updater's words, not git's", err)
	}
}

// main tracking origin/main has no "other" branch to check out — the
// advice must not tell the user to check out the branch they are
// already on. Both counts read as English here too, the other way
// round from the test above.
func TestDivergedMainRefusalDoesNotSuggestCheckingOutMain(t *testing.T) {
	answers := cleanCheckout()
	answers["rev-list --left-right --count origin/main...HEAD"] = "1\t2"
	g := &fakeGit{answers: answers}
	g.install(t)

	err := preflightCheckout("/repo")
	if err == nil {
		t.Fatal("preflightCheckout = nil error on a diverged main, want a refusal")
	}
	for _, want := range []string{"2 local commits that", "1 commit behind it"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "git checkout main") {
		t.Errorf("error = %q, want it to not suggest checking out the branch it is already on", err)
	}
	if strings.Contains(err.Error(), "reset --hard") {
		t.Errorf("error = %q, want it to not suggest discarding local commits", err)
	}
}

// A count git could not produce is a refusal, not a pass: "" or garbage
// from rev-list means the checkout is in a state this code does not
// understand, and the safe answer to that is not to pull.
func TestDivergedBranchCheckRefusesAnUnreadableCount(t *testing.T) {
	answers := cleanCheckout()
	answers["rev-list --left-right --count origin/main...HEAD"] = "fatal: bad revision"
	g := &fakeGit{answers: answers}
	g.install(t)

	if err := preflightCheckout("/repo"); err == nil {
		t.Fatal("preflightCheckout = nil error with an unreadable rev-list count, want a refusal")
	}
}
