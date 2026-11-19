// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/lucascaro/hive/internal/buildinfo"
	"github.com/lucascaro/hive/internal/menubar"
	"github.com/lucascaro/hive/internal/registry"
)

const (
	// restartFlag marks an instance started by Restart Menu Bar.
	restartFlag = menubar.RestartFlag
	// restartLockWait is how long a restarted instance waits for its
	// predecessor's lock. Generous: the predecessor only has to return
	// from systray.Run, which is milliseconds.
	restartLockWait  = 5 * time.Second
	lockPollInterval = 50 * time.Millisecond
)

// claimSingleton takes an exclusive lock so only one hivebar runs.
//
// Both hived and hivegui spawn hivebar best-effort on boot, and
// launchd may have started it at login as well, so several attempts
// racing at once is the normal case rather than an edge one. Two status
// items in the menu bar is not a subtle bug — it is the most visible
// thing the user could possibly see.
//
// flock, not a pidfile: a pidfile has to be validated against a
// possibly-recycled pid (see cmd/hivegui/restart_unix.go, which needs a
// `ps` name check for exactly that reason) and has to be cleaned up
// after a crash. A flock is released by the kernel when the process
// dies, however it dies.
//
// The returned release function drops the lock. It is unnecessary at
// exit — the kernel does it — but a test needs to hand the lock back.
//
// wait is how long to keep trying when the lock is held. Zero is the normal
// start: lose quietly, because losing the race is the expected case. A
// restarted instance passes restartLockWait instead — its predecessor is
// quitting on purpose and is about to let go, so failing at once would
// leave no menu bar at all.
func claimSingleton(wait time.Duration) (release func(), ok bool) {
	path := filepath.Join(registry.StateDir(), menubar.LockFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		// No state dir means no daemon either. Refuse rather than
		// running unlocked: an unlocked hivebar is how you get two.
		return nil, false
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false
	}
	deadline := time.Now().Add(wait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !time.Now().Before(deadline) {
			_ = f.Close()
			return nil, false
		}
		time.Sleep(lockPollInterval)
	}
	// Record who holds it. menubar.Spawn reads the build id back to
	// replace a menu bar left running from before an update.
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(menubar.FormatLockRecord(os.Getpid(), buildinfo.BuildID())), 0)
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, true
}
