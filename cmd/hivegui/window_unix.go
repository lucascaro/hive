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
			if cwd != "" {
				cmd.Dir = cwd
			}
			return cmd.Start()
		}
	}
	cmd := exec.Command(self)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

// enclosingAppBundle walks up from a Mach-O path looking for an
// ".app" ancestor whose Contents/MacOS contains it. Returns "" when
// the binary isn't inside a bundle (dev builds).
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
