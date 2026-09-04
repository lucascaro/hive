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

	"fyne.io/systray"

	"github.com/lucascaro/hive/internal/buildinfo"
	"github.com/lucascaro/hive/internal/registry"
)

// iconTemplate is a template image: black plus alpha only, which macOS
// recolours for the menu bar's appearance. Shipping a coloured icon
// here is the classic way to end up invisible in dark mode.
//
//go:embed icon/hive-template.png
var iconTemplate []byte

func main() {
	version := flag.Bool("version", false, "print build identity and exit")
	flag.Parse()
	if *version {
		id := buildinfo.CurrentIdentity()
		log.SetFlags(0)
		log.SetOutput(os.Stdout)
		log.Printf("hivebar %s (build %s, daemon contract %d)",
			id.Release, id.BuildID, id.DaemonContract)
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
	release, ok := claimSingleton()
	if !ok {
		log.Printf("hivebar: another instance holds the lock; exiting")
		return
	}
	defer release()

	menu := NewMenu()
	client := NewClient(menu.Update)
	menu.Attach(client)

	go client.Run()

	// systray.Run must be called from main: the package locks the OS
	// thread itself, and Go guarantees main starts on the initial one —
	// which is the thread AppKit requires.
	systray.Run(menu.Ready, func() {
		log.Printf("hivebar: exiting")
	})
}
