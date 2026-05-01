package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// locateHived finds the hived binary. Lookup order:
//  1. $HIVED env var (override for dev / packaging)
//  2. Sibling of the running GUI binary
//  3. macOS .app bundle: same Contents/MacOS dir as the GUI
//  4. PATH
//
// Returns an error with the searched paths if none hit.
func locateHived() (string, error) {
	if p := os.Getenv("HIVED"); p != "" {
		if isExecutable(p) {
			return p, nil
		}
	}
	// Resolve the GUI's own binary path.
	self, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(self)
		ext := ""
		if runtime.GOOS == "windows" {
			ext = ".exe"
		}
		candidate := filepath.Join(dir, "hived"+ext)
		if isExecutable(candidate) {
			return candidate, nil
		}
		// macOS app bundle: GUI is .../Contents/MacOS/<gui>; hived may be alongside.
		// Already covered by `dir` lookup above.
	}
	if p, err := exec.LookPath("hived"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("hived binary not found (looked at $HIVED, sibling of %s, and $PATH)", self)
}

func isExecutable(p string) bool {
	st, err := os.Stat(p)
	if err != nil {
		return false
	}
	if st.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true // assume any file is fine on Windows
	}
	return st.Mode()&0o111 != 0
}
