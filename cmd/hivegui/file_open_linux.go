//go:build linux

package main

import (
	"os"
	"path/filepath"

	"github.com/lucascaro/hive/internal/proc"
)

func openDefault(path string) error {
	return proc.Command("xdg-open", path).Run()
}

// reveal asks the desktop's file manager to show the file, through the
// freedesktop FileManager1 interface every major file manager
// implements. Where that is missing (a bare WM, a session without
// D-Bus), fall back to opening the containing directory — less
// precise, but it still shows the user where the file lives.
func reveal(path string) error {
	err := proc.Command("dbus-send",
		"--session", "--print-reply",
		"--dest=org.freedesktop.FileManager1",
		"/org/freedesktop/FileManager1",
		"org.freedesktop.FileManager1.ShowItems",
		"array:string:file://"+path,
		"string:",
	).Run()
	if err == nil {
		return nil
	}
	return proc.Command("xdg-open", filepath.Dir(path)).Run()
}

func statMeta(path string) (fileMeta, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileMeta{}, err
	}
	return fileMeta{
		isDir:   info.IsDir(),
		execBit: !info.IsDir() && info.Mode().Perm()&0o111 != 0,
	}, nil
}

// guardPath is the name isLaunchable should judge. On linux that is the
// path itself; the Windows build expands 8.3 short names here.
func guardPath(path string) string { return path }
