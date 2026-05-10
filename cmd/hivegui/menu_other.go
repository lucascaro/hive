//go:build !darwin

package main

import (
	"github.com/wailsapp/wails/v2/pkg/menu"
)

// buildAppMenu returns nil on non-darwin platforms.
//
// On macOS the menu owns the visible menu bar and its accelerators are
// the canonical source of every keyboard shortcut. On Windows and
// Linux the same accelerators double-fire alongside the in-window JS
// keyboard listener — Ctrl+G toggled grid twice (back to single),
// Ctrl+Down/Up advanced two steps and felt reversed, etc. The native
// menu adds no user-facing value on those platforms (Wails surfaces it
// only as a hidden accelerator table), so we drop it and let the JS
// keyboard handler in cmd/hivegui/frontend/src/main.js own every
// shortcut. Adding a new shortcut on Windows/Linux is therefore a
// frontend-only change.
func buildAppMenu(_ *App) *menu.Menu { return nil }
