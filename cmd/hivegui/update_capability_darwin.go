//go:build darwin

package main

import "path/filepath"

// updateCapability reports whether this build can install an update in
// place, and why not when it cannot.
//
// macOS answers yes unconditionally. applyStagedBundle still refuses a
// process that is not running from an .app bundle, but it does so at
// click time with a specific message — and that has been the behaviour
// since in-app update shipped. Turning it into an up-front capability
// probe would change what a `wails dev` session shows, which is a macOS
// UX change that belongs in its own PR rather than riding along with
// the Windows port.
func updateCapability() (bool, string) { return true, "" }

// stagedDaemonPath locates hived inside a staged payload, so the
// contract probe in update_restart_kind.go does not have to know what a
// payload looks like on this platform.
func stagedDaemonPath(staged string) string {
	return filepath.Join(staged, "Contents", "MacOS", "hived")
}

// pruneRenamedAside is the Windows startup sweep for images that could
// not be deleted while they were still mapped. macOS replaces a bundle
// wholesale and leaves nothing behind, so there is nothing to sweep.
func pruneRenamedAside() {}
