//go:build !windows

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Package-level seams so tests can drive the branches without
// shelling out or needing a real hived, mirroring probeFn in
// restart_windows.go.
var (
	looksLikeHivedFn = pidLooksLikeHived
	waitForExitFn    = waitForExit
)

// killRunningHived sends SIGTERM to the hived recorded in <sock>.pid,
// waits for it to exit, and escalates to SIGKILL on a 3s budget.
//
// This is the *fallback* kill channel. RestartDaemon prefers an
// in-band FrameShutdown over the control conn, because everything
// below depends on the pidfile being present and accurate — and a
// nil return here does NOT mean the socket is free. Three branches
// return nil having killed nothing (no pidfile, unrecognised process
// name, already-gone pid), which is exactly how a "restarted" GUI
// ended up reconnecting to the daemon it meant to replace. The caller
// probes the socket afterwards and treats that as the truth.
//
// The pidfile is scoped to the socket the daemon owns (sibling file,
// "<sock>.pid"). That way a user running a second hived with a custom
// --socket can't be accidentally signaled by the GUI's restart action,
// which only ever talks to the default socket.
//
// The unrecognised-name branch is the safety-critical one: the OS
// hands recycled pids to editors, shells, anything; we must not
// SIGTERM them.
func killRunningHived(sock string) error {
	pidPath := sock + ".pid"
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("hivegui: kill hived: no pidfile at %s; nothing to signal", pidPath)
			return nil
		}
		return fmt.Errorf("read pidfile: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 1 {
		return fmt.Errorf("invalid pid in %s: %q", pidPath, raw)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find pid %d: %w", pid, err)
	}
	// Probe: signal-0 returns nil if alive, ESRCH if gone, EPERM if
	// the pid belongs to another user. A stale pidfile whose pid was
	// recycled is the "alive but not hived" case — guard with a comm
	// check below before any destructive signal.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		log.Printf("hivegui: kill hived: pid %d already gone", pid)
		return nil // already gone, nothing to do
	}
	if !looksLikeHivedFn(pid) {
		// Stale pidfile pointing at a recycled, unrelated pid. Drop
		// the file and bail; do NOT signal an unknown process.
		log.Printf("hivegui: kill hived: pid %d is not a hived; removing stale pidfile %s", pid, pidPath)
		_ = os.Remove(pidPath)
		return nil
	}
	log.Printf("hivegui: kill hived: SIGTERM pid %d", pid)

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("signal pid %d: %w", pid, err)
	}
	if waitForExitFn(proc, 3*time.Second) {
		return nil
	}
	// Still alive after 3s — escalate, then wait for the kernel to
	// reap it so the caller's reconnect doesn't race the dying socket.
	log.Printf("hivegui: kill hived: pid %d survived SIGTERM, escalating to SIGKILL", pid)
	_ = proc.Signal(syscall.SIGKILL)
	if !waitForExitFn(proc, 2*time.Second) {
		// Mirrors the Windows path, which has always reported this.
		// Note the caller does not rely on this error alone: it probes
		// the socket either way, because a zombie child (hived is
		// spawned by the GUI and never Wait()ed on) keeps answering
		// signal(0) forever while holding no socket.
		return fmt.Errorf("pid %d still alive 2s after SIGKILL", pid)
	}
	return nil
}

// pidLooksLikeHived returns true if pid is currently running and its
// process name (basename of argv0) is "hived". Uses ps because
// /proc/<pid>/comm is Linux-only; ps -o comm= works on darwin and
// linux. Returns false on any error so the caller can stay
// conservative about who it signals.
func pidLooksLikeHived(pid int) bool {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return false
	}
	name := strings.TrimSpace(string(out))
	// ps -o comm may print the full path; take the basename.
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return name == "hived"
}

func waitForExit(proc *os.Process, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
