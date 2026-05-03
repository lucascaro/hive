//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lucascaro/hive/internal/registry"
)

// killRunningHived sends SIGTERM to the hived recorded in $STATE/hived.pid,
// waits for it to exit, and escalates to SIGKILL on a 3s budget. On
// SIGKILL, also waits for the process to actually exit so the caller
// can rely on "killRunningHived returned nil ⇒ socket is free."
//
// Returns nil if the pidfile is missing, the recorded pid no longer
// exists, or the recorded pid does not look like a hived (a stale
// pidfile whose pid was recycled to an unrelated user process). The
// last case is the safety-critical one: the OS hands recycled pids to
// editors, shells, anything; we must not SIGTERM them.
func killRunningHived(_ string) error {
	pidPath := filepath.Join(registry.StateDir(), "hived.pid")
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
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
		return nil // already gone, nothing to do
	}
	if !pidLooksLikeHived(pid) {
		// Stale pidfile pointing at a recycled, unrelated pid. Drop
		// the file and bail; do NOT signal an unknown process.
		_ = os.Remove(pidPath)
		return nil
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("signal pid %d: %w", pid, err)
	}
	if waitForExit(proc, 3*time.Second) {
		return nil
	}
	// Still alive after 3s — escalate, then wait for the kernel to
	// reap it so the caller's reconnect doesn't race the dying socket.
	_ = proc.Signal(syscall.SIGKILL)
	waitForExit(proc, 2*time.Second)
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
