package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestManifest_Validate(t *testing.T) {
	good := Manifest{ID: "web-hook2", Name: "Webhook", Version: "0.1.0", APIVersion: APIVersion,
		Main: &Entry{Command: []string{"node", "main.mjs"}}}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid manifest: %v", err)
	}
	cases := map[string]func(*Manifest){
		"bad id":                  func(m *Manifest) { m.ID = "Web_Hook" },
		"leading dash":            func(m *Manifest) { m.ID = "-x" },
		"no name":                 func(m *Manifest) { m.Name = " " },
		"ui entry":                func(m *Manifest) { m.UI = json.RawMessage(`{"entry":"ui.js"}`) },
		"no main":                 func(m *Manifest) { m.Main = nil },
		"empty command":           func(m *Manifest) { m.Main = &Entry{Command: []string{""}} },
		"newline in name":         func(m *Manifest) { m.Name = "Webhook\nRuns: true" },
		"control in version":      func(m *Manifest) { m.Version = "0.1\x1b[2J" },
		"bidi in description":     func(m *Manifest) { m.Description = "safe \u202egnp.exe" },
		"line separator in name":  func(m *Manifest) { m.Name = "Web\u2028hook" },
		"zero-width in name":      func(m *Manifest) { m.Name = "Web\u200bhook" },
		"BOM in name":             func(m *Manifest) { m.Name = "\ufeffWebhook" },
		"NEL in version":          func(m *Manifest) { m.Version = "0.1\u0085" },
		"bidi isolate in command": func(m *Manifest) { m.Main = &Entry{Command: []string{"node", "a\u2067b"}} },
		"newline in command":      func(m *Manifest) { m.Main = &Entry{Command: []string{"node\nrm"}} },
	}
	for name, edit := range cases {
		m := good
		m.Main = &Entry{Command: append([]string(nil), good.Main.Command...)}
		edit(&m)
		err := m.Validate()
		if err == nil || errors.Is(err, ErrAPIVersion) {
			t.Errorf("%s: Validate = %v, want a structural error", name, err)
		}
	}
	future := good
	future.APIVersion = "1.0"
	if err := future.Validate(); !errors.Is(err, ErrAPIVersion) {
		t.Errorf("api_version 1.0: Validate = %v, want ErrAPIVersion", err)
	}
}

func TestInstallDir_CopiesSkipsGitAndSymlinks(t *testing.T) {
	m, _ := newTestManager(t)
	src := writePlugin(t, "copy", "run", nil)
	_ = os.MkdirAll(filepath.Join(src, ".git"), 0o700)
	_ = os.WriteFile(filepath.Join(src, ".git", "HEAD"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(src, "main.mjs"), []byte("//"), 0o600)
	if runtime.GOOS != "windows" {
		_ = os.Symlink("/etc", filepath.Join(src, "leak"))
	}
	info := install(t, m, src)
	if info.Source != src || info.Commit != "" {
		t.Errorf("source/commit = %q/%q", info.Source, info.Commit)
	}
	dst := m.installPath("copy")
	if _, err := os.Stat(filepath.Join(dst, "main.mjs")); err != nil {
		t.Errorf("main.mjs not copied: %v", err)
	}
	for _, skipped := range []string{".git", "leak"} {
		if _, err := os.Lstat(filepath.Join(dst, skipped)); !os.IsNotExist(err) {
			t.Errorf("%s was copied (%v)", skipped, err)
		}
	}
}

func TestInstall_DuplicateIDRefused(t *testing.T) {
	m, _ := newTestManager(t)
	install(t, m, writePlugin(t, "twice", "run", nil))
	_, err := m.Install(context.Background(), writePlugin(t, "twice", "run", nil), "")
	if !errors.Is(err, errDuplicate) {
		t.Fatalf("second install = %v, want errDuplicate", err)
	}
	assertNoStaging(t, m)
}

func TestInstall_InvalidManifestLeavesNothing(t *testing.T) {
	m, _ := newTestManager(t)
	bad := writePlugin(t, "Bad_ID", "run", nil)
	if _, err := m.Install(context.Background(), bad, ""); err == nil {
		t.Fatal("installed a manifest with an invalid id")
	}
	if _, err := m.Install(context.Background(), "relative/dir", ""); err == nil {
		t.Fatal("installed from a relative path")
	}
	if len(m.List()) != 0 {
		t.Fatal("a failed install was listed")
	}
	assertNoStaging(t, m)
}

func TestIsGitURL(t *testing.T) {
	for src, want := range map[string]bool{
		"https://github.com/a/b.git": true,
		"ssh://git@host/a/b":         true,
		"git@github.com:a/b.git":     true,
		"file:///tmp/repo":           true,
		"/abs/local/dir":             false,
		"~/plugins/x":                false,
	} {
		got, err := isGitURL(src)
		if err != nil || got != want {
			t.Errorf("isGitURL(%q) = %v, %v; want %v", src, got, err, want)
		}
	}
	for _, bad := range []string{"ext::sh -c touch% /tmp/pwned", "http://example.com/x", "fd::3"} {
		if _, err := isGitURL(bad); err == nil {
			t.Errorf("isGitURL(%q) allowed a forbidden transport", bad)
		}
	}
}

func TestGitEnv_NeverPrompts(t *testing.T) {
	// The clone's environment is what keeps git and ssh from waiting on
	// a terminal the daemon does not have.
	env := strings.Join(gitEnv(), "\n")
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes"} {
		if !strings.Contains(env, want) {
			t.Errorf("git env missing %s", want)
		}
	}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

// gitRepo commits dir's contents into a new repo and returns a file://
// URL for it plus the commit.
func gitRepo(t *testing.T, dir string) (string, string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"}, {"add", "-A"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "plugin"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	c := exec.Command("git", "rev-parse", "HEAD")
	c.Dir = dir
	out, err := c.Output()
	if err != nil {
		t.Fatal(err)
	}
	return "file://" + filepath.ToSlash(dir), strings.TrimSpace(string(out))
}

func TestInstallGit_FileURLPinsCommit(t *testing.T) {
	requireGit(t)
	m, _ := newTestManager(t)
	url, commit := gitRepo(t, writePlugin(t, "fromgit", "run", nil))
	info, err := m.Install(context.Background(), url, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "fromgit" || info.Source != url || info.Commit != commit || info.Enabled {
		t.Fatalf("git install = %+v, want commit %s, disabled", info, commit)
	}
	assertNoStaging(t, m)
}

func TestInstallGit_UnreachableSSHFailsFast(t *testing.T) {
	requireGit(t)
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh not installed")
	}
	m, _ := newTestManager(t)
	start := time.Now()
	_, err := m.Install(context.Background(), "ssh://git@127.0.0.1:1/nope.git", "")
	if err == nil {
		t.Fatal("install from a closed port succeeded")
	}
	if d := time.Since(start); d > 15*time.Second {
		t.Fatalf("failed after %s; it should not wait for a prompt or the clone timeout", d)
	}
	assertNoStaging(t, m)
}

// A clone whose git forks a grandchild that holds the output pipe must
// still return promptly when cancelled, with the grandchild dead.
func TestInstallGit_ContextCancelKillsClone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script git shim")
	}
	shim := t.TempDir()
	pidFile := filepath.Join(shim, "child.pid")
	script := "#!/bin/sh\nsleep 300 &\necho $! > " + pidFile + "\nwait\n"
	if err := os.WriteFile(filepath.Join(shim, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))

	m, _ := newTestManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := m.Install(ctx, "https://example.invalid/p.git", "")
		done <- err
	}()
	var child int
	deadline := time.Now().Add(5 * time.Second)
	for child == 0 {
		if b, err := os.ReadFile(pidFile); err == nil {
			child, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		}
		if time.Now().After(deadline) {
			t.Fatal("shim never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled install succeeded")
		}
	case <-time.After(7 * time.Second):
		t.Fatal("cancelled install did not return")
	}
	assertDead(t, child)
	assertNoStaging(t, m)
}

// An install whose fetch finishes after Stop must not land, and must
// clean up after itself.
func TestInstall_AfterStopCleansStaging(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script git shim")
	}
	requireGit(t)
	real, _ := exec.LookPath("git")
	shim := t.TempDir()
	script := "#!/bin/sh\nsleep 0.5\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(shim, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	url, _ := gitRepo(t, writePlugin(t, "slow", "run", nil))
	t.Setenv("PATH", shim+string(os.PathListSeparator)+os.Getenv("PATH"))

	m, _ := newTestManager(t)
	done := make(chan error, 1)
	go func() {
		_, err := m.Install(context.Background(), url, "")
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	m.Stop()
	if err := <-done; !errors.Is(err, ErrManagerStopped) {
		t.Fatalf("install racing Stop = %v, want ErrManagerStopped", err)
	}
	if len(m.List()) != 0 {
		t.Fatal("plugin landed after Stop")
	}
	assertNoStaging(t, m)
}

func assertNoStaging(t *testing.T, m *Manager) {
	t.Helper()
	ents, _ := os.ReadDir(filepath.Join(m.stateDir, installDir))
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".staging-") {
			t.Fatalf("staging dir %s left behind", e.Name())
		}
	}
}

func TestLimiter_ThrottlesAfterBurst(t *testing.T) {
	l := &Limiter{tokens: 10, last: time.Now(), rate: 100, burst: 10}
	ctx := context.Background()
	start := time.Now()
	for range 10 {
		_ = l.Wait(ctx, 1) // the burst: no wait
	}
	if d := time.Since(start); d > 20*time.Millisecond {
		t.Fatalf("burst took %s", d)
	}
	start = time.Now()
	for range 20 {
		_ = l.Wait(ctx, 1) // 20 tokens at 100/s ≈ 200ms
	}
	if d := time.Since(start); d < 150*time.Millisecond {
		t.Fatalf("20 tokens past the burst took %s, want ≈200ms", d)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := l.Wait(cctx, 1000); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait on a cancelled ctx = %v", err)
	}
}
