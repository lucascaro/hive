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
// It deliberately does not try to predict a build failure. Missing
// toolchains, a dirty tree and a detached HEAD are all things
// stageLatest reports with a specific message, and all things the user
// can fix without moving their install.
func updateCapability() (bool, string) {
	install, err := installDirFn()
	if err != nil {
		return false, "Hive cannot tell where it is installed, so it cannot update in place"
	}
	if err := ensureWritable(install); err != nil {
		return false, err.Error()
	}
	// The latest channel builds into a directory it first erases, which
	// cannot be the one Hive is running from. Checked here as well as in
	// stageLatest so the button is never offered for an install that
	// cannot take it.
	settings, err := loadUpdateSettings()
	if err != nil || settings.Channel != ChannelLatest {
		return true, ""
	}
	repo, err := resolveSourceRepo(settings.SourceRepo)
	if err != nil {
		// Not a capability problem: the latest-channel check reports a
		// missing or unusable checkout itself, with a better message
		// than anything this function could offer.
		return true, ""
	}
	if err := checkLatestInstallLayout(install, buildOutputDir(repo)); err != nil {
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
