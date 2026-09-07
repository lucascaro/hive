package main

import (
	"net/url"
	"strings"
)

// remoteIsUpstream reports whether a git remote URL names exactly
// github.com/<updateRepo>: HTTPS, ssh://, or scp-like git@ spellings,
// with an optional .git suffix or trailing slash. Host and path are
// compared whole — a substring match let evil.example/lucascaro/hive
// through, and this gate guards a pull that is then executed.
//
// It lives in a non-suffixed file, away from its only (darwin-only)
// caller, so its table test runs on every platform CI builds.
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
