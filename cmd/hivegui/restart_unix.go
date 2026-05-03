//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lucascaro/hive/internal/registry"
)

// killRunningHived sends SIGTERM to the hived recorded in $STATE/hived.pid,
// then waits up to ~3s for it to exit (its socket disappears). If the
// pidfile is missing or stale, returns nil (nothing to kill).
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
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// ESRCH = already gone; treat as success.
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("signal pid %d: %w", pid, err)
	}
	for i := 0; i < 30; i++ {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			return nil // exited
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Still alive after 3s — escalate.
	_ = proc.Signal(syscall.SIGKILL)
	return nil
}
