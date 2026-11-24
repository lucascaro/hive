// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build !darwin && !windows

package dictation

// No dictation box on this platform (spec 575: macOS and Windows only).
func handyPaths() []string     { return nil }
func isExecutable(string) bool { return false }
func handyRunning() bool       { return false }
