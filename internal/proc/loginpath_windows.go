//go:build windows

package proc

import (
	"os"
	"os/exec"
	"path/filepath"
)

// LoginPATH is the process's own PATH on Windows. There is no login
// shell to ask: a Start-menu launch inherits the user's PATH from the
// registry, which is where Node's installer puts itself.
func LoginPATH() string { return os.Getenv("PATH") }

// LookPathIn returns the file name resolves to on the given PATH, or "".
// It tries the PATHEXT suffixes exec.LookPath would, because npx on
// Windows is npx.cmd.
func LookPathIn(path, name string) string {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		if p, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return p
		}
	}
	return ""
}
