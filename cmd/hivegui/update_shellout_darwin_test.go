package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Coverage for the pieces the rest of the update tests stub out: the
// channel dispatcher, the shell-outs (git, ditto, build.sh), the
// last-guard bundle check, and the download size cap. These are the
// functions that stand between a bad download and the user's installed
// app, so "it's only a wrapper" is not a reason to leave them unproven.

// ------------------------------ verifyBundle ------------------------------

// verifyBundle is the last check before swapBundle replaces the running
// app. Every way a bundle can be wrong has to fail here, because after
// this point the old app is already being moved aside.
func TestVerifyBundleRejectsUnusableBundles(t *testing.T) {
	cases := []struct {
		name string
		// build returns a bundle path in dir, having created whatever
		// (broken) contents the case is about.
		build func(t *testing.T, dir string) string
		want  string
	}{
		{
			name: "complete bundle passes",
			build: func(t *testing.T, dir string) string {
				return stubBundle(t, dir, "ok")
			},
		},
		{
			name: "missing hived",
			build: func(t *testing.T, dir string) string {
				b := stubBundle(t, dir, "ok")
				if err := os.Remove(filepath.Join(b, "Contents", "MacOS", "hived")); err != nil {
					t.Fatal(err)
				}
				return b
			},
			want: "missing hived",
		},
		{
			name: "missing hivegui",
			build: func(t *testing.T, dir string) string {
				b := stubBundle(t, dir, "ok")
				if err := os.Remove(filepath.Join(b, "Contents", "MacOS", "hivegui")); err != nil {
					t.Fatal(err)
				}
				return b
			},
			want: "missing hivegui",
		},
		{
			// The failure mode a naive archive/zip extraction would
			// produce: files all there, executable bit gone, app dead on
			// launch with no in-app way back.
			name: "binary not executable",
			build: func(t *testing.T, dir string) string {
				b := stubBundle(t, dir, "ok")
				if err := os.Chmod(filepath.Join(b, "Contents", "MacOS", "hivegui"), 0o644); err != nil {
					t.Fatal(err)
				}
				return b
			},
			want: "not executable",
		},
		{
			name: "not a bundle at all",
			build: func(t *testing.T, dir string) string {
				return filepath.Join(dir, "nothing-here.app")
			},
			want: "missing hivegui",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := verifyBundle(c.build(t, t.TempDir()))
			if c.want == "" {
				if err != nil {
					t.Fatalf("verifyBundle = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("verifyBundle = nil, want an error naming %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %q, want it to mention %q", err, c.want)
			}
		})
	}
}

// ------------------------------ stageUpdate -------------------------------

// The dispatcher is three lines, and getting it backwards would send the
// release channel into a git pull over the user's checkout. Routing is
// asserted by which failure comes back: each channel has a distinctive
// early refusal.
func TestStageUpdateRoutesByChannel(t *testing.T) {
	dir := isolateStateDir(t)
	repo := fakeHiveCheckout(t)
	writeFile(t, filepath.Join(dir, "update.json"),
		fmt.Sprintf(`{"channel":"latest","source_repo":%q}`, repo))

	// Latest: a dirty tree refuses before anything else happens.
	g := &fakeGit{answers: map[string]string{"status --porcelain": " M x.go"}}
	g.install(t)
	_, err := stageUpdate(UpdateInfo{Channel: ChannelLatest}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("latest channel err = %v, want the dirty-tree refusal from stageLatest", err)
	}

	// Release: no such release asset. Crucially, it must NOT have gone
	// anywhere near git.
	before := len(g.calls)
	releaseServer(t, map[string][]byte{}, nil)
	_, err = stageUpdate(UpdateInfo{Channel: ChannelRelease, Latest: "9.9.9"}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "has no") {
		t.Fatalf("release channel err = %v, want a missing-asset error from stageRelease", err)
	}
	if len(g.calls) != before {
		t.Errorf("release channel ran git commands: %v", g.calls[before:])
	}

	// An empty channel is the release channel — normalizeChannel's
	// default has to hold all the way through here.
	_, err = stageUpdate(UpdateInfo{Latest: "9.9.9"}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "has no") {
		t.Fatalf("empty channel err = %v, want it treated as the release channel", err)
	}
}

// -------------------------------- runGit ----------------------------------

// runGit folds stderr into the error. Without it every git failure reads
// "exit status 128", which says nothing about what went wrong.
func TestRunGitReportsStderr(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()

	if _, err := runGit(repo, "rev-parse", "--abbrev-ref", "@{upstream}"); err == nil {
		t.Fatal("runGit = nil error outside a repository, want a failure")
	} else if !strings.Contains(err.Error(), "git rev-parse") {
		t.Errorf("error = %q, want it to name the command", err)
	}

	// And the success path returns trimmed stdout.
	if out, err := runGit(repo, "--version"); err != nil {
		t.Fatalf("runGit --version: %v", err)
	} else if !strings.HasPrefix(out, "git version") {
		t.Errorf("runGit --version = %q, want it to start with %q", out, "git version")
	} else if strings.HasSuffix(out, "\n") {
		t.Error("runGit did not trim trailing whitespace")
	}
}

// ------------------------------ dittoExtract ------------------------------

// ditto is used instead of archive/zip precisely because an .app needs
// its mode bits and symlinks intact. This proves that against a zip
// built the way build.sh builds one.
func TestDittoExtractPreservesModeBitsAndSymlinks(t *testing.T) {
	if _, err := exec.LookPath("ditto"); err != nil {
		t.Skip("ditto not available")
	}
	if _, err := exec.LookPath("zip"); err != nil {
		t.Skip("zip not available")
	}

	src := t.TempDir()
	macos := filepath.Join(src, bundleName, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"hivegui", "hived"} {
		if err := os.WriteFile(filepath.Join(macos, name), []byte("bin"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A framework-style symlink, which archive/zip would flatten into a
	// regular file holding the link target.
	fw := filepath.Join(src, bundleName, "Contents", "Frameworks")
	if err := os.MkdirAll(fw, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("Versions/A/Lib", filepath.Join(fw, "Lib")); err != nil {
		t.Fatal(err)
	}

	// Same invocation build.sh uses.
	zipPath := filepath.Join(t.TempDir(), "Hive-test-macos-universal.zip")
	cmd := exec.Command("zip", "-rq", "--symlinks", zipPath, bundleName)
	cmd.Dir = src
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("zip: %v: %s", err, out)
	}

	dest := filepath.Join(t.TempDir(), "app")
	if err := dittoExtract(zipPath, dest); err != nil {
		t.Fatalf("dittoExtract: %v", err)
	}

	bundle := filepath.Join(dest, bundleName)
	if err := verifyBundle(bundle); err != nil {
		t.Fatalf("extracted bundle fails verifyBundle: %v", err)
	}
	st, err := os.Lstat(filepath.Join(bundle, "Contents", "Frameworks", "Lib"))
	if err != nil {
		t.Fatalf("symlink missing after extract: %v", err)
	}
	if st.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink was extracted as a regular file")
	}
}

func TestDittoExtractRejectsGarbage(t *testing.T) {
	if _, err := exec.LookPath("ditto"); err != nil {
		t.Skip("ditto not available")
	}
	bad := filepath.Join(t.TempDir(), "not-a.zip")
	writeFile(t, bad, "definitely not a zip archive")
	if err := dittoExtract(bad, filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("dittoExtract = nil error on a non-zip, want the failure surfaced")
	}
}

// ----------------------------- runBuildScript -----------------------------

func TestRunBuildScriptStreamsProgress(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "build.sh"), "#!/bin/sh\necho step one\necho\necho step two\n")
	if err := os.Chmod(filepath.Join(repo, "build.sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	var lines []string
	if err := runBuildScript(repo, func(s string) { lines = append(lines, s) }); err != nil {
		t.Fatalf("runBuildScript: %v", err)
	}
	// Blank lines are dropped — the button has one line to render in and
	// a blank one would read as a stall.
	want := []string{"step one", "step two"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Errorf("progress = %v, want %v", lines, want)
	}
}

// A failed build must report the last thing the script said, not a bare
// "exit status 1" the user cannot act on.
func TestRunBuildScriptReportsLastLineOnFailure(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "build.sh"),
		"#!/bin/sh\necho compiling\necho 'error: wails not found' >&2\nexit 1\n")
	if err := os.Chmod(filepath.Join(repo, "build.sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := runBuildScript(repo, func(string) {})
	if err == nil {
		t.Fatal("runBuildScript = nil error for a failing build, want the failure surfaced")
	}
	if !strings.Contains(err.Error(), "wails not found") {
		t.Errorf("error = %q, want it to carry the build's own message", err)
	}
}

// build.sh drives npm/vite/wails, which colour their output and redraw
// progress with carriage returns. The banner renders its message as
// text, so anything not stripped here reaches the user as literal
// `ESC[32m` garbage.
func TestPlainProgressLineStripsTerminalControls(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain text passes through", "compiling hivegui", "compiling hivegui"},
		{"sgr colours", "\x1b[32mbuilding\x1b[0m", "building"},
		{"cr redraw keeps last segment", "50%\r80%\r100%", "100%"},
		{"osc title with bel", "\x1b]0;npm run build\x07done", "done"},
		{"osc title with st", "\x1b]0;npm\x1b\\done", "done"},
		{"control only collapses to empty", "\x1b[2K\x1b[1G", ""},
		{"utf8 survives", "\x1b[32m✓ built\x1b[0m", "✓ built"},
		{"stray control bytes dropped", "a\x07b", "ab"},
		{"tabs kept, surrounding space trimmed", "  a\tb  ", "a\tb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := plainProgressLine(tc.in); got != tc.want {
				t.Errorf("plainProgressLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The end-to-end guarantee: no escape byte can reach the progress
// callback or the failure message, whatever build.sh prints.
func TestRunBuildScriptReportsSanitizedProgress(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "build.sh"),
		"#!/bin/sh\nprintf '\\033[32mcompiling\\033[0m\\n'\nprintf '10%%\\r100%%\\n'\n"+
			"printf '\\033[2K\\033[1G\\n'\nprintf '\\033[31merror: boom\\033[0m\\n' >&2\nexit 1\n")
	if err := os.Chmod(filepath.Join(repo, "build.sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	var lines []string
	err := runBuildScript(repo, func(s string) { lines = append(lines, s) })
	if err == nil {
		t.Fatal("runBuildScript = nil error for a failing build, want the failure surfaced")
	}
	want := []string{"compiling", "100%", "error: boom"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Errorf("progress = %v, want %v", lines, want)
	}
	if strings.ContainsRune(err.Error(), 0x1B) {
		t.Errorf("error = %q, want no escape bytes", err)
	}
}

func TestRunBuildScriptReportsMissingScript(t *testing.T) {
	if err := runBuildScript(t.TempDir(), func(string) {}); err == nil {
		t.Fatal("runBuildScript = nil error with no build.sh, want a failure")
	}
}

// stageLatest's other refusal: a detached HEAD has no upstream to
// fast-forward from, and pulling would either fail confusingly or move
// the user somewhere they did not ask to go.
func TestStageLatestRefusesDetachedHead(t *testing.T) {
	dir := isolateStateDir(t)
	repo := fakeHiveCheckout(t)
	writeFile(t, filepath.Join(dir, "update.json"),
		fmt.Sprintf(`{"channel":"latest","source_repo":%q}`, repo))

	g := &fakeGit{
		answers: map[string]string{"status --porcelain": ""},
		errs:    map[string]error{"symbolic-ref --quiet": fmt.Errorf("exit status 1")},
	}
	g.install(t)

	_, err := stageLatest(UpdateInfo{Channel: ChannelLatest}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "detached HEAD") {
		t.Fatalf("stageLatest err = %v, want the detached-HEAD refusal", err)
	}
	if g.ran("pull") {
		t.Error("stageLatest pulled onto a detached HEAD")
	}
}

// -------------------------------- download --------------------------------

// The cap exists so a wrong or hostile Content-Length cannot fill the
// disk. Shrunk here rather than served for real.
func TestDownloadRejectsOversizeBody(t *testing.T) {
	prev := maxDownloadBytes
	maxDownloadBytes = 16
	t.Cleanup(func() { maxDownloadBytes = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 64)))
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "big.zip")
	err := download(t.Context(), srv.URL, dest)
	if err == nil {
		t.Fatal("download = nil error for a body over the cap, want a refusal")
	}
	if !strings.Contains(err.Error(), "larger than") {
		t.Errorf("error = %q, want it to name the size limit", err)
	}
}

// Exactly at the cap is not over it — an off-by-one here would reject
// legitimate downloads.
func TestDownloadAcceptsBodyExactlyAtCap(t *testing.T) {
	prev := maxDownloadBytes
	maxDownloadBytes = 16
	t.Cleanup(func() { maxDownloadBytes = prev })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 16)))
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "exact.zip")
	if err := download(t.Context(), srv.URL, dest); err != nil {
		t.Fatalf("download of a body exactly at the cap = %v, want nil", err)
	}
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 16 {
		t.Errorf("wrote %d bytes, want 16", len(b))
	}
}

func TestDownloadReportsHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	err := download(t.Context(), srv.URL, filepath.Join(t.TempDir(), "x.zip"))
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("download err = %v, want it to name the HTTP status", err)
	}
}

// ------------------------- cross-volume (EXDEV) swap ----------------------

// swapBundle copies the staged bundle to a sibling of the installed one
// before renaming, because staging lives under the state dir and the app
// under /Applications — different volumes, where a direct os.Rename
// fails with EXDEV. Every other swap test has both paths on one volume,
// so this is the only place that claim is actually exercised.
//
// Skips rather than fails when a disk image cannot be created (CI
// sandboxes, no hdiutil); the same-volume tests still cover the logic.
func TestSwapBundleAcrossVolumes(t *testing.T) {
	stagingVolume := mountScratchVolume(t)

	staged := stubBundle(t, stagingVolume, "new")
	installDir := t.TempDir() // the boot volume
	installed := stubBundle(t, installDir, "old")

	// Guard the premise: if these turn out to be the same device, the
	// test proves nothing and should say so rather than pass silently.
	if sameDevice(t, stagingVolume, installDir) {
		t.Skip("scratch volume landed on the same device; nothing cross-volume to prove")
	}
	if err := os.Rename(staged, filepath.Join(installDir, "direct-rename.app")); err == nil {
		t.Skip("a direct cross-device rename succeeded; this platform has nothing to prove here")
	}

	if err := swapBundle(staged, installed); err != nil {
		t.Fatalf("swapBundle across volumes: %v", err)
	}
	if got := readMarker(t, installed); got != "new" {
		t.Errorf("installed binary = %q after a cross-volume swap, want %q", got, "new")
	}
	if err := verifyBundle(installed); err != nil {
		t.Errorf("bundle is not usable after a cross-volume swap: %v", err)
	}
	entries, err := os.ReadDir(installDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("swapBundle left %s behind", e.Name())
		}
	}
}

// mountScratchVolume attaches a small disk image and returns its mount
// point, detaching it on cleanup.
func mountScratchVolume(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("hdiutil"); err != nil {
		t.Skip("hdiutil not available")
	}
	img := filepath.Join(t.TempDir(), "scratch.dmg")
	name := fmt.Sprintf("hive-swap-test-%d", os.Getpid())
	if out, err := exec.Command("hdiutil", "create", "-size", "20m", "-fs", "APFS",
		"-volname", name, "-quiet", img).CombinedOutput(); err != nil {
		t.Skipf("hdiutil create: %v: %s", err, out)
	}
	mount := filepath.Join(t.TempDir(), "mnt")
	if err := os.MkdirAll(mount, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("hdiutil", "attach", img, "-mountpoint", mount,
		"-nobrowse", "-quiet").CombinedOutput(); err != nil {
		t.Skipf("hdiutil attach: %v: %s", err, out)
	}
	t.Cleanup(func() {
		// -force: the test may still hold directory handles.
		_ = exec.Command("hdiutil", "detach", mount, "-force", "-quiet").Run()
	})
	return mount
}

func sameDevice(t *testing.T, a, b string) bool {
	t.Helper()
	sa, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	da, oka := deviceOf(sa)
	db, okb := deviceOf(sb)
	return oka && okb && da == db
}

// deviceOf pulls the device number out of a FileInfo so sameDevice can
// confirm the two paths really are on different volumes.
func deviceOf(fi os.FileInfo) (int32, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Dev, true
}

// ------------------------ latest-channel hardening ------------------------

// latestRepo wires an isolated state dir + a checkout on the latest
// channel, and returns the fakeGit driving it.
func latestRepo(t *testing.T, answers map[string]string, errs map[string]error) *fakeGit {
	t.Helper()
	dir := isolateStateDir(t)
	repo := fakeHiveCheckout(t)
	writeFile(t, filepath.Join(dir, "update.json"),
		fmt.Sprintf(`{"channel":"latest","source_repo":%q}`, repo))
	if answers == nil {
		answers = map[string]string{}
	}
	if _, ok := answers["status --porcelain"]; !ok {
		answers["status --porcelain"] = ""
	}
	if _, ok := answers["rev-parse --abbrev-ref --symbolic-full-name @{upstream}"]; !ok {
		answers["rev-parse --abbrev-ref --symbolic-full-name @{upstream}"] = "origin/main"
	}
	if _, ok := answers["remote get-url origin"]; !ok {
		answers["remote get-url origin"] = "git@github.com:" + updateRepo + ".git"
	}
	g := &fakeGit{answers: answers, errs: errs}
	g.install(t)
	return g
}

// The source repo path is read out of update.json and its contents get
// built and executed. validateSourceRepo only proves the directory
// *looks* like hive — .git, build.sh and a module line are all
// plantable — so the upstream remote is the real check.
func TestStageLatestRefusesForeignRemote(t *testing.T) {
	g := latestRepo(t, map[string]string{
		"remote get-url origin": "https://github.com/someone-else/hive.git",
	}, nil)
	built := false
	prev := runBuildFn
	runBuildFn = func(string, func(string)) error { built = true; return nil }
	t.Cleanup(func() { runBuildFn = prev })

	_, err := stageLatest(UpdateInfo{Channel: ChannelLatest}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "refusing to build") {
		t.Fatalf("stageLatest err = %v, want a refusal naming the foreign remote", err)
	}
	if g.ran("pull") {
		t.Error("stageLatest pulled from an unpinned remote")
	}
	if built {
		t.Error("stageLatest built code from an unpinned remote")
	}
}

func TestStageLatestAcceptsBothRemoteSpellings(t *testing.T) {
	for _, url := range []string{
		"git@github.com:" + updateRepo + ".git",
		"https://github.com/" + updateRepo + ".git",
		"https://github.com/" + updateRepo,
	} {
		t.Run(url, func(t *testing.T) {
			latestRepo(t, map[string]string{"remote get-url origin": url}, nil)
			// Fails later, at the build — which is proof the remote
			// check let it through.
			prev := runBuildFn
			runBuildFn = func(string, func(string)) error { return fmt.Errorf("sentinel") }
			t.Cleanup(func() { runBuildFn = prev })

			_, err := stageLatest(UpdateInfo{Channel: ChannelLatest}, func(string) {})
			if err == nil || !strings.Contains(err.Error(), "sentinel") {
				t.Fatalf("stageLatest err = %v, want it to reach the build step", err)
			}
		})
	}
}

// A pull runs the checkout's own hooks before build.sh gets a turn, so
// a planted post-merge hook would execute from a button press.
func TestStageLatestDisablesGitHooks(t *testing.T) {
	g := latestRepo(t, nil, nil)
	prev := runBuildFn
	runBuildFn = func(string, func(string)) error { return fmt.Errorf("stop here") }
	t.Cleanup(func() { runBuildFn = prev })

	_, _ = stageLatest(UpdateInfo{Channel: ChannelLatest}, func(string) {})

	var pull string
	for _, c := range g.calls {
		if strings.Contains(c, "pull") {
			pull = c
		}
	}
	if pull == "" {
		t.Fatal("stageLatest never pulled")
	}
	if !strings.Contains(pull, "core.hooksPath=/dev/null") {
		t.Errorf("pull invocation = %q, want hooks disabled", pull)
	}
	if !strings.Contains(pull, "--ff-only") {
		t.Errorf("pull invocation = %q, want --ff-only", pull)
	}
}

// A pull that fails must stop the staging, not fall through to building
// whatever is currently checked out.
func TestStageLatestStopsOnPullFailure(t *testing.T) {
	latestRepo(t, nil, map[string]error{"-c core.hooksPath=/dev/null": fmt.Errorf("would clobber local changes")})
	built := false
	prev := runBuildFn
	runBuildFn = func(string, func(string)) error { built = true; return nil }
	t.Cleanup(func() { runBuildFn = prev })

	_, err := stageLatest(UpdateInfo{Channel: ChannelLatest}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "would clobber") {
		t.Fatalf("stageLatest err = %v, want the pull failure surfaced", err)
	}
	if built {
		t.Error("stageLatest built after a failed pull")
	}
}

// ------------------------ stageRelease success path -----------------------

// The happy path had no test: every release-channel case asserted a
// refusal, so nothing proved a good release actually installs.
func TestStageReleaseSucceedsAndLeavesAUsableBundle(t *testing.T) {
	isolateStateDir(t)

	// A real zip of a real (stub) bundle, packed the way build.sh packs.
	src := t.TempDir()
	stubBundle(t, src, "new")
	zipName := "Hive-9.9.9-macos-universal.zip"
	zipPath := filepath.Join(t.TempDir(), zipName)
	cmd := exec.Command("zip", "-rq", zipPath, bundleName)
	cmd.Dir = src
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("zip unavailable: %v: %s", err, out)
	}
	body, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}

	releaseServer(t, map[string][]byte{
		zipName:        body,
		checksumsAsset: []byte(sha256Of(body) + "  " + zipName + "\n"),
	}, nil)

	var steps []string
	bundle, err := stageRelease(UpdateInfo{Channel: ChannelRelease, Latest: "9.9.9"},
		func(s string) { steps = append(steps, s) })
	if err != nil {
		t.Fatalf("stageRelease: %v", err)
	}
	if err := verifyBundle(bundle); err != nil {
		t.Fatalf("staged bundle is not usable: %v", err)
	}
	if got := readMarker(t, bundle); got != "new" {
		t.Errorf("staged binary = %q, want %q", got, "new")
	}
	// The zip is tens of MB in production and is not kept once unpacked.
	if _, err := os.Stat(filepath.Join(updatesRoot(), "9.9.9", zipName)); err == nil {
		t.Error("stageRelease kept the downloaded zip after unpacking")
	}
	// The user watches these; silence for the length of a download reads
	// as a hang.
	if len(steps) < 4 {
		t.Errorf("progress steps = %v, want the user to see each phase", steps)
	}
}

// ---------------------------- staging cleanup -----------------------------

func TestPruneStagingDirsClearsEverything(t *testing.T) {
	isolateStateDir(t)
	for _, v := range []string{"1.0.0", "2.0.0"} {
		if _, err := stagingDir(v); err != nil {
			t.Fatal(err)
		}
	}
	pruneStagingDirs()
	if _, err := os.Stat(updatesRoot()); !os.IsNotExist(err) {
		t.Errorf("updates root still present after prune (err=%v)", err)
	}
	// Idempotent: called on every successful apply, including the first.
	pruneStagingDirs()
}
