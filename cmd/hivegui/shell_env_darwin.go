package main

import (
	"os"
	"strings"

	"github.com/lucascaro/hive/internal/proc"
)

// An app launched from Finder inherits only the system PATH from
// /etc/paths. That has no Homebrew arm64 prefix (/opt/homebrew/bin) and
// no Node version manager directory — fnm mints one per shell and nvm
// installs itself into a shell rc file — so ./build.sh dies with
// `exec: "npm": executable file not found in $PATH`. The fix is the one
// VS Code settled on: ask the user's own shell what its PATH is.
// The probe itself lives in internal/proc (LoginPATH).

// loginPATH is the login shell's PATH, resolved once per process by
// internal/proc (which the daemon shares, for ACP adapters).
var loginPATH = proc.LoginPATH

// loginPATHFn is the seam envWithLoginPATH reads through, mirroring
// extractZipFn and friends in update_apply_darwin.go. The probe result
// is cached for the life of the process, so a test cannot steer it by
// setting SHELL — the resolution itself is covered against a fake shell
// by internal/proc's resolveLoginPATH tests.
var loginPATHFn = loginPATH

// git resolves on the same PATH build.sh gets, so a Spotlight launch and
// a terminal launch run the same git. See gitCommandFn.
func init() {
	gitCommandFn = func() (string, []string) {
		env := envWithLoginPATH(os.Environ())
		if bin := lookPathIn(pathOf(env), "git"); bin != "" {
			return bin, env
		}
		return "git", env
	}
}

// envWithLoginPATH replaces PATH in env with the login shell's. Only
// PATH: the rest of the login environment is the user's shell session,
// not this build's, and importing it would clobber the HIVE_* vars the
// app sets deliberately.
func envWithLoginPATH(env []string) []string {
	path := loginPATHFn()
	if path == "" {
		return env
	}
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "PATH=") {
			out = append(out, kv)
		}
	}
	return append(out, "PATH="+path)
}

// pathSourceDescription names where the PATH the build runs with came
// from, so a refusal points at the file the user has to edit. Without
// it the message blames the login shell even when $SHELL was unset and
// no shell was ever consulted.
func pathSourceDescription() string {
	shell := os.Getenv("SHELL")
	if shell == "" || loginPATHFn() == "" {
		return "this app inherited (no usable login shell to ask)"
	}
	return "reported by " + shell
}

// buildTools are what ./build.sh cannot run without and what actually
// goes missing under a Finder-launched PATH. lipo, git and the rest
// live in /usr/bin, which is on every PATH there is. Var so a test
// driving runBuildScript with a stub script can waive the check.
var buildTools = []string{"go", "npm"}

// missingBuildTools names the build tools that do not resolve on path.
// Checked up front so a PATH that is still wrong fails with something
// the user can act on, rather than as `build.sh failed` forty lines
// into npm's output.
func missingBuildTools(path string) []string {
	var missing []string
	for _, tool := range buildTools {
		if !executableIn(path, tool) {
			missing = append(missing, tool)
		}
	}
	return missing
}

// executableIn reports whether name resolves to an executable file on
// the given PATH.
func executableIn(path, name string) bool {
	return lookPathIn(path, name) != ""
}

// lookPathIn returns the file name resolves to on the given PATH, or "".
// Relative entries never win; see proc.LookPathIn.
func lookPathIn(path, name string) string { return proc.LookPathIn(path, name) }

// pathOf returns the PATH entry of an environment slice.
func pathOf(env []string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			return v
		}
	}
	return ""
}
