// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build !darwin

package main

// HandyInstalled is false off macOS: the dictation HUD is macOS-only.
func (a *App) HandyInstalled() bool { return false }
