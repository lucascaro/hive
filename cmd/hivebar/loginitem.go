// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// The "Start at Login" item mirrors Settings → Start at login, which is
// SMAppService registration of this very bundle. SMAppService resolves the
// helper against the CALLING process's bundle, so hivebar cannot register
// itself; it asks hivegui, inside Hive.app, via a headless flag instead (see
// cmd/hivegui/loginitem_cli.go). A GUI that is not running is exactly the
// case this exists for, which is why it is not a wire command.

const (
	loginItemFlag     = "--menubar-login-item="
	loginItemSentinel = "hive-login-item: "

	// loginItemTimeout bounds every external call the menu makes. A hung
	// exec must not freeze a refresh forever, and an older hivegui that
	// ignores the flag would start a whole GUI that only this kill ends.
	loginItemTimeout = 5 * time.Second

	// loginItemsSettingsURL opens System Settings → General → Login Items,
	// where a registration waiting for approval is granted.
	loginItemsSettingsURL = "x-apple.systempreferences:com.apple.LoginItems-Extension"
)

// loginView is how the menu shows one login-item status.
type loginView struct {
	Visible bool
	Checked bool
	Title   string
	Tooltip string
}

// loginItemView maps a status (or a failed probe) to the menu item.
//
// Hidden unless the OS can act on it: "not-found" is a dev tree with no
// bundle, "unsupported" is macOS 12 and earlier, and a failed probe means
// the answer is unknown — a checkmark that guesses is worse than no item.
func loginItemView(status string, err error) loginView {
	if err != nil {
		return loginView{}
	}
	switch status {
	case "enabled":
		return loginView{Visible: true, Checked: true, Title: "Start at Login",
			Tooltip: "Click to stop starting the menu bar at login"}
	case "not-registered":
		return loginView{Visible: true, Title: "Start at Login",
			Tooltip: "Start the menu bar at login"}
	case "requires-approval":
		return loginView{Visible: true, Title: "Start at Login — Approve in System Settings…",
			Tooltip: "Opens System Settings → Login Items, where macOS waits for your approval"}
	default:
		return loginView{}
	}
}

// parseLoginStatus extracts the status from hivegui's sentinel line. Any
// output without it is a failed probe, never a guess.
func parseLoginStatus(out string) (string, error) {
	for _, line := range strings.Split(out, "\n") {
		if s, ok := strings.CutPrefix(strings.TrimSpace(line), loginItemSentinel); ok && s != "" {
			return s, nil
		}
	}
	return "", errors.New("hivegui did not answer the login-item probe")
}

func loginItemCall(run runner, gui, verb string) (string, error) {
	out, err := run(loginItemTimeout, gui, loginItemFlag+verb)
	if err != nil {
		return "", err
	}
	return parseLoginStatus(out)
}

// probeLoginItem reads the registration state.
func probeLoginItem(run runner, gui string) (string, error) {
	return loginItemCall(run, gui, "status")
}

// toggleLoginItem acts on a click given the status it was showing:
// enabled → disable, not-registered → enable, requires-approval → open System
// Settings (calling enable again would do nothing). It returns the status the
// menu should now show. After a failure that is the freshly re-read truth,
// not the cached one, so the checkmark never lies about the failed change.
func toggleLoginItem(run runner, gui, status string) (string, error) {
	var (
		next string
		err  error
	)
	switch status {
	case "enabled":
		next, err = loginItemCall(run, gui, "disable")
	case "not-registered":
		next, err = loginItemCall(run, gui, "enable")
	case "requires-approval":
		if _, err = run(loginItemTimeout, "open", loginItemsSettingsURL); err == nil {
			next, err = probeLoginItem(run, gui)
		}
	default:
		return status, fmt.Errorf("login item is %q; nothing to toggle", status)
	}
	if err != nil {
		if fresh, perr := probeLoginItem(run, gui); perr == nil {
			return fresh, err
		}
		return status, err
	}
	return next, nil
}
