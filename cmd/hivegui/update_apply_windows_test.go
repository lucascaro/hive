//go:build windows

package main

import (
	"archive/zip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// installWith writes a fake install directory holding both executables
// with recognisable contents, and points installDirFn at it.
func installWith(t *testing.T, gui, daemon string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, guiExe), gui)
	writeFile(t, filepath.Join(dir, daemonExe), daemon)
	useInstallDir(t, dir)
	return dir
}

func useInstallDir(t *testing.T, dir string) {
	t.Helper()
	prev := installDirFn
	installDirFn = func() (string, error) { return dir, nil }
	t.Cleanup(func() { installDirFn = prev })
}

// stagedWith writes a staged payload directory.
func stagedWith(t *testing.T, gui, daemon string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, guiExe), gui)
	writeFile(t, filepath.Join(dir, daemonExe), daemon)
	return dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestSwapExesReplacesInstalled(t *testing.T) {
	install := installWith(t, "old-gui", "old-daemon")
	staged := stagedWith(t, "new-gui", "new-daemon")

	if err := swapExes(staged, install); err != nil {
		t.Fatalf("swapExes: %v", err)
	}
	if got := readFile(t, filepath.Join(install, guiExe)); got != "new-gui" {
		t.Errorf("%s = %q, want new-gui", guiExe, got)
	}
	if got := readFile(t, filepath.Join(install, daemonExe)); got != "new-daemon" {
		t.Errorf("%s = %q, want new-daemon", daemonExe, got)
	}
	// Nothing running held these, so the best-effort cleanup should have
	// removed the displaced copies rather than leaving litter.
	for _, name := range payloadExes {
		if _, err := os.Stat(filepath.Join(install, "."+name+".old")); err == nil {
			t.Errorf("%s was left behind though nothing had it mapped", "."+name+".old")
		}
		if _, err := os.Stat(filepath.Join(install, "."+name+".new")); err == nil {
			t.Errorf("%s was left behind", "."+name+".new")
		}
	}
}

// A failure partway through must leave a launchable Hive, not a
// half-replaced one. payloadExes swaps hived first, so removing the
// staged hivegui makes the *second* file fail with the first already
// installed — the case rollback exists for.
func TestSwapExesRollsBackOnFailure(t *testing.T) {
	install := installWith(t, "old-gui", "old-daemon")
	staged := stagedWith(t, "new-gui", "new-daemon")
	// Make the copy of hivegui fail by replacing it with a directory:
	// os.Open on it succeeds, io.Copy does not.
	if err := os.Remove(filepath.Join(staged, guiExe)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(staged, guiExe), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := swapExes(staged, install); err == nil {
		t.Fatal("swapExes succeeded though the staged hivegui was unreadable")
	}
	if got := readFile(t, filepath.Join(install, guiExe)); got != "old-gui" {
		t.Errorf("%s = %q, want the original old-gui back", guiExe, got)
	}
	if got := readFile(t, filepath.Join(install, daemonExe)); got != "old-daemon" {
		t.Errorf("%s = %q, want the original old-daemon back", daemonExe, got)
	}
	entries, err := os.ReadDir(install)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".new") {
			t.Errorf("rollback left %s behind", e.Name())
		}
	}
}

// TestSwapExesSurvivesRunningImage pins the property this whole
// implementation rests on, against a real running process rather than a
// description of one.
//
// update_apply_other.go used to assert that replacing a running .exe
// "would need a detached helper process". It does not: Windows refuses
// to overwrite or delete a mapped image but allows renaming it, which
// is exactly what swapExes does. If a future Windows release ever
// tightened that, this test is what would catch it — everything else
// here would still pass against files nothing has open.
func TestSwapExesSurvivesRunningImage(t *testing.T) {
	install := t.TempDir()
	useInstallDir(t, install)

	// The test binary is a real executable we can run; copy it in as
	// hivegui.exe and start it so the swap has a mapped image to move.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(install, guiExe)
	if err := copyFile(self, target); err != nil {
		t.Fatalf("copy test binary: %v", err)
	}
	writeFile(t, filepath.Join(install, daemonExe), "old-daemon")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, target, "-test.run=TestSwapHelperSleeps", "-test.timeout=25s")
	cmd.Env = append(os.Environ(), "HIVE_SWAP_HELPER=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	// Wait until the image is genuinely mapped: until then the rename
	// would succeed for the boring reason.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := os.Remove(target); err != nil {
			break // refused — the process has it open, which is what we want
		}
		// It went away; put it back and keep waiting for the child.
		if err := copyFile(self, target); err != nil {
			t.Fatalf("restore target: %v", err)
		}
		if time.Now().After(deadline) {
			t.Skip("helper process never mapped the image; nothing to prove here")
		}
		time.Sleep(50 * time.Millisecond)
	}

	staged := stagedWith(t, "new-gui", "new-daemon")
	if err := swapExes(staged, install); err != nil {
		t.Fatalf("swapExes over a running image: %v", err)
	}
	if got := readFile(t, target); got != "new-gui" {
		t.Errorf("%s = %q, want new-gui", guiExe, got)
	}
	// The point of the exercise: the process whose image we just renamed
	// is still running. A swap that killed it would be a regression even
	// if the files ended up correct.
	alive, _, err := runTasklistProbe(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("probe helper process: %v", err)
	}
	if !alive {
		t.Error("helper process died during the swap")
	}
}

// TestSwapHelperSleeps is not a test. It is the child process
// TestSwapExesSurvivesRunningImage starts so there is a real mapped
// image to rename. It exits immediately unless that test asked for it.
func TestSwapHelperSleeps(t *testing.T) {
	if os.Getenv("HIVE_SWAP_HELPER") != "1" {
		t.Skip("helper process entry point")
	}
	time.Sleep(20 * time.Second)
}

// A displaced image that is still mapped cannot be deleted, so a second
// update before a restart has to find another name rather than failing.
func TestFreeAsidePathCountsUpWhenTaken(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "."+guiExe+".old")

	got, err := freeAsidePath(dir, guiExe)
	if err != nil || got != base {
		t.Fatalf("freeAsidePath on a clean dir = %q, %v; want %q", got, err, base)
	}

	// An existing-but-removable file is reused, not counted past.
	writeFile(t, base, "stale")
	got, err = freeAsidePath(dir, guiExe)
	if err != nil || got != base {
		t.Fatalf("freeAsidePath with a removable leftover = %q, %v; want %q", got, err, base)
	}

	// A directory at that path stands in for an undeletable mapped
	// image: os.Remove refuses it, so the counter has to advance.
	if err := os.Mkdir(base, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(base, "held"), "x")
	got, err = freeAsidePath(dir, guiExe)
	if err != nil {
		t.Fatalf("freeAsidePath with an undeletable leftover: %v", err)
	}
	if got != base+".1" {
		t.Errorf("freeAsidePath = %q, want %q", got, base+".1")
	}
}

func TestIsSwapLeftoverMatchesOnlyOurFiles(t *testing.T) {
	leftovers := []string{
		"." + guiExe + ".old",
		"." + daemonExe + ".old",
		"." + daemonExe + ".old.3",
	}
	for _, name := range leftovers {
		if !isSwapLeftover(name) {
			t.Errorf("isSwapLeftover(%q) = false, want true", name)
		}
	}
	// .new is deliberately absent from the sweep: another window may be
	// mid-swap and still need it. See pruneRenamedAside.
	keep := []string{guiExe, daemonExe, "." + guiExe + ".new", "." + daemonExe + ".new",
		"hivegui.exe.bak", ".hivegui.exe", "notes.old", ".hivegui.exe.olds"}
	for _, name := range keep {
		if isSwapLeftover(name) {
			t.Errorf("isSwapLeftover(%q) = true, want false — that file is not ours to delete", name)
		}
	}
}

func TestPruneRenamedAsideRemovesOnlyLeftovers(t *testing.T) {
	dir := t.TempDir()
	useInstallDir(t, dir)
	// Both executables present, so nothing here is an interrupted swap
	// needing repair - this is only about what the sweep removes.
	writeFile(t, filepath.Join(dir, guiExe), "live")
	writeFile(t, filepath.Join(dir, daemonExe), "live")
	writeFile(t, filepath.Join(dir, "."+guiExe+".old"), "displaced")
	writeFile(t, filepath.Join(dir, "."+daemonExe+".old.2"), "displaced")
	writeFile(t, filepath.Join(dir, "hive.json"), "keep me")

	pruneRenamedAside()

	for _, gone := range []string{"." + guiExe + ".old", "." + daemonExe + ".old.2"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("%s survived the sweep", gone)
		}
	}
	for _, kept := range []string{guiExe, daemonExe, "hive.json"} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s was swept but should have been left alone", kept)
		}
	}
}

func TestApplyRefusesUnwritableInstallDir(t *testing.T) {
	staged := stagedWith(t, "new-gui", "new-daemon")
	missing := filepath.Join(t.TempDir(), "not-there")
	useInstallDir(t, missing)

	err := applyStagedBundle(staged)
	if err == nil {
		t.Fatal("applyStagedBundle succeeded against an unwritable install dir")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not name the directory it refused", err)
	}
}

func TestApplyRefusesIncompletePayload(t *testing.T) {
	install := installWith(t, "old-gui", "old-daemon")
	staged := t.TempDir()
	writeFile(t, filepath.Join(staged, guiExe), "new-gui") // no hived

	if err := applyStagedBundle(staged); err == nil {
		t.Fatal("applyStagedBundle installed a payload with no hived")
	}
	if got := readFile(t, filepath.Join(install, guiExe)); got != "old-gui" {
		t.Errorf("%s = %q, want the install untouched", guiExe, got)
	}
}

func TestVerifyPayloadRejectsEmptyExe(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, guiExe), "")
	writeFile(t, filepath.Join(dir, daemonExe), "ok")
	if err := verifyPayload(dir); err == nil {
		t.Error("verifyPayload accepted a zero-length hivegui.exe")
	}
}

// The latest channel builds into a directory it erases first, so it
// cannot be the directory Hive is running from.
func TestCheckLatestInstallLayoutRefusesBuildDir(t *testing.T) {
	repo := t.TempDir()
	build := buildOutputDir(repo)
	if err := os.MkdirAll(build, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkLatestInstallLayout(build, build); err == nil {
		t.Fatal("running from the build directory was accepted")
	}
	// Case differences must not be a way around it.
	if err := checkLatestInstallLayout(strings.ToUpper(build), build); err == nil {
		t.Error("an upper-cased spelling of the build directory was accepted")
	}
	other := t.TempDir()
	if err := checkLatestInstallLayout(other, build); err != nil {
		t.Errorf("a normal install was refused: %v", err)
	}
}

func TestExtractPayloadTakesOnlyExpectedNames(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "release.zip")
	writeZip(t, zipPath, map[string]string{
		guiExe:                  "gui-body",
		daemonExe:               "daemon-body",
		"README.txt":            "ignore me",
		`..\..\evil.exe`:        "traversal",
		"nested/" + guiExe:      "must not shadow the real one",
		"installer/setup.exe":   "ignore me too",
		"subdir/notes/hive.log": "ignore",
	})

	dest := filepath.Join(dir, "app")
	if err := extractPayload(zipPath, dest); err != nil {
		t.Fatalf("extractPayload: %v", err)
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(payloadExes) {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("extracted %v, want exactly %v", names, payloadExes)
	}
	// A nested entry with the same base name must not win, whatever
	// order the archive happens to list them in.
	if got := readFile(t, filepath.Join(dest, guiExe)); got != "gui-body" {
		t.Errorf("%s = %q, want gui-body", guiExe, got)
	}
	// Nothing may be written outside dest, whatever the archive claims.
	if _, err := os.Stat(filepath.Join(dir, "evil.exe")); err == nil {
		t.Error("an entry escaped the destination directory")
	}
}

func TestExtractPayloadRequiresBothExes(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "release.zip")
	writeZip(t, zipPath, map[string]string{guiExe: "gui-body"})

	err := extractPayload(zipPath, filepath.Join(dir, "app"))
	if err == nil {
		t.Fatal("extractPayload accepted an archive with no hived.exe")
	}
	if !strings.Contains(err.Error(), daemonExe) {
		t.Errorf("error %q does not say which file was missing", err)
	}
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

// A repo path with a space would split the command if it reached bash
// unquoted, and build.sh would be run against the wrong directory.
func TestBuildCommandLineQuotesTheRepoPath(t *testing.T) {
	got := buildCommandLine(`D:\my code\hive`)
	want := `cd 'D:/my code/hive' && ./build.sh --platform windows`
	if got != want {
		t.Errorf("buildCommandLine = %q, want %q", got, want)
	}
	if !strings.Contains(buildCommandLine(`D:\it's\hive`), `'\''`) {
		t.Error("an embedded quote was not escaped")
	}
}

// The darwin build-tool probe decides a tool exists by testing
// st.Mode()&0o111, which is always 0 on Windows. Anything that reused
// it would report every tool missing; this pins that the Windows path
// asks the login shell instead.
func TestRunBuildScriptReportsMissingToolsFromTheProbe(t *testing.T) {
	prev := buildToolProbeFn
	buildToolProbeFn = func(context.Context, string, string) ([]string, error) {
		return []string{"go", "npm"}, nil
	}
	t.Cleanup(func() { buildToolProbeFn = prev })

	err := runBuildScript(t.TempDir(), func(string) {})
	if err == nil {
		t.Fatal("runBuildScript ran the build with no toolchain")
	}
	if !strings.Contains(err.Error(), "go") || !strings.Contains(err.Error(), "npm") {
		t.Errorf("error %q does not name the missing tools", err)
	}
}

func TestUpdateCapabilityRefusesUnwritableInstall(t *testing.T) {
	isolateStateDir(t)
	useInstallDir(t, filepath.Join(t.TempDir(), "not-there"))

	ok, reason := updateCapability()
	if ok {
		t.Fatal("updateCapability said yes for an install dir that does not exist")
	}
	if reason == "" {
		t.Error("updateCapability refused without saying why")
	}
}

func TestUpdateCapabilityAcceptsAWritableInstall(t *testing.T) {
	isolateStateDir(t)
	useInstallDir(t, t.TempDir())

	ok, reason := updateCapability()
	if !ok {
		t.Fatalf("updateCapability refused a writable install: %s", reason)
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty when the answer is yes", reason)
	}
}

func TestStagedDaemonPathPointsAtHived(t *testing.T) {
	if got, want := stagedDaemonPath(`C:\staged`), filepath.Join(`C:\staged`, daemonExe); got != want {
		t.Errorf("stagedDaemonPath = %q, want %q", got, want)
	}
}

// swapExes has one window - between renaming the old image aside and
// moving the new one into place - where the install directory holds no
// <exe> at all. A process death there used to be unrecoverable: the
// startup sweep deleted both halves, so hived.exe was gone for good with
// no in-app way back (the release channel refuses when you are already
// on the newest version), and the same window for hivegui.exe left Hive
// unlaunchable.
func TestPruneRestoresAnInterruptedSwap(t *testing.T) {
	for _, tc := range []struct {
		name    string
		present map[string]string
		want    string
	}{
		{
			"prefers the new image, which is the update that was asked for",
			map[string]string{"." + daemonExe + ".old": "previous", "." + daemonExe + ".new": "incoming"},
			"incoming",
		},
		{
			"falls back to the displaced image when there is no new one",
			map[string]string{"." + daemonExe + ".old": "previous"},
			"previous",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			useInstallDir(t, dir)
			writeFile(t, filepath.Join(dir, guiExe), "live")
			for name, body := range tc.present {
				writeFile(t, filepath.Join(dir, name), body)
			}

			pruneRenamedAside()

			got, err := os.ReadFile(filepath.Join(dir, daemonExe))
			if err != nil {
				t.Fatalf("%s was not restored after an interrupted swap: %v", daemonExe, err)
			}
			if string(got) != tc.want {
				t.Errorf("%s = %q, want %q", daemonExe, got, tc.want)
			}
		})
	}
}

// Every GUI window is its own process, so opening a second one while the
// first is mid-swap runs this sweep concurrently. Deleting .new there
// makes the running swap fail with a baffling "cannot find the file"
// error. A stale .new costs disk only, and the next swap overwrites it
// anyway, so leaving it is the cheaper trade.
func TestPruneLeavesIncomingFilesForARunningSwap(t *testing.T) {
	dir := t.TempDir()
	useInstallDir(t, dir)
	writeFile(t, filepath.Join(dir, guiExe), "live")
	writeFile(t, filepath.Join(dir, daemonExe), "live")
	writeFile(t, filepath.Join(dir, "."+daemonExe+".new"), "incoming")

	pruneRenamedAside()

	if _, err := os.Stat(filepath.Join(dir, "."+daemonExe+".new")); err != nil {
		t.Errorf(".%s.new was swept while a swap could still be using it: %v", daemonExe, err)
	}
}
