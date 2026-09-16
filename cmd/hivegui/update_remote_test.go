package main

import (
	"strings"
	"testing"
)

func TestRemoteIsUpstream(t *testing.T) {
	ok := []string{
		"https://github.com/lucascaro/hive",
		"https://github.com/lucascaro/hive.git",
		"https://github.com/lucascaro/hive/",
		"git@github.com:lucascaro/hive.git",
		"ssh://git@github.com/lucascaro/hive.git",
		"https://GitHub.com/lucascaro/hive",
	}
	bad := []string{
		"https://evil.example/lucascaro/hive",
		"https://github.com.evil.example/lucascaro/hive",
		"https://github.com/lucascaro/hive-fork",
		"https://github.com/someone/lucascaro/hive",
		"git@gitlab.com:lucascaro/hive.git",
		"",
	}
	for _, u := range ok {
		if !remoteIsUpstream(u) {
			t.Errorf("%q: want true", u)
		}
	}
	for _, u := range bad {
		if remoteIsUpstream(u) {
			t.Errorf("%q: want false", u)
		}
	}
}

// ------------------------------ pinnedRemote ------------------------------

// The remote is found by URL, not by name and not through the current
// branch's upstream: a checkout on a feature branch with no upstream,
// or one where the pinned repository is called "upstream" and "origin"
// is a fork, still has exactly one remote worth building from.
func TestPinnedRemoteFindsTheRemoteByURL(t *testing.T) {
	g := &fakeGit{answers: map[string]string{
		"remote":                  "origin\nupstream",
		"remote get-url origin":   "https://github.com/someone-else/hive.git",
		"remote get-url upstream": "https://github.com/" + updateRepo,
	}}
	g.install(t)

	remote, err := pinnedRemote("/repo")
	if err != nil {
		t.Fatalf("pinnedRemote = %v, want the remote that points at %s", err, updateRepo)
	}
	if remote != "upstream" {
		t.Errorf("pinnedRemote = %q, want %q", remote, "upstream")
	}
	if g.ran("fetch") {
		t.Error("pinnedRemote fetched — it must only look")
	}
}

// A checkout with no remote at the pinned repository has nothing this
// button may pull and execute. That is a refusal in the user's terms,
// naming what to add, and nothing is fetched from the remotes it does
// have.
func TestPinnedRemoteRefusesWhenNoRemoteIsPinned(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answers map[string]string
	}{
		{"no remotes", map[string]string{"remote": ""}},
		{"only a fork", map[string]string{
			"remote":                "origin",
			"remote get-url origin": "https://github.com/someone-else/hive.git",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &fakeGit{answers: tc.answers}
			g.install(t)

			_, err := pinnedRemote("/repo")
			if err == nil {
				t.Fatal("pinnedRemote = nil error with no pinned remote, want a refusal")
			}
			for _, want := range []string{"refusing to build", updateRepo} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
			if g.ran("fetch") {
				t.Error("pinnedRemote fetched from a remote nobody has vouched for")
			}
		})
	}
}
