//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// These mirror restart_windows_test.go. They exist because the unix
// path had zero coverage while carrying three branches that return
// nil without killing anything — the class of bug that let "Restart
// Hive" relaunch into the daemon it was supposed to replace.

func writePidfile(t *testing.T, pid int) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "s")
	if err := os.WriteFile(sock+".pid", []byte(strconv.Itoa(pid)), 0o600); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	return sock
}

// sleeper starts a long-lived child we own, so tests can signal a
// real pid without touching anything else on the machine. The
// returned channel closes when the child is reaped — the only
// reliable exit signal here, since signal(0) keeps succeeding on an
// unreaped zombie (the same trap that broke waitForExit in
// production).
func sleeper(t *testing.T) (int, <-chan struct{}) {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	reaped := make(chan struct{})
	go func() { _ = cmd.Wait(); close(reaped) }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd.Process.Pid, reaped
}

func stubSeams(t *testing.T, looksLikeHived bool, exits bool) {
	t.Helper()
	origName, origWait := looksLikeHivedFn, waitForExitFn
	looksLikeHivedFn = func(int) bool { return looksLikeHived }
	waitForExitFn = func(*os.Process, time.Duration) bool { return exits }
	t.Cleanup(func() { looksLikeHivedFn, waitForExitFn = origName, origWait })
}

func TestKillRunningHived_NoPidfile(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s")
	if err := killRunningHived(sock); err != nil {
		t.Fatalf("missing pidfile should be a no-op, got %v", err)
	}
}

func TestKillRunningHived_InvalidPid(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s")
	if err := os.WriteFile(sock+".pid", []byte("not-a-pid"), 0o600); err != nil {
		t.Fatalf("write pidfile: %v", err)
	}
	if err := killRunningHived(sock); err == nil {
		t.Fatal("unparseable pidfile should error, got nil")
	}
}

func TestKillRunningHived_DeadPidNoError(t *testing.T) {
	// A pid we know is gone: start a child and reap it.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run true: %v", err)
	}
	sock := writePidfile(t, cmd.Process.Pid)

	if err := killRunningHived(sock); err != nil {
		t.Fatalf("dead pid should be a no-op, got %v", err)
	}
}

func TestKillRunningHived_StalePidfileRemoved(t *testing.T) {
	pid, reaped := sleeper(t)
	sock := writePidfile(t, pid)
	stubSeams(t, false, true) // alive, but not a hived

	if err := killRunningHived(sock); err != nil {
		t.Fatalf("recycled pid should be a no-op, got %v", err)
	}
	if _, err := os.Stat(sock + ".pid"); !os.IsNotExist(err) {
		t.Errorf("stale pidfile should have been removed, stat err = %v", err)
	}
	// And the innocent bystander must still be running.
	select {
	case <-reaped:
		t.Errorf("unrelated pid %d was killed", pid)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestKillRunningHived_StillAliveAfterKillErrors(t *testing.T) {
	pid, _ := sleeper(t)
	sock := writePidfile(t, pid)
	stubSeams(t, true, false) // a hived that never appears to exit

	err := killRunningHived(sock)
	if err == nil {
		t.Fatal("a process that outlives SIGKILL must be reported, got nil")
	}
}

func TestKillRunningHived_HappyPath(t *testing.T) {
	pid, reaped := sleeper(t)
	sock := writePidfile(t, pid)
	stubSeams(t, true, true)

	if err := killRunningHived(sock); err != nil {
		t.Fatalf("killRunningHived: %v", err)
	}
	// SIGTERM was really delivered — `sleep` dies on it.
	select {
	case <-reaped:
	case <-time.After(2 * time.Second):
		t.Fatalf("pid %d still alive; SIGTERM was not delivered", pid)
	}
}
