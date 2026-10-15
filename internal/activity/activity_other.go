// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build !darwin

package activity

// No App Nap outside macOS; nothing to opt out of.
func disableThrottling() {}
