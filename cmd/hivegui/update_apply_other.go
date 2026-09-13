//go:build !darwin && !windows

package main

import "fmt"

// Self-update applies on macOS and Windows. Linux ships no release
// artifact (README documents a native build), so there is nothing for
// an in-app updater to install here. The banner keeps its Download
// button, which opens the release page — so this is a missing
// convenience, not a missing update path.
//
// errUnsupported is returned rather than silently doing nothing: the
// frontend shows it, which is how the user learns to use the link.
var errUnsupported = fmt.Errorf("in-app update is not available on this platform — use the Download button to update manually")

func stageUpdate(UpdateInfo, func(string)) (string, error) { return "", errUnsupported }

func applyStagedBundle(string) error { return errUnsupported }

// updateCapability reports that nothing here can install an update, so
// the frontend renders the reason instead of an Update button that
// would dead-end. The reason is the user-facing half of errUnsupported.
func updateCapability() (bool, string) {
	return false, "in-app update is not available on this platform"
}

// stagedDaemonPath would locate hived inside a staged payload. Nothing
// stages here, but the symbol has to exist or cmd/hivegui stops
// compiling for linux, which a darwin-only local run would not notice.
func stagedDaemonPath(staged string) string { return staged }

// pruneRenamedAside is the Windows startup sweep for images that could
// not be deleted while mapped. Nothing renames anything aside here.
func pruneRenamedAside() {}
