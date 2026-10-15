// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build !darwin

// Package menubar is a no-op off macOS: the menu-bar agent is
// darwin-only (see menubar_darwin.go and cmd/hivebar). Keeping the
// package present on every platform means the callers need no build
// tags of their own.
package menubar

// Spawn does nothing on this platform.
func Spawn() {}
