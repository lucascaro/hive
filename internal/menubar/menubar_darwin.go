//go:build darwin

// Package menubar starts Hive's macOS menu-bar agent when it is not
// already running.
//
// It lives in internal/ rather than in either binary because both need
// it and neither owns it: hived starts hivebar so the menu bar exists
// exactly as long as the daemon it reports on, and hivegui starts it
// too so a user who launches the GUI on a machine where the daemon was
// already up still gets one.
//
// Racing is expected, not exceptional — hivebar's own flock decides who
// wins (cmd/hivebar/singleton.go), so both callers can fire blindly.
package menubar

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/lucascaro/hive/internal/registry"
)

// Spawn starts hivebar detached, best-effort.
//
// Everything here is a soft failure: a missing binary, a dev tree with
// no bundle, a exec that fails. The menu bar is a convenience and its
// absence must never stop a daemon from serving or a GUI from opening.
// Errors are logged, never returned — a caller that could not act on
// one would only be forced to ignore it at its own call site instead.
func Spawn() {
	if os.Getenv("HIVE_NO_MENUBAR") != "" {
		return
	}
	// An isolated daemon (scripts/dev-iso.sh, the e2e-real harness,
	// anything with HIVE_STATE_DIR set) must not touch the user's menu
	// bar. hivebar locks and reports against the default socket and
	// state dir, so one started from an isolated run would either fight
	// the real one for the lock or, worse, win it and start reporting
	// the wrong daemon. Same guard, and the same reasoning, as the
	// orphan-worktree reclaim in internal/daemon.
	if registry.StateDirOverridden() {
		return
	}
	app, err := locate()
	if err != nil {
		log.Printf("menubar: not started: %v", err)
		return
	}
	// `open`, not exec of the inner binary: macOS will not give a
	// status item to a process LaunchServices did not start as a
	// bundle, so running Contents/MacOS/hivebar directly produces a
	// silent no-op that looks exactly like a crash.
	cmd := exec.Command("open", "-a", app)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		log.Printf("menubar: start %s: %v", app, err)
		return
	}
	// Reap it: `open` returns immediately, and an unwaited child of a
	// long-lived daemon is a zombie for the life of the process.
	go func() { _ = cmd.Wait() }()
}

// locate finds hivebar.app.
//
// Lookup order mirrors cmd/hivegui/locate.go: an explicit override
// first (dev and packaging), then the bundle layout, then a sibling
// directory for a plain `go build` tree.
func locate() (string, error) {
	if p := os.Getenv("HIVEBAR"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	if app := enclosingAppBundle(self); app != "" {
		c := filepath.Join(app, "Contents", "Library", "LoginItems", "hivebar.app")
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	c := filepath.Join(filepath.Dir(self), "hivebar.app")
	if _, err := os.Stat(c); err == nil {
		return c, nil
	}
	return "", os.ErrNotExist
}

func enclosingAppBundle(p string) string {
	for dir := filepath.Dir(p); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if strings.HasSuffix(dir, ".app") {
			return dir
		}
	}
	return ""
}
