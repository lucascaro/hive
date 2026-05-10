//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

// spawnNewGUI launches another instance of the GUI as a detached
// child so the relaunched window is independent of this (about-to-
// quit) process.
//
// On darwin, when we're running inside a .app bundle we relaunch via
// `open -n <bundle.app>`. Re-execing the inner Mach-O directly works
// for spawning a process but doesn't go through LaunchServices, so
// the new instance doesn't get proper activation: no Dock focus, and
// in practice the Wails/WebKit window never appears. `-n` forces a
// new instance even if LS still sees the dying parent.
//
// For dev builds (binary outside a .app, e.g. `wails dev` or `go
// run`) we fall back to re-execing the binary directly.
func spawnNewGUI(cwd string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		if app := enclosingAppBundle(self); app != "" {
			cmd := exec.Command("open", "-n", app)
			// LaunchServices resets the bundle's cwd to "/", so
			// cmd.Dir doesn't reach the relaunched GUI. Propagate
			// the desired launch dir via an env var that
			// resolveLaunchDir() picks up on startup. `open`
			// inherits env to the launched bundle on macOS.
			if cwd != "" {
				cmd.Env = append(os.Environ(), "HIVE_LAUNCH_DIR="+cwd)
			}
			return startDetached(cmd, cwd)
		}
	}
	cmd := exec.Command(self)
	return startDetached(cmd, cwd)
}

// startDetached applies the shared detachment + stdio nulling so the
// child process is independent of this (about-to-quit) parent. Both
// the `open -n <bundle>` branch and the dev re-exec branch route
// through here to keep the contract consistent.
func startDetached(cmd *exec.Cmd, cwd string) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

// enclosingAppBundle walks up from a Mach-O path and returns the
// first ancestor directory whose name ends in ".app". Returns "" when
// no such ancestor exists within a few levels (dev builds where the
// binary lives outside a bundle).
func enclosingAppBundle(exe string) string {
	dir := filepath.Dir(exe)
	for i := 0; i < 6 && dir != "/" && dir != "."; i++ {
		if strings.HasSuffix(dir, ".app") {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return ""
}
