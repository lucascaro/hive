package main

import "os"

// lookEditorPath finds an editor's launcher on the user's *login*
// PATH, not the GUI process's. A bundled macOS app inherits a minimal
// PATH from launchd, so `code`, `cursor`, `zed` and `subl` — all of
// which live in /usr/local/bin or a user-managed prefix — are
// invisible to exec.LookPath here. Same reason and same helpers as
// gitCommandFn in shell_env_darwin.go.
func lookEditorPath(name string) (string, error) {
	if p := lookPathIn(pathOf(envWithLoginPATH(os.Environ())), name); p != "" {
		return p, nil
	}
	return "", errEditorNotFound
}
