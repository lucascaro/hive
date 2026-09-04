//go:build darwin

package main

import (
	"testing"

	"github.com/lucascaro/hive/internal/registry"
)

// Both hived and hivegui spawn hivebar best-effort, and launchd may
// have started it at login, so several instances racing is the normal
// case. Two icons in the menu bar is the most visible bug this feature
// could ship.
func TestSingletonRefusesASecondInstance(t *testing.T) {
	t.Setenv("HIVE_STATE_DIR", t.TempDir())
	if !registry.StateDirOverridden() {
		t.Fatal("HIVE_STATE_DIR did not take effect; this test would lock the real state dir")
	}

	release, ok := claimSingleton()
	if !ok {
		t.Fatal("first claim failed")
	}

	if _, ok := claimSingleton(); ok {
		t.Error("second claim succeeded; two menu bar icons would appear")
	}

	// And the lock is reusable once the holder lets go, so a restarted
	// hivebar is not locked out by its own dead predecessor.
	release()
	release2, ok := claimSingleton()
	if !ok {
		t.Fatal("claim after release failed; a crash would wedge the menu bar until reboot")
	}
	release2()
}
