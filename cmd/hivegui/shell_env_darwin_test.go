package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The login-PATH probe runs `$SHELL -i -l -c`, which sources the
// developer's own rc file. Nothing in a test run has any business doing
// that, so the seam is pinned before any test runs rather than relying
// on each one to remember — the same floor TestMain gives the update
// seams, which cannot pin this one because it does not exist off
// darwin. stubLoginPATH overrides it where a test needs a real answer.
func init() {
	loginPATHFn = func() string { return "/usr/bin:/bin" }
}

// stubLoginPATH points the seam at a fixed answer. "" stands for a
// probe that failed.
func stubLoginPATH(t *testing.T, path string) {
	t.Helper()
	prev := loginPATHFn
	loginPATHFn = func() string { return path }
	t.Cleanup(func() { loginPATHFn = prev })
}

func TestEnvWithLoginPATHReplacesOnlyPATH(t *testing.T) {
	stubLoginPATH(t, "/opt/homebrew/bin:/usr/bin")
	got := envWithLoginPATH([]string{"PATH=/usr/bin:/bin", "FOO=bar"})
	want := []string{"FOO=bar", "PATH=/opt/homebrew/bin:/usr/bin"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("envWithLoginPATH = %v, want %v", got, want)
	}
}

func TestEnvWithLoginPATHKeepsPATHWhenShellFails(t *testing.T) {
	stubLoginPATH(t, "")
	got := envWithLoginPATH([]string{"PATH=/usr/bin:/bin", "FOO=bar"})
	want := []string{"PATH=/usr/bin:/bin", "FOO=bar"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("envWithLoginPATH = %v, want the original env untouched", got)
	}
}

func TestMissingBuildToolsNamesWhatIsAbsent(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(dir, "go"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Present but not executable: still missing, as far as exec is
	// concerned.
	writeFile(t, filepath.Join(dir, "npm"), "#!/bin/sh\n")

	got := missingBuildTools(dir)
	if strings.Join(got, ",") != "npm" {
		t.Errorf("missingBuildTools = %v, want [npm]", got)
	}
	if got := missingBuildTools(""); strings.Join(got, ",") != "go,npm" {
		t.Errorf("missingBuildTools(empty PATH) = %v, want both", got)
	}
}

// The refusal message points the user at a file to edit, so it must not
// blame a login shell when none was consulted.
func TestPathSourceDescriptionNamesTheActualSource(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	stubLoginPATH(t, "/opt/homebrew/bin")
	if got := pathSourceDescription(); got != "reported by /bin/zsh" {
		t.Errorf("pathSourceDescription = %q, want the shell named", got)
	}

	stubLoginPATH(t, "")
	if got := pathSourceDescription(); !strings.Contains(got, "inherited") {
		t.Errorf("pathSourceDescription = %q, want it to say the PATH was inherited", got)
	}

	t.Setenv("SHELL", "")
	if got := pathSourceDescription(); !strings.Contains(got, "inherited") {
		t.Errorf("pathSourceDescription with no SHELL = %q, want it to say the PATH was inherited", got)
	}
}

// runGit must resolve git on the login PATH, not the process one: from a
// Spotlight launch the process PATH finds /usr/bin/git, the xcrun shim
// that fails until the Xcode license is accepted.
func TestRunGitUsesLoginPATH(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "git")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho login-path-git\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubLoginPATH(t, dir)

	out, err := runGit(dir, "--version")
	if err != nil {
		t.Fatalf("runGit: %v", err)
	}
	if out != "login-path-git" {
		t.Errorf("runGit ran %q, want the git on the login PATH", out)
	}
}

// lookPathIn chooses the git that runs. A relative or empty PATH entry
// resolves against the working directory — a checkout — so it must never
// win, the same rule exec.LookPath enforces with ErrDot.
func TestLookPathInIgnoresRelativeEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	for _, path := range []string{"", ":", ".", "./"} {
		if got := lookPathIn(path, "git"); got != "" {
			t.Errorf("lookPathIn(%q) = %q, want no match from the working directory", path, got)
		}
	}
	if got := lookPathIn(":"+dir, "git"); got != filepath.Join(dir, "git") {
		t.Errorf("lookPathIn with an absolute entry = %q, want it found", got)
	}
}
