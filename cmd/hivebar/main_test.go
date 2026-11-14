// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import "testing"

// Restart Menu Bar relaunches with -restart. If main stopped honouring it the
// new instance would lose the lock to its still-quitting predecessor and exit
// quietly, leaving the user with no menu bar.
func TestRestartFlagEnablesLockWait(t *testing.T) {
	o, err := parseFlags([]string{restartFlag})
	if err != nil {
		t.Fatal(err)
	}
	if !o.restart || o.lockWait() != restartLockWait {
		t.Errorf("restart=%v lockWait=%v, want true/%v", o.restart, o.lockWait(), restartLockWait)
	}
	o, err = parseFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.restart || o.lockWait() != 0 {
		t.Errorf("a normal start must lose the lock race immediately, got wait %v", o.lockWait())
	}
}

func TestVersionFlagStillParses(t *testing.T) {
	o, err := parseFlags([]string{"-version"})
	if err != nil || !o.version {
		t.Errorf("version=%v err=%v", o.version, err)
	}
}
