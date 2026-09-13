//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeStubExe puts something LookPath and os.Stat will accept where a
// bash is expected. Nothing here ever runs it.
func writeStubExe(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// Windows ships two bash.exe launchers that start a WSL distro rather
// than a Win32 bash: the System32 one and the Store app execution alias
// in WindowsApps. Both sit on a default PATH, so LookPath finds one
// before Git for Windows' bash on any machine with WSL installed.
//
// Handing build.sh to either is a dead end. The build command cds to a
// Windows path (`cd D:/git/hive`), which inside a distro is
// /mnt/d/git/hive, so the update dies with
//
//	build.sh failed: /bin/bash: line 1: cd: D:/git/hive: No such file or directory
//
// and a Linux bash could not produce a Windows binary even if the path
// resolved. Skip them and use the Git Bash the command line is written
// for.
func TestFindBashSkipsWSLLaunchers(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string
		rel  []string
	}{
		{"store app execution alias", "LOCALAPPDATA", []string{"Microsoft", "WindowsApps", "bash.exe"}},
		{"system32 launcher", "SystemRoot", []string{"System32", "bash.exe"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			wsl := filepath.Join(append([]string{root}, tc.rel...)...)
			writeStubExe(t, wsl)
			t.Setenv(tc.env, root)
			t.Setenv("PATH", filepath.Dir(wsl))

			gitBash := filepath.Join(t.TempDir(), "bash.exe")
			writeStubExe(t, gitBash)
			restore := bashCandidates
			bashCandidates = []string{gitBash}
			t.Cleanup(func() { bashCandidates = restore })

			got, err := findBash()
			if err != nil {
				t.Fatalf("findBash: %v", err)
			}
			if got != gitBash {
				t.Errorf("findBash = %q, want the Git Bash candidate %q", got, gitBash)
			}
		})
	}
}

// With no Git Bash to fall back to, "bash is not on the PATH" is a lie
// that sends the user looking for a bash they already have. Name WSL as
// the reason instead.
func TestFindBashExplainsAWSLOnlyPath(t *testing.T) {
	root := t.TempDir()
	wsl := filepath.Join(root, "Microsoft", "WindowsApps", "bash.exe")
	writeStubExe(t, wsl)
	t.Setenv("LOCALAPPDATA", root)
	t.Setenv("PATH", filepath.Dir(wsl))

	restore := bashCandidates
	bashCandidates = nil
	t.Cleanup(func() { bashCandidates = restore })

	_, err := findBash()
	if err == nil {
		t.Fatal("findBash = nil error with only a WSL bash reachable, want a refusal")
	}
	if !strings.Contains(err.Error(), "WSL") {
		t.Errorf("error = %q, want it to name WSL as the reason", err)
	}
}

// A real Git Bash on the PATH is still preferred over the hardcoded
// candidate list — the skip must be specific to the WSL launchers.
func TestFindBashUsesBashOnPath(t *testing.T) {
	dir := t.TempDir()
	onPath := filepath.Join(dir, "bash.exe")
	writeStubExe(t, onPath)
	t.Setenv("PATH", dir)

	restore := bashCandidates
	bashCandidates = nil
	t.Cleanup(func() { bashCandidates = restore })

	got, err := findBash()
	if err != nil {
		t.Fatalf("findBash: %v", err)
	}
	if got != onPath {
		t.Errorf("findBash = %q, want the bash on PATH %q", got, onPath)
	}
}

// The launcher is usually the FIRST bash on the PATH, because
// WindowsApps sits near the front of it. Giving up on the rest of the
// PATH at that point strands anyone whose Git for Windows is not at the
// default location - a scoop shim, a portable Git, an install on D: -
// and tells them to install the Git they already have. That is the same
// confusing refusal the WSL skip exists to prevent, just moved.
func TestFindBashSkipsWSLLauncherEarlierOnPath(t *testing.T) {
	root := t.TempDir()
	wsl := filepath.Join(root, "Microsoft", "WindowsApps", "bash.exe")
	writeStubExe(t, wsl)
	t.Setenv("LOCALAPPDATA", root)

	gitDir := t.TempDir()
	gitBash := filepath.Join(gitDir, "bash.exe")
	writeStubExe(t, gitBash)

	// Launcher first, the real thing second.
	t.Setenv("PATH", filepath.Dir(wsl)+string(os.PathListSeparator)+gitDir)

	restore := bashCandidates
	bashCandidates = nil // no Git for Windows at any default location
	t.Cleanup(func() { bashCandidates = restore })

	got, err := findBash()
	if err != nil {
		t.Fatalf("findBash: %v", err)
	}
	if got != gitBash {
		t.Errorf("findBash = %q, want the bash further along PATH %q", got, gitBash)
	}
}
