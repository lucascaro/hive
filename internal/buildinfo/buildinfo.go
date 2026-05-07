// Package buildinfo holds build identity strings populated at link
// time via -ldflags. Both hived and hivegui import this so the
// version-handshake banner can detect a stale daemon (built from a
// different commit than the GUI talking to it).
package buildinfo

import (
	"runtime/debug"
	"sync"
)

// buildIDOverride is set at link time, e.g.
//
//	go build -ldflags "-X github.com/lucascaro/hive/internal/buildinfo.buildIDOverride=$(git rev-parse --short HEAD)"
//
// When unset, BuildID() falls back to the VCS revision Go embeds in
// every build (Go ≥ 1.18). This means a plain `go build` still
// produces a meaningful, distinct BuildID — important because the
// stale-daemon banner is silent when both sides report the same
// string, and "dev"-vs-"dev" was the original bug class this whole
// feature exists to surface.
var buildIDOverride = ""

// versionOverride is set at link time to the human-readable release
// version (e.g. "0.4.1"), e.g.
//
//	go build -ldflags "-X github.com/lucascaro/hive/internal/buildinfo.versionOverride=0.4.1"
//
// It is set by ./build.sh when invoked with --version, and by
// scripts/release.sh during a tagged release. Plain `go build` and
// `./build.sh` (no --version) leave this empty, in which case
// Version() reports "dev" — the GUI's update checker treats "dev" as
// "skip update check" so untagged local builds don't get pestered
// with banners.
var versionOverride = ""

var (
	mu       sync.Mutex
	resolved string
	cached   bool
)

// SetForTest overrides the resolved BuildID for the lifetime of the
// caller's test. Use t.Cleanup to restore. Tests that mutate this
// must not run with t.Parallel(); the daemon reads BuildID() from
// per-connection goroutines, so a write race would tear.
func SetForTest(value string) (restore func()) {
	mu.Lock()
	prevResolved, prevCached := resolved, cached
	resolved = value
	cached = true
	mu.Unlock()
	return func() {
		mu.Lock()
		resolved = prevResolved
		cached = prevCached
		mu.Unlock()
	}
}

// BuildID returns this binary's build identity.
//
// Resolution order:
//  1. The link-time override (set by build.sh / release pipelines).
//  2. The Go-embedded vcs.revision (short to 7 chars), suffixed with
//     "-dirty" if vcs.modified is true.
//  3. The literal "dev" if both above are missing (shouldn't happen
//     for a normal `go build` post-1.18, but defensive).
func BuildID() string {
	mu.Lock()
	defer mu.Unlock()
	if cached {
		return resolved
	}
	if buildIDOverride != "" {
		resolved = buildIDOverride
	} else {
		resolved = vcsBuildID()
	}
	cached = true
	return resolved
}

// Version returns the human-readable release version (e.g. "0.4.1")
// stamped at link time, or "dev" when unset. Unlike BuildID this is
// suitable for comparing against a remote release tag — see the GUI
// update checker.
func Version() string {
	if versionOverride == "" {
		return "dev"
	}
	return versionOverride
}

func vcsBuildID() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}
