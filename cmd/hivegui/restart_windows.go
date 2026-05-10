//go:build windows

package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// probeFn is the package-level seam used by killRunningHived so tests
// can stub the tasklist invocation without shelling out. Production
// code points it at runTasklistProbe.
var probeFn = runTasklistProbe

// killRunningHived terminates the hived recorded in <sock>.pid and
// waits for it to actually exit so the caller can rely on the
// "killRunningHived returned nil ⇒ socket is free" contract that the
// unix sibling (restart_unix.go) provides.
//
// On Windows there is no in-band soft-kill for a process spawned with
// DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP and no console control
// handler installed (see spawn_windows.go), so this is a hard kill via
// TerminateProcess (os.Process.Kill on Windows).
//
// Safety: like restart_unix.go, the pidfile is scoped to the socket
// the daemon owns (sibling file, "<sock>.pid") so a second hived with
// a custom --socket can't be accidentally killed. Before terminating,
// the recorded pid is verified to currently be running as hived.exe
// via tasklist; if it is not (recycled pid), the stale pidfile is
// removed and no kill is performed.
//
// Returns nil if the pidfile is missing, the recorded pid no longer
// exists, or the recorded pid does not look like a hived. Returns a
// non-nil error if tasklist itself cannot be invoked (PATH/exec
// failure) or if the killed process does not exit within the budget —
// surfacing those keeps the GUI from silently relaunching while the
// previous daemon is still bound to the socket.
func killRunningHived(sock string) error {
	pidPath := sock + ".pid"
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

	alive, isHived, err := probeFn(pid)
	if err != nil {
		return fmt.Errorf("probe pid %d: %w", pid, err)
	}
	if !alive {
		return nil // already gone
	}
	if !isHived {
		// Stale pidfile pointing at a recycled, unrelated pid.
		_ = os.Remove(pidPath)
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find pid %d: %w", pid, err)
	}
	if err := proc.Kill(); err != nil {
		// ESRCH-equivalent races are fine; the process is gone.
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return fmt.Errorf("terminate pid %d: %w", pid, err)
	}
	if !waitForExitWindows(pid, 3*time.Second) {
		return fmt.Errorf("pid %d still alive 3s after TerminateProcess", pid)
	}
	return nil
}

// runTasklistProbe reports whether pid is currently running and
// whether its image name is hived.exe. Uses the built-in tasklist
// utility (ships with every Windows install) for symmetry with the
// unix implementation's `ps -o comm=` probe.
//
// Distinguishes "tasklist couldn't run" (returns err) from "tasklist
// ran and found no matching pid" (returns alive=false, err=nil). The
// caller cares about the difference: a missing pid means the daemon
// is already dead; an exec failure means we can't tell, so we must
// not silently proceed as if the socket were free.
func runTasklistProbe(pid int) (alive, hived bool, err error) {
	cmd := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/FO", "CSV", "/NH")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return false, false, fmt.Errorf("exec tasklist: %w", err)
	}
	a, h := parseTasklistRow(string(out))
	return a, h, nil
}

// parseTasklistRow extracts (alive, isHived) from tasklist CSV output.
// When the pid does not exist tasklist prints either an empty body or
// "INFO: No tasks are running which match the specified criteria." on
// stdout. When it exists the row is: "image","pid","session","sess#","mem".
// Split out for unit testing without invoking tasklist.
func parseTasklistRow(out string) (alive, hived bool) {
	out = strings.TrimSpace(out)
	if out == "" || strings.HasPrefix(strings.ToUpper(out), "INFO:") {
		return false, false
	}
	r := csv.NewReader(strings.NewReader(out))
	r.FieldsPerRecord = -1
	rec, err := r.Read()
	if err != nil || len(rec) == 0 {
		return false, false
	}
	image := strings.ToLower(strings.TrimSpace(rec[0]))
	// tasklist appends .exe on Windows; basename in case future tasklist
	// variants prepend a path.
	if i := strings.LastIndexAny(image, `\/`); i >= 0 {
		image = image[i+1:]
	}
	return true, image == "hived.exe"
}

// waitForExitWindows polls until pid is gone or the budget elapses.
// Mirrors the unix waitForExit helper. Errors from the probe are
// treated as "still alive, keep polling" — the post-kill wait is best
// effort and the caller ultimately surfaces a timeout if the deadline
// passes.
func waitForExitWindows(pid int, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		alive, _, err := probeFn(pid)
		if err == nil && !alive {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
