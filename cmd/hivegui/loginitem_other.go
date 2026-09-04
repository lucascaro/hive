//go:build !darwin

package main

import "errors"

// MenuBarLoginItemStatus reports that this platform has no menu bar.
// See cmd/hivebar and loginitem_darwin.go.
func (a *App) MenuBarLoginItemStatus() string { return "unsupported" }

// SetMenuBarLoginItem always fails off macOS: there is no menu-bar
// agent to register.
func (a *App) SetMenuBarLoginItem(bool) error {
	return errors.New("the Hive menu bar is macOS-only")
}
