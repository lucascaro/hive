//go:build windows

package main

import (
	"strings"

	"golang.org/x/sys/windows"
)

// resolveDir returns the final path behind dir, following symlinks and
// junctions alike. filepath.EvalSymlinks cannot be used for this here:
// since Go 1.23 (winsymlink=1) a junction is reported as ModeIrregular,
// not ModeSymlink, so EvalSymlinks hands it back unresolved — and an
// unresolved junction is exactly what Wails' os.Lstat check refuses.
// GetFinalPathNameByHandle is what the kernel itself resolves the
// reparse chain to.
func resolveDir(dir string) (string, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return "", err
	}
	// FILE_FLAG_BACKUP_SEMANTICS is required to open a directory handle.
	h, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	// The flags value 0 is FILE_NAME_NORMALIZED | VOLUME_NAME_DOS, which
	// x/sys/windows does not name.
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", err
	}
	if n >= uint32(len(buf)) {
		buf = make([]uint16, n+1)
		if n, err = windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0); err != nil {
			return "", err
		}
	}
	final := windows.UTF16ToString(buf[:n])
	// The API answers in the \\?\ namespace; give back what a user
	// would type. A UNC final path (\\?\UNC\srv\share) becomes \\srv\share.
	if strings.HasPrefix(final, `\\?\UNC\`) {
		return `\\` + final[len(`\\?\UNC\`):], nil
	}
	return strings.TrimPrefix(final, `\\?\`), nil
}
