package main

import (
	"fmt"
	"net/url"
	"strings"
)

// latestBranch is the branch the latest channel tracks on the pinned
// remote. The tip of main is what "latest" always meant for a checkout
// on main; it now means that for every checkout.
const latestBranch = "main"

// pinnedRemote returns the name of the remote in repo whose URL is
// this project's own repository.
//
// Found by URL, not by name and not through the current branch's
// upstream: a checkout parked on a branch that tracks nothing still
// has a remote worth building from, and a clone where the pinned
// repository is called "upstream" and "origin" is a fork must build
// from the former. The URL match is remoteIsUpstream's, whole host and
// path, because what comes from this remote is fetched and executed.
//
// Portable, unlike the build tree it feeds (update_source_tree.go):
// the check in update_latest.go runs on Linux too, where there is no
// in-place apply.
func pinnedRemote(repo string) (string, error) {
	out, err := runGitFn(repo, "remote")
	if err != nil {
		return "", err
	}
	for _, name := range strings.Fields(out) {
		url, err := runGitFn(repo, "remote", "get-url", name)
		if err != nil {
			return "", err
		}
		if remoteIsUpstream(url) {
			return name, nil
		}
	}
	return "", fmt.Errorf("refusing to build from %s: no remote there points at github.com/%s — "+
		"add one (git remote add origin https://github.com/%s.git) and update again", repo, updateRepo, updateRepo)
}

// remoteIsUpstream reports whether a git remote URL names exactly
// github.com/<updateRepo>: HTTPS, ssh://, or scp-like git@ spellings,
// with an optional .git suffix or trailing slash. Host and path are
// compared whole — a substring match let evil.example/lucascaro/hive
// through, and this gate guards a fetch that is then executed.
func remoteIsUpstream(remote string) bool {
	remote = strings.TrimSpace(remote)
	var host, path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return false
		}
		host, path = u.Hostname(), u.Path
	} else if at := strings.Index(remote, "@"); at >= 0 {
		// scp-like: git@github.com:owner/repo.git
		h, p, ok := strings.Cut(remote[at+1:], ":")
		if !ok {
			return false
		}
		host, path = h, "/"+p
	} else {
		return false
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	return strings.EqualFold(host, "github.com") && path == "/"+updateRepo
}
