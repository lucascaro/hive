//go:build !darwin

package main

import "os/exec"

// lookEditorPath resolves an editor launcher on PATH. Only macOS needs
// the login-shell dance (see editor_path_darwin.go); elsewhere the GUI
// process inherits the user's PATH.
func lookEditorPath(name string) (string, error) {
	// exec.LookPath already returns an explicit path as-is when it is
	// executable, so unlike the darwin variant this needs no special
	// case.
	p, err := exec.LookPath(name)
	if err != nil {
		return "", errEditorNotFound
	}
	return p, nil
}
