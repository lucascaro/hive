package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// OpenTerminalAt launches the user's OS terminal application with its
// working directory set to path. If path is empty, falls back to the
// app's launch directory. Best-effort: returns an error if no
// suitable terminal can be located on this platform.
//
// macOS: opens the user's default Terminal.app via `open -a Terminal`.
// Linux: tries $TERMINAL, then x-terminal-emulator, then a fixed list
// of common emulators.
// Windows: opens Windows Terminal (wt.exe) if present, else cmd.exe.
func (a *App) OpenTerminalAt(path string) error {
	if path == "" {
		path = a.launchDir
	}
	if path == "" {
		return errors.New("no working directory to open")
	}
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		return fmt.Errorf("not a directory: %s", path)
	}
	return openOSTerminal(path)
}

func openOSTerminal(dir string) error {
	switch runtime.GOOS {
	case "darwin":
		// `open -a Terminal <dir>` opens a new Terminal.app window
		// with cwd set to dir. Honors the user's default shell.
		cmd := exec.Command("open", "-a", "Terminal", dir)
		return cmd.Start()
	case "windows":
		// Prefer Windows Terminal; fall back to cmd.exe.
		if wt, err := exec.LookPath("wt.exe"); err == nil {
			return exec.Command(wt, "-d", dir).Start()
		}
		c := exec.Command("cmd.exe", "/C", "start", "", "cmd.exe", "/K")
		c.Dir = dir
		return c.Start()
	default:
		// Linux / BSD: pick the first emulator we can find.
		candidates := []string{
			os.Getenv("TERMINAL"),
			"x-terminal-emulator",
			"gnome-terminal",
			"konsole",
			"xfce4-terminal",
			"alacritty",
			"kitty",
			"wezterm",
			"foot",
			"xterm",
		}
		for _, name := range candidates {
			if name == "" {
				continue
			}
			bin, err := exec.LookPath(name)
			if err != nil {
				continue
			}
			c := exec.Command(bin)
			c.Dir = dir
			return c.Start()
		}
		return errors.New("no terminal emulator found (set $TERMINAL)")
	}
}
