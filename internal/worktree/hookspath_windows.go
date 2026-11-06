// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build windows

package worktree

import (
	"os"
	"sync"
)

// noHooksDir is an empty directory for core.hooksPath. Windows has no
// device that can stand in for one; the directory is made once per
// process, in the user's own temp dir (private to them by ACL).
var noHooksDir = sync.OnceValues(func() (string, error) {
	return os.MkdirTemp("", "hive-nohooks-")
})

// noHooksPath is what safeGit hands core.hooksPath so no hook runs.
func noHooksPath() (string, error) { return noHooksDir() }
