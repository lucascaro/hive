// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import (
	"testing"
	"time"

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

	release, ok := claimSingleton(0)
	if !ok {
		t.Fatal("first claim failed")
	}

	if _, ok := claimSingleton(0); ok {
		t.Error("second claim succeeded; two menu bar icons would appear")
	}

	// And the lock is reusable once the holder lets go, so a restarted
	// hivebar is not locked out by its own dead predecessor.
	release()
	release2, ok := claimSingleton(0)
	if !ok {
		t.Fatal("claim after release failed; a crash would wedge the menu bar until reboot")
	}
	release2()
}

// A restarted hivebar starts while its predecessor still holds the lock.
// Failing immediately — the normal "lost the race" path — would leave the
// user with no menu bar at all.
func TestClaimSingletonWaitsForRelease(t *testing.T) {
	t.Setenv("HIVE_STATE_DIR", t.TempDir())
	if !registry.StateDirOverridden() {
		t.Fatal("HIVE_STATE_DIR did not take effect")
	}
	release, ok := claimSingleton(0)
	if !ok {
		t.Fatal("first claim failed")
	}
	if _, ok := claimSingleton(0); ok {
		t.Fatal("no-wait claim succeeded while the lock was held")
	}
	time.AfterFunc(200*time.Millisecond, release)
	release2, ok := claimSingleton(3 * time.Second)
	if !ok {
		t.Fatal("waiting claim did not take the lock after the holder released it")
	}
	release2()
}

func TestClaimSingletonWaitGivesUp(t *testing.T) {
	t.Setenv("HIVE_STATE_DIR", t.TempDir())
	if !registry.StateDirOverridden() {
		t.Fatal("HIVE_STATE_DIR did not take effect")
	}
	release, ok := claimSingleton(0)
	if !ok {
		t.Fatal("first claim failed")
	}
	defer release()
	start := time.Now()
	if _, ok := claimSingleton(100 * time.Millisecond); ok {
		t.Fatal("claim succeeded while the lock was held")
	}
	if d := time.Since(start); d < 100*time.Millisecond || d > 2*time.Second {
		t.Errorf("gave up after %v, want ~100ms", d)
	}
}
