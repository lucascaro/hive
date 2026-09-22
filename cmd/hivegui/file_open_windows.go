package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/lucascaro/hive/internal/proc"
	"golang.org/x/sys/windows"
)

// openDefault asks the shell to open the file with its registered
// handler. ShellExecuteW is a direct API call rather than a spawned
// command: `cmd /C start` would re-parse the path through cmd's own
// quoting rules, which is a second place for an attacker-chosen name
// to mean something.
func openDefault(path string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

// reveal opens Explorer with the file selected. The comma in
// "/select," is Explorer's own syntax, not a shell construct.
//
// The command line is built by hand because Explorer parses its own
// lpCommandLine rather than taking an argv. Go's default escaping
// quotes any argument containing a space, producing
// `explorer.exe "/select,C:\Users\John Smith\a.txt"` — Explorer does
// not recognise that shape, silently ignores the argument and opens
// the default folder. It needs the path quoted *inside* the token:
// `/select,"C:\..."`. Same SysProcAttr.CmdLine bypass as
// internal/session/spawn_windows.go.
//
// path arrives cleaned and absolute from resolvePath, and a '"' cannot
// appear in a Windows file name, so it is refused rather than escaped.
func reveal(path string) error {
	if strings.Contains(path, `"`) {
		return fmt.Errorf("refusing to reveal a path containing a quote: %s", path)
	}
	c := proc.Command("explorer.exe")
	c.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: `"` + c.Path + `" /select,"` + path + `"`,
	}
	return c.Start()
}

func statMeta(path string) (fileMeta, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileMeta{}, err
	}
	return fileMeta{isDir: info.IsDir()}, nil
}

// longPath expands an 8.3 short name (PAYLOA~1.SET) to its real name.
// The registered handler fires on the long name's extension, so the
// guard has to see that one: without this, payload.settingcontent-ms
// reads as ".set" and passes.
func longPath(path string) string {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return path
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetLongPathName(p, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return path
	}
	return windows.UTF16ToString(buf[:n])
}

// guardPath is the name isLaunchable should judge: the long one, since
// that is the extension whose handler fires.
func guardPath(path string) string { return longPath(path) }
