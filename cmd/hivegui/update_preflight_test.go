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
		"rev-list --count origin/main..HEAD":                      "0",
	}
}

// preflightCheckout is the whole list of things stageLatest refuses
// before git moves or the build starts, on both platforms. Every
// refusal must name the problem in the user's terms — none of these
// should ever reach the banner as raw git stderr — and none may pull.
func TestPreflightCheckoutRefusals(t *testing.T) {
	cases := []struct {
		name    string
		answers map[string]string
		errs    map[string]error
		want    string
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
			name:    "foreign remote",
			answers: map[string]string{"remote get-url origin": "https://github.com/someone-else/hive.git"},
			want:    "refusing to build",
		},
		{
			name: "diverged branch",
			answers: map[string]string{
				"symbolic-ref --quiet --short HEAD":  "integ/windows-parity",
				"rev-list --count origin/main..HEAD": "14",
			},
			want: `branch "integ/windows-parity", which has 14 local commits that origin/main does not`,
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
		})
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

// The way out has to be in the message: the user who hits this is
// looking at a banner, not a terminal, and "Not possible to
// fast-forward, aborting." is what they got before. Singular and
// plural both read as English.
func TestDivergedBranchRefusalNamesTheWayOut(t *testing.T) {
	answers := cleanCheckout()
	answers["symbolic-ref --quiet --short HEAD"] = "feat/thing"
	answers["rev-list --count origin/main..HEAD"] = "1"
	g := &fakeGit{answers: answers}
	g.install(t)

	err := preflightCheckout("/repo")
	if err == nil {
		t.Fatal("preflightCheckout = nil error on a diverged branch, want a refusal")
	}
	for _, want := range []string{"1 local commit that", "git checkout main", `"feat/thing"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "Not possible to fast-forward") {
		t.Errorf("error = %q, want the updater's words, not git's", err)
	}
}

// A count git could not produce is a refusal, not a pass: "" or garbage
// from rev-list means the checkout is in a state this code does not
// understand, and the safe answer to that is not to pull.
func TestDivergedBranchCheckRefusesAnUnreadableCount(t *testing.T) {
	answers := cleanCheckout()
	answers["rev-list --count origin/main..HEAD"] = "fatal: bad revision"
	g := &fakeGit{answers: answers}
	g.install(t)

	if err := preflightCheckout("/repo"); err == nil {
		t.Fatal("preflightCheckout = nil error with an unreadable rev-list count, want a refusal")
	}
}
