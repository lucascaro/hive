//go:build windows

package main

import "path/filepath"

// updateCapability reports whether this build can install an update in
// place, and why not when it cannot.
//
// Unlike macOS this is a real probe, not a constant. It answers the two
// questions the swap depends on before the button is ever drawn, so a
// user whose install cannot be updated reads the reason in the banner
// instead of discovering it by clicking Update — which is the whole
// complaint this port exists to fix.
//
// It deliberately does not try to predict a build failure. A missing
// toolchain or an unreachable remote are things stageLatest reports
// with a specific message, and things the user can fix without moving
// their install. Where the install lives relative to the checkout no
// longer matters either: the latest channel builds in its own tree
// (update_source_tree.go), so even the checkout's cmd/hivegui/build/bin
// is just a directory the swap installs into.
func updateCapability() (bool, string) {
	install, err := installDirFn()
	if err != nil {
		return false, "Hive cannot tell where it is installed, so it cannot update in place"
	}
	if err := ensureWritable(install); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// stagedDaemonPath locates hived inside a staged payload, so the
// contract probe in update_restart_kind.go does not have to know what a
// payload looks like on this platform.
func stagedDaemonPath(staged string) string {
	return filepath.Join(staged, daemonExe)
}
