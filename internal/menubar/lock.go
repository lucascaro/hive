// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package menubar

import (
	"fmt"
	"strconv"
	"strings"
)

// LockFile is hivebar's singleton lock, under registry.StateDir().
//
// Its contents identify the holder: "<pid> <build id>\n". hivebar writes
// it (cmd/hivebar/singleton.go) and Spawn reads it to tell whether the
// running menu bar is a build older than the bundle it was spawned from.
const LockFile = "hivebar.lock"

// RestartFlag tells a freshly launched hivebar to wait for its
// predecessor's lock instead of losing the race quietly.
const RestartFlag = "-restart"

// FormatLockRecord is what a hivebar writes into LockFile once it holds it.
func FormatLockRecord(pid int, buildID string) string {
	return fmt.Sprintf("%d %s\n", pid, buildID)
}

// ParseLockRecord reads FormatLockRecord's output.
//
// A record with only a pid — written by a hivebar that predates the build
// id — parses with an empty buildID, which never matches a real build, so
// that holder is treated as stale. That is the upgrade path this exists for.
func ParseLockRecord(b []byte) (pid int, buildID string, ok bool) {
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0, "", false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return 0, "", false
	}
	if len(fields) > 1 {
		buildID = fields[1]
	}
	return pid, buildID, true
}
