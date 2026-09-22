package main

import (
	"os"

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
func reveal(path string) error {
	return proc.Command("explorer.exe", "/select,"+path).Start()
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
