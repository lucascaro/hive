package main

import (
	"os"

	"github.com/lucascaro/hive/internal/proc"
	"golang.org/x/sys/unix"
)

// openDefault hands the file to Launch Services. Every path reaching
// here is absolute and cleaned (resolvePath), so it can never be read
// as an option, and it is never launchable (OpenFile checked first).
func openDefault(path string) error {
	return proc.Command("open", path).Run()
}

// reveal selects the file in Finder instead of opening it. This is
// where a launchable file, a bundle and a directory all end up.
func reveal(path string) error {
	return proc.Command("open", "-R", path).Run()
}

// finderInfoAliasFlag is kIsAlias in the FinderInfo `finderFlags`
// field: a big-endian uint16 at bytes 8-9 of the com.apple.FinderInfo
// extended attribute.
const finderInfoAliasFlag = 0x8000

func statMeta(path string) (fileMeta, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileMeta{}, err
	}
	return fileMeta{
		isDir:   info.IsDir(),
		execBit: !info.IsDir() && info.Mode().Perm()&0o111 != 0,
		isAlias: isFinderAlias(path),
	}, nil
}

// isFinderAlias reports whether path is a Finder alias file. An alias
// is not a symlink — EvalSymlinks does not follow it and Stat reports
// an ordinary small file — but `open` resolves it, so an alias to an
// application launches that application.
func isFinderAlias(path string) bool {
	buf := make([]byte, 32)
	n, err := unix.Getxattr(path, "com.apple.FinderInfo", buf)
	if err != nil || n < 10 {
		return false
	}
	flags := uint16(buf[8])<<8 | uint16(buf[9])
	return flags&finderInfoAliasFlag != 0
}

// guardPath is the name isLaunchable should judge. On darwin that is the
// path itself; the Windows build expands 8.3 short names here.
func guardPath(path string) string { return path }
