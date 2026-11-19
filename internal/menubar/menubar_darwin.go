// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

// Package menubar starts Hive's macOS menu-bar agent when it is not
// already running, and replaces one left running from a different build.
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
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/lucascaro/hive/internal/buildinfo"
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
	//
	// Plain `open -a` only activates a hivebar that is already running,
	// so after an update the old build would keep the menu bar until
	// logout. A holder of a different build is replaced instead: launch
	// a new instance that waits for the lock (the `-n … -restart` shape
	// Restart Menu Bar uses), and only once `open` succeeded, SIGTERM the
	// old one — the kernel then drops its flock. Killing first would
	// leave no menu bar at all if the launch failed.
	lock := filepath.Join(registry.StateDir(), LockFile)
	stalePid, stale := staleHolder(lock, buildinfo.BuildID())
	stale = stale && isHivebar(stalePid)
	cmd := exec.Command("open", openArgs(app, stale)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		log.Printf("menubar: start %s: %v", app, err)
		return
	}
	// Reap it: `open` returns once LaunchServices has the launch, and an
	// unwaited child of a long-lived daemon is a zombie for the life of
	// the process. Waiting here, off the caller's path, is also what
	// tells a stale replacement that the new instance really launched.
	go func() { afterOpen(cmd.Wait(), stale, stalePid, syscall.Kill) }()
}

// openArgs is the `open` command line. A replacement needs -n (plain -a
// only activates the running instance) and RestartFlag (so the new one
// waits for the old one's lock instead of losing the race).
func openArgs(app string, replace bool) []string {
	if replace {
		return []string{"-n", "-a", app, "--args", RestartFlag}
	}
	return []string{"-a", app}
}

// afterOpen stops the replaced hivebar, and only once `open` succeeded:
// stopping it after a failed launch would leave no menu bar at all.
func afterOpen(openErr error, replace bool, pid int, kill func(int, syscall.Signal) error) {
	if openErr != nil || !replace {
		return
	}
	log.Printf("menubar: replacing hivebar pid %d (different build)", pid)
	if err := kill(pid, syscall.SIGTERM); err != nil {
		log.Printf("menubar: stop stale hivebar pid %d: %v", pid, err)
	}
}

// staleHolder reports the pid recorded in the lock file when its build
// is not selfBuild.
//
// It does not probe the flock: even a non-blocking shared lock, held for
// an instant, makes a hivebar starting at that moment lose its claim and
// exit. Whether the pid is still a live hivebar is isHivebar's call.
func staleHolder(lockPath, selfBuild string) (pid int, stale bool) {
	b, err := os.ReadFile(lockPath)
	if err != nil {
		return 0, false
	}
	pid, build, ok := ParseLockRecord(b)
	if !ok || build == selfBuild {
		return 0, false
	}
	return pid, true
}

// isHivebar guards the SIGTERM: the record may be a dead instance's,
// and its pid recycled. Never signal a process that is not hivebar.
func isHivebar(pid int) bool {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp.Proc.P_pid != int32(pid) {
		return false
	}
	comm := kp.Proc.P_comm[:]
	if i := bytes.IndexByte(comm, 0); i >= 0 {
		comm = comm[:i]
	}
	return string(comm) == "hivebar"
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
