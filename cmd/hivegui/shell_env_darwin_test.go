package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeShell writes a script that behaves like a shell for probe
// purposes: it runs the last argument it was handed. Everything before
// body is printed first, standing in for the greetings, instant
// prompts and login banners a real rc file emits.
func fakeShell(t *testing.T, noise string, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fakeshell")
	writeFile(t, path, "#!/bin/sh\n"+
		"printf '%s' '"+noise+"'\n"+
		"for a in \"$@\"; do last=\"$a\"; done\n"+
		body+
		"exec /bin/sh -c \"$last\"\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveLoginPATHReadsShellPATH(t *testing.T) {
	shell := fakeShell(t, "", "PATH=/opt/homebrew/bin:/usr/bin; export PATH\n")
	if got := resolveLoginPATH(shell); got != "/opt/homebrew/bin:/usr/bin" {
		t.Errorf("resolveLoginPATH = %q, want the shell's own PATH", got)
	}
}

// rc files print. powerlevel10k's instant prompt emits escape codes,
// fish greets, corporate .zshrc files echo banners — and any of it can
// contain something that looks like an env entry. Only what lies
// between the markers counts.
func TestResolveLoginPATHIgnoresGreetingNoise(t *testing.T) {
	shell := fakeShell(t, "Welcome!\nPATH=/decoy\n\x1b[1mready\x1b[0m\n",
		"PATH=/real/bin; export PATH\n")
	if got := resolveLoginPATH(shell); got != "/real/bin" {
		t.Errorf("resolveLoginPATH = %q, want the marked block's PATH, not the greeting's", got)
	}
}

// -i is the load-bearing flag (a login non-interactive zsh never reads
// .zshrc, where nvm and fnm install themselves), but a shell that
// rejects it must still be probed rather than written off.
func TestResolveLoginPATHFallsBackWhenInteractiveRejected(t *testing.T) {
	shell := fakeShell(t, "",
		"case \"$1\" in -i) echo 'Unknown option: -i' >&2; exit 1;; esac\n"+
			"PATH=/fallback/bin; export PATH\n")
	if got := resolveLoginPATH(shell); got != "/fallback/bin" {
		t.Errorf("resolveLoginPATH = %q, want the second argv form to be tried", got)
	}
}

// The bound has to hold against the thing an interactive rc actually
// does: background a job. That grandchild inherits stdout, and
// Output() waits on the pipe rather than on the shell — so without
// WaitDelay this probe returns when the background job ends, not when
// the timeout expires, and the build button sits on "Updating…" until
// it does.
func TestResolveLoginPATHDoesNotWaitForBackgroundedRCJobs(t *testing.T) {
	prev := loginEnvTimeout
	loginEnvTimeout = 500 * time.Millisecond
	t.Cleanup(func() { loginEnvTimeout = prev })

	shell := fakeShell(t, "", "sleep 5 &\nPATH=/rc/bin; export PATH\n")

	done := make(chan string, 1)
	start := time.Now()
	go func() { done <- resolveLoginPATH(shell) }()
	select {
	case got := <-done:
		// The shell printed everything before backgrounding anything,
		// so the PATH must survive the WaitDelay cutoff.
		if got != "/rc/bin" {
			t.Errorf("resolveLoginPATH = %q, want /rc/bin — output is complete even when the pipe is held open", got)
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Errorf("resolveLoginPATH took %v, want it bounded by loginEnvTimeout", elapsed)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("resolveLoginPATH blocked on the backgrounded rc job, want it bounded by loginEnvTimeout")
	}
}

func TestResolveLoginPATHGivesUpQuietly(t *testing.T) {
	if got := resolveLoginPATH(""); got != "" {
		t.Errorf("resolveLoginPATH(no SHELL) = %q, want empty", got)
	}
	if got := resolveLoginPATH(filepath.Join(t.TempDir(), "nope")); got != "" {
		t.Errorf("resolveLoginPATH(unrunnable) = %q, want empty", got)
	}
	// A shell that runs but prints nothing marked: no PATH, no crash.
	silent := filepath.Join(t.TempDir(), "silent")
	writeFile(t, silent, "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(silent, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveLoginPATH(silent); got != "" {
		t.Errorf("resolveLoginPATH(silent shell) = %q, want empty", got)
	}
}

// tcsh rejects -l alongside -c outright ("Unknown option: `-l'"), so it
// gets its own argv form.
func TestProbeArgvsPerShell(t *testing.T) {
	for _, tc := range []struct {
		shell string
		first string
	}{
		{"/bin/zsh", "-i"},
		{"/bin/bash", "-i"},
		{"/opt/homebrew/bin/fish", "-i"},
		{"/bin/tcsh", "-ic"},
		{"/bin/csh", "-ic"},
	} {
		got := probeArgvs(tc.shell)
		if len(got) == 0 || got[0][0] != tc.first {
			t.Errorf("probeArgvs(%s) = %v, want first form to start with %q", tc.shell, got, tc.first)
		}
	}
}

// env -0 is used precisely so a value containing a newline cannot forge
// an entry of its own.
func TestPathFromEnvBlockIsNULDelimited(t *testing.T) {
	block := []byte("EDITOR=vim\x00GREETING=hi\nPATH=/forged\x00PATH=/real\x00")
	if got := pathFromEnvBlock(block); got != "/real" {
		t.Errorf("pathFromEnvBlock = %q, want /real", got)
	}
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
