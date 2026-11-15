// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"fmt"
	"io"
	"strings"
)

// menuBarLoginItemFlag is the headless entry point cmd/hivebar uses to read
// and flip its own login-item registration.
//
// SMAppService resolves the helper against the CALLING process's bundle
// (see loginitem_darwin.go), and hivebar is itself the helper, so it cannot
// register itself. hived cannot do it either: it must stay cgo-free. The one
// process that can is hivegui inside Hive.app, so hivebar runs it for a
// moment with this flag instead of asking a running GUI over the wire — a
// menu bar that is stuck is exactly when no GUI is open.
const menuBarLoginItemFlag = "--menubar-login-item="

// loginItemSentinel prefixes the one line the flag prints on stdout. A caller
// that sees anything else — an older hivegui that ignores the flag would start
// a full GUI instead — must treat the call as failed, not guess at the output.
const loginItemSentinel = "hive-login-item: "

// loginItemBackend is the part of *App the flag drives; a fake in tests.
type loginItemBackend interface {
	MenuBarLoginItemStatus() string
	SetMenuBarLoginItem(enable bool) error
}

// handleMenuBarLoginItemFlag runs the headless mode when args is exactly one
// --menubar-login-item=<status|enable|disable> argument. It reports whether
// it handled args and, if so, the process exit code.
//
// enable and disable print the status after the change, so the caller learns
// the truth in one exec rather than racing a second probe.
func handleMenuBarLoginItemFlag(args []string, stdout, stderr io.Writer, b loginItemBackend) (handled bool, code int) {
	if len(args) != 1 || !strings.HasPrefix(args[0], menuBarLoginItemFlag) {
		return false, 0
	}
	switch verb := strings.TrimPrefix(args[0], menuBarLoginItemFlag); verb {
	case "status":
	case "enable", "disable":
		if err := b.SetMenuBarLoginItem(verb == "enable"); err != nil {
			fmt.Fprintln(stderr, err)
			return true, 1
		}
	default:
		fmt.Fprintf(stderr, "unknown %s value %q (want status, enable or disable)\n",
			strings.TrimSuffix(menuBarLoginItemFlag, "="), verb)
		return true, 2
	}
	fmt.Fprintf(stdout, "%s%s\n", loginItemSentinel, b.MenuBarLoginItemStatus())
	return true, 0
}
