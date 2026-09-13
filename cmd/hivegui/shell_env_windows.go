//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lucascaro/hive/internal/proc"
)

// Windows counterparts to the build-environment helpers in
// shell_env_darwin.go.
//
// The darwin versions cannot be reused, and not only because that file
// is build-tagged: executableIn there decides a tool exists by testing
// st.Mode()&0o111, and Go reports mode 0 for the executable bit on
// Windows. Reusing it would report every build tool missing and refuse
// every latest-channel update.

// bashCandidates are the usual absolute locations of Git for Windows'
// bash, tried when PATH has none. A GUI launched from the Start menu
// routinely has a narrower PATH than the user's shell, and "bash not
// found" is a confusing way to report a Git install that is right where
// the installer put it.
var bashCandidates = []string{
	`C:\Program Files\Git\bin\bash.exe`,
	`C:\Program Files (x86)\Git\bin\bash.exe`,
	`C:\Program Files\Git\usr\bin\bash.exe`,
}

// wslBashDirs are the directories Windows puts a bash.exe in that starts
// a WSL distro rather than a Win32 bash: the System32 launcher and the
// Store app execution alias. Both sit on a default PATH, so on a machine
// with WSL installed LookPath finds one before Git for Windows' bash.
func wslBashDirs() []string {
	var dirs []string
	if root := os.Getenv("SystemRoot"); root != "" {
		dirs = append(dirs, filepath.Join(root, "System32"))
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		dirs = append(dirs, filepath.Join(local, "Microsoft", "WindowsApps"))
	}
	return dirs
}

// isWSLBash reports whether p is one of those launchers.
//
// Neither can run build.sh. buildCommandLine cds to a Windows path, which
// inside a distro lives under /mnt, so the build dies with "cd:
// D:/git/hive: No such file or directory"; and a Linux bash could not
// produce a Windows binary even if the path did resolve.
func isWSLBash(p string) bool {
	dir := filepath.Clean(filepath.Dir(p))
	for _, d := range wslBashDirs() {
		if strings.EqualFold(dir, filepath.Clean(d)) {
			return true
		}
	}
	return false
}

// findBash locates the bash that will run build.sh.
func findBash() (string, error) {
	sawWSL := false
	if p, err := exec.LookPath("bash"); err == nil {
		if !isWSLBash(p) {
			return p, nil
		}
		sawWSL = true
	}
	for _, p := range bashCandidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	if sawWSL {
		return "", fmt.Errorf("build.sh needs Git for Windows' bash, but the only bash on the PATH launches a WSL distro, " +
			"which cannot reach the checkout by its Windows path and cannot build a Windows binary. " +
			"Install Git for Windows (which ships bash), then try again")
	}
	return "", fmt.Errorf("build.sh needs bash, which is not on the PATH and not in the usual Git for Windows locations. " +
		"Install Git for Windows (which ships bash), then try again")
}

// buildToolProbeFn is the seam tests replace so they need no real bash.
var buildToolProbeFn = probeBuildTools

// buildTools are what build.sh cannot run without. wails is deliberately
// absent: it is resolved by build.sh itself from the Go bin directory,
// and probing for it here would refuse builds that would have worked.
var buildTools = []string{"go", "npm"}

// probeBuildTools asks the login shell which build tools it can resolve,
// and returns the ones it cannot.
//
// The probe runs through the same `bash -lc` the build will use, which
// is the point: this process's PATH is not the PATH build.sh runs
// under, and checking ours would produce refusals that contradict what
// happens when the user runs build.sh by hand.
func probeBuildTools(ctx context.Context, bash, repo string) ([]string, error) {
	script := "for t in " + strings.Join(buildTools, " ") + "; do command -v \"$t\" >/dev/null 2>&1 || echo \"$t\"; done"
	cmd := proc.CommandContext(ctx, bash, "-lc", script)
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("probe build tools: %w", err)
	}
	var missing []string
	for _, line := range strings.Fields(string(out)) {
		missing = append(missing, line)
	}
	return missing, nil
}
