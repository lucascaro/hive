// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build !windows

package worktree

import "os"

// noHooksPath is what safeGit hands core.hooksPath so no hook runs.
// /dev/null is not a directory, so <hooksPath>/<hook> can never exist,
// and unlike a temp dir there is nothing another local user could
// recreate under that name with a hook in it.
func noHooksPath() (string, error) { return os.DevNull, nil }
