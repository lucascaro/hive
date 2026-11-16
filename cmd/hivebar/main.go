// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

// hivebar is Hive's macOS menu-bar agent: a status-bar item showing
// what the daemon is and what it is holding, with actions to reach it.
//
// It exists because the daemon had no surface of its own. hived
// outlives the GUI — it is spawned detached and has no idle exit — so
// with every window closed there was no way to see whether it was
// running, what version it was, what sessions it held, or to restart
// it.
//
// It is a pure wire client. No PTY, no registry writes, nothing read
// out of the state dir but its own lock file; everything it knows
// arrives on one control connection. That is the same rule the GUI
// obeys (DESIGN.md), and it is why this can be a separate process at
// all.
//
// Why a separate .app rather than a tray inside an existing binary:
// putting it in hived would pull AppKit and cgo into a headless daemon
// and break the Linux build; putting it in hivegui would make it
// vanish during a GUI reload and after a quit, which are exactly the
// moments it exists for. macOS also requires a bundle for a status
// item to appear at all, so `go run ./cmd/hivebar` shows nothing —
// build with ./build.sh. See README.md in this directory.
package main

import (
	_ "embed"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/systray"

	"github.com/lucascaro/hive/internal/buildinfo"
	"github.com/lucascaro/hive/internal/registry"
)

// iconTemplate is a template image: black plus alpha only, which macOS
// recolours for the menu bar's appearance. Shipping a coloured icon
// here is the classic way to end up invisible in dark mode.
//
// It lives in assets/ and not icon/ because the widely-used global
// gitignore rule for macOS's `Icon\r` files is a bare `Icon`, and
// macOS matches that case-insensitively against a directory named
// `icon`. The file was then silently skipped by `git add -A` and
// absent from `git status`, and the failure surfaced only as a CI
// build error on the embed.
//
//go:embed assets/hive-template.png
var iconTemplate []byte

// options is hivebar's command line.
type options struct {
	version bool
	restart bool
}

func parseFlags(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("hivebar", flag.ContinueOnError)
	fs.BoolVar(&o.version, "version", false, "print build identity and exit")
	fs.BoolVar(&o.restart, "restart", false,
		"started by Restart Menu Bar: wait for the previous instance to release the lock")
	return o, fs.Parse(args)
}

// lockWait is how long this instance waits for the singleton lock.
func (o options) lockWait() time.Duration {
	if o.restart {
		return restartLockWait
	}
	return 0
}

func main() {
	opts, err := parseFlags(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	if opts.version {
		id := buildinfo.CurrentIdentity()
		log.SetFlags(0)
		log.SetOutput(os.Stdout)
		log.Printf("hivebar %s (build %s, daemon contract %d)",
			id.Release, id.BuildID, id.DaemonContract)
		log.Print(strings.TrimSuffix(buildinfo.LegalNotice(), "\n"))
		return
	}

	// A bundled agent has no terminal, so its stderr goes nowhere.
	// Tee to the state dir beside hived.log — the same reasoning that
	// put hived's log there.
	if f, err := registry.OpenLogFile("hivebar.log"); err == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, f))
		log.Printf("hivebar: log tee to %s",
			filepath.Join(registry.StateDir(), "hivebar.log"))
	}

	// Exit quietly, not loudly: both hived and hivegui spawn hivebar
	// best-effort on boot and launchd may have started it at login, so
	// losing this race is the normal case, not an error worth a dialog
	// or a non-zero status.
	release, ok := claimSingleton(opts.lockWait())
	if !ok {
		log.Printf("hivebar: another instance holds the lock; exiting")
		return
	}
	defer release()

	menu := NewMenu()
	client := NewClient(menu.Update)
	menu.Attach(client)

	// The dictation capture HUD (spec 553): a native panel, the Carbon
	// hotkey that opens it, and the menu item that names that hotkey.
	hud := NewHUD(nativeView{}, client)
	dict := NewDictation(nativeRegistrar{}, menu.SetDictation)
	setNativeTargets(hud, dict)
	client.AttachHUD(hud, dict)
	menu.AttachHUD(hud)

	go client.Run()

	// systray.Run must be called from main: the package locks the OS
	// thread itself, and Go guarantees main starts on the initial one —
	// which is the thread AppKit requires.
	systray.Run(menu.Ready, func() {
		log.Printf("hivebar: exiting")
	})
}
