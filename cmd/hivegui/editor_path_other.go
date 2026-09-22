//go:build !darwin

package main

import "os/exec"

// lookEditorPath resolves an editor launcher on PATH. Only macOS needs
// the login-shell dance (see editor_path_darwin.go); elsewhere the GUI
// process inherits the user's PATH.
func lookEditorPath(name string) (string, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", errEditorNotFound
	}
	return p, nil
}
