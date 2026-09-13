//go:build windows

package main

import (
	"archive/zip"
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/lucascaro/hive/internal/proc"
)

// A Windows install is two loose executables in one directory, not a
// bundle. That is what build.sh packages into the release zip and what
// `wails build` leaves in cmd/hivegui/build/bin, so it is also the
// shape a staged payload takes here: `staged` is a directory holding
// both of these.
const (
	guiExe    = "hivegui.exe"
	daemonExe = "hived.exe"
)

// payloadExes is every file an update replaces, in the order the swap
// walks them. hivegui last is deliberate: it is the running process's
// own image, so if anything is going to fail it should fail while the
// rollback still has the smaller job.
var payloadExes = []string{daemonExe, guiExe}

// Seams for tests, mirroring the darwin block in update_apply_darwin.go.
var (
	// extractZipFn unpacks a release zip into a directory.
	extractZipFn = extractPayload
	// runBuildFn runs build.sh in a checkout, streaming progress.
	runBuildFn = runBuildScript
	// installDirFn resolves the directory the running binaries live in.
	// Seamed because tests cannot move the test binary into a fixture.
	installDirFn = installDir
)

// stageUpdate prepares the new build and returns the staged payload
// directory. Called from StartUpdate's goroutine; progress is reported
// by calling progress with a short human-readable line.
func stageUpdate(info UpdateInfo, progress func(string)) (string, error) {
	if info.Channel == ChannelLatest {
		return stageLatest(info, progress)
	}
	return stageRelease(info, progress)
}

// ---------------------------- release channel ----------------------------

// stageRelease downloads the Windows zip for the newest release,
// verifies its checksum, and unpacks it.
//
// Unlike macOS this is integrity checking only, not a supply-chain
// defense. The macOS path pins a bundle to Hive's Apple Developer team
// (update_verify_darwin.go), which is what makes a release signed by
// anyone else refuseable. No Windows release binary is signed at all —
// there is no signtool in build.sh or scripts/release-artifacts.sh — so
// there is no publisher to pin and nothing an equivalent check could
// assert. checksums.txt still catches the truncated or corrupted
// download, which is the failure users actually hit.
//
// Closing that gap needs either code-signing certificates or a detached
// signature over checksums.txt with a key baked into the binary. Both
// are upstream release-process decisions; see
// docs/exec-plans/active/windows-in-app-update.md.
func stageRelease(info UpdateInfo, progress func(string)) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	progress("Looking up release…")
	assets, err := fetchReleaseAssets(ctx)
	if err != nil {
		return "", err
	}
	zipName := fmt.Sprintf("Hive-%s-windows-amd64.zip", info.Latest)
	zipURL, err := assetURL(assets, zipName)
	if err != nil {
		return "", err
	}
	sumURL, err := assetURL(assets, checksumsAsset)
	if err != nil {
		return "", fmt.Errorf("%w — refusing to install an unverifiable download", err)
	}

	dir, err := stagingDir(info.Latest)
	if err != nil {
		return "", err
	}
	// Any early return below leaves nothing behind to be mistaken for a
	// verified staging on the next run.
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(dir)
		}
	}()

	progress("Downloading checksums…")
	sumPath := filepath.Join(dir, checksumsAsset)
	if err := download(ctx, sumURL, sumPath); err != nil {
		return "", err
	}
	want, err := checksumFor(sumPath, zipName)
	if err != nil {
		return "", err
	}

	progress(fmt.Sprintf("Downloading %s…", zipName))
	zipPath := filepath.Join(dir, zipName)
	if err := download(ctx, zipURL, zipPath); err != nil {
		return "", err
	}

	progress("Verifying download…")
	got, err := sha256File(zipPath)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(got, want) {
		return "", fmt.Errorf("checksum mismatch for %s (got %s, want %s) — download discarded", zipName, got, want)
	}

	progress("Unpacking…")
	appDir := filepath.Join(dir, "app")
	if err := extractZipFn(zipPath, appDir); err != nil {
		return "", err
	}
	if err := verifyPayload(appDir); err != nil {
		return "", err
	}
	// The zip has served its purpose; the unpacked exes are what we keep
	// until the user restarts.
	_ = os.Remove(zipPath)
	ok = true
	return appDir, nil
}

// extractPayload unpacks exactly the two executables an update replaces
// and ignores every other entry.
//
// Entries are matched against a fixed list of names, and only at the
// top level of the archive — build.sh zips the two binaries with no
// directory around them. Nothing derived from an entry name ever
// reaches filepath.Join, which is what makes zip-slip structurally
// impossible here rather than something a check has to catch. Matching
// the top level only also means a stray docs/hivegui.exe cannot shadow
// the real one, and a release that grows extra files does not silently
// install them.
func extractPayload(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", filepath.Base(zipPath), err)
	}
	defer zr.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	found := map[string]bool{}
	for _, f := range zr.File {
		name, ok := payloadEntry(f.Name)
		if !ok || found[name] {
			continue
		}
		if err := extractOne(f, filepath.Join(dest, name)); err != nil {
			return err
		}
		found[name] = true
	}
	for _, name := range payloadExes {
		if !found[name] {
			return fmt.Errorf("%s contains no %s", filepath.Base(zipPath), name)
		}
	}
	return nil
}

// payloadEntry maps a zip entry name to the payload file it is, if any.
// Zip names always use forward slashes; a backslash in one is not a
// separator but it is a good sign the archive is not ours, so both are
// treated as disqualifying.
func payloadEntry(entry string) (string, bool) {
	if strings.ContainsAny(entry, `/\`) {
		return "", false
	}
	for _, want := range payloadExes {
		if strings.EqualFold(entry, want) {
			return want, true
		}
	}
	return "", false
}

func extractOne(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("read %s from archive: %w", f.Name, err)
	}
	defer rc.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	// Same cap the download honours: an entry that claims to be
	// enormous must not fill the disk on the way out of the archive.
	n, err := io.Copy(out, io.LimitReader(rc, maxDownloadBytes+1))
	if err != nil {
		return fmt.Errorf("unpack %s: %w", f.Name, err)
	}
	if n > maxDownloadBytes {
		return fmt.Errorf("unpack %s: larger than %d bytes", f.Name, maxDownloadBytes)
	}
	return out.Close()
}

// ---------------------------- latest channel -----------------------------

// stageLatest fast-forwards the source checkout and builds it.
//
// Both refusals below are pre-flighted before git or the build runs.
// That ordering is the whole point: a build is minutes long, and a
// failure the updater could have predicted from the start should not
// cost the user those minutes with the button stuck on "Updating…".
func stageLatest(info UpdateInfo, progress func(string)) (string, error) {
	settings, err := loadUpdateSettings()
	if err != nil {
		return "", err
	}
	repo, err := resolveSourceRepo(settings.SourceRepo)
	if err != nil {
		return "", err
	}
	buildDir := buildOutputDir(repo)

	install, err := installDirFn()
	if err != nil {
		return "", err
	}
	if err := checkLatestInstallLayout(install, buildDir); err != nil {
		return "", err
	}
	if err := ensureWritable(install); err != nil {
		return "", err
	}

	progress("Checking working tree…")
	dirty, err := runGitFn(repo, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(dirty) != "" {
		return "", fmt.Errorf("%s has uncommitted changes — commit or stash them first", repo)
	}
	if _, err := runGitFn(repo, "symbolic-ref", "--quiet", "HEAD"); err != nil {
		return "", fmt.Errorf("%s has a detached HEAD — check out a branch first", repo)
	}

	// The checkout path comes out of update.json, and validateSourceRepo
	// only proves the directory *looks* like hive — .git, build.sh and a
	// module line are all plantable. Pinning the upstream remote is the
	// check that the code about to be pulled and executed is actually
	// ours.
	if err := verifyUpstreamRemote(repo); err != nil {
		return "", err
	}

	progress("Pulling latest commits…")
	// core.hooksPath=/dev/null: a pull runs the checkout's own hooks
	// (post-merge, post-checkout) before build.sh gets a turn, so a
	// planted hook would execute from a button press. Nothing this
	// button does needs hooks.
	if _, err := runGitFn(repo, "-c", "core.hooksPath=/dev/null", "pull", "--ff-only"); err != nil {
		return "", err
	}

	progress("Building… (this takes a few minutes)")
	if err := runBuildFn(repo, progress); err != nil {
		return "", err
	}
	if err := verifyPayload(buildDir); err != nil {
		return "", err
	}
	return buildDir, nil
}

// buildOutputDir is where build.sh leaves the Windows binaries.
func buildOutputDir(repo string) string {
	return filepath.Join(repo, "cmd", "hivegui", "build", "bin")
}

// checkLatestInstallLayout refuses the one layout the latest channel
// cannot update: Hive running straight out of its own build directory.
//
// `wails build -clean` wipes cmd/hivegui/build/bin before writing to
// it, and Windows will not delete a mapped image — so the build would
// fail on the running hivegui.exe before producing anything. macOS
// never hits this because POSIX unlink happily removes a running
// binary's directory entry.
//
// Refusing is the deliberate choice over working around it. The
// workaround is to move the running images out of the install directory
// for the duration of a multi-minute build, which leaves the user with
// no installed Hive if the app dies in the middle.
func checkLatestInstallLayout(install, buildDir string) error {
	if !sameDir(install, buildDir) {
		return nil
	}
	return fmt.Errorf("Hive is running from its own build directory (%s), which the build step has to erase. "+
		"Copy %s and %s somewhere outside the checkout — %%LOCALAPPDATA%%\\Programs\\Hive is the usual spot — "+
		"and run Hive from there; updates then apply in place", install, guiExe, daemonExe)
}

// runBuildScript runs build.sh through bash and streams its output into
// progress. Only the most recent line is reported — build.sh is chatty
// and the button has one line to show it in.
//
// build.sh is a shell script, so Windows needs bash to run it at all;
// Git for Windows ships one. The invocation is `bash -lc` for the login
// PATH (go, npm and wails routinely live somewhere a GUI process's own
// PATH never saw) with an explicit cd, because a login shell may run a
// profile that changes directory out from under cmd.Dir.
func runBuildScript(repo string, progress func(string)) error {
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()

	bash, err := findBash()
	if err != nil {
		return err
	}
	// Probe through bash rather than against this process's PATH: the
	// login shell is the environment the build actually runs in, and
	// refusing on our own narrower PATH would reject builds that would
	// have worked.
	if missing, err := buildToolProbeFn(ctx, bash, repo); err != nil {
		log.Printf("hivegui: could not probe build tools: %v; letting build.sh report it", err)
	} else if len(missing) > 0 {
		return fmt.Errorf("build.sh needs %s: not found on the PATH your login shell reports. "+
			"Install them, then try again", strings.Join(missing, " and "))
	}

	cmd := proc.CommandContext(ctx, bash, "-lc", buildCommandLine(repo))
	cmd.Dir = repo
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("run build.sh: %w", err)
	}
	var tail string
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := plainProgressLine(sc.Text())
		if line == "" {
			continue
		}
		tail = line
		progress(line)
	}
	// A scanner error (a line past the 1MB cap, a read fault) stops the
	// loop with the pipe still open. build.sh then blocks on a full pipe
	// buffer and cmd.Wait sits there until the timeout, with the button
	// stuck on "Updating…" the whole time. Draining lets the child
	// finish and Wait return.
	if err := sc.Err(); err != nil {
		log.Printf("hivegui: build.sh output scan stopped early: %v", err)
		_, _ = io.Copy(io.Discard, stdout)
	}
	if err := cmd.Wait(); err != nil {
		if tail != "" {
			return fmt.Errorf("build.sh failed: %s", tail)
		}
		return fmt.Errorf("build.sh failed: %w", err)
	}
	return nil
}

// buildCommandLine is the script handed to `bash -lc`.
//
// Git Bash accepts a Windows path with forward slashes, which avoids a
// dependency on cygpath being present. Single-quoted so a directory
// containing a space or a shell metacharacter cannot split the command;
// an embedded quote is escaped the POSIX way.
func buildCommandLine(repo string) string {
	return "cd " + shellQuote(filepath.ToSlash(repo)) + " && ./build.sh --platform windows"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ------------------------------- apply -----------------------------------

// applyStagedBundle replaces the installed executables with the staged
// ones.
//
// Both refusals are re-checked here rather than trusted from staging
// time: staging can be minutes old, and the user may have moved Hive or
// changed permissions in between.
func applyStagedBundle(staged string) error {
	install, err := installDirFn()
	if err != nil {
		return fmt.Errorf("locate running binary: %w", err)
	}
	if sameDir(staged, install) {
		return fmt.Errorf("the staged build is already installed at %s", install)
	}
	if err := ensureWritable(install); err != nil {
		return err
	}
	if err := verifyPayload(staged); err != nil {
		return err
	}
	return swapExes(staged, install)
}

// swapped records one file's progress through swapExes so a later
// failure can undo it.
type swapped struct {
	target    string
	aside     string // "" when there was no previous file to move
	installed bool
}

// swapExes lands every staged executable over its installed
// counterpart.
//
// The shape is the same as swapBundle's on macOS, for the same reason:
// each incoming file is copied to a *sibling* of its target first, so
// the rename that actually installs it is same-directory and
// effectively atomic even when staging lives on another volume.
//
// What differs is why the rename is needed at all. Windows refuses to
// overwrite or delete an executable while its image is mapped, but it
// *does* allow renaming one — which is what lets a running hivegui.exe
// replace itself with no helper process and no elevation. The
// displaced image cannot be deleted until the process that owns it
// exits, so it is left behind and swept by pruneRenamedAside on the
// next start.
func swapExes(staged, install string) error {
	var incoming []string
	cleanupIncoming := func() {
		for _, p := range incoming {
			_ = os.Remove(p)
		}
	}

	// Phase 1: land every incoming file next to its target. Nothing is
	// displaced yet, so a failure here costs nothing.
	for _, name := range payloadExes {
		dst := filepath.Join(install, "."+name+".new")
		_ = os.Remove(dst)
		if err := copyFile(filepath.Join(staged, name), dst); err != nil {
			cleanupIncoming()
			return fmt.Errorf("stage %s into %s: %w", name, install, err)
		}
		incoming = append(incoming, dst)
	}

	// Phase 2: displace and install, one file at a time, remembering
	// enough to walk it back.
	var done []swapped
	rollback := func() {
		for i := len(done) - 1; i >= 0; i-- {
			d := done[i]
			if d.installed {
				// The file at target is the one we just put there, not a
				// mapped image, so this removal succeeds.
				_ = os.Remove(d.target)
			}
			if d.aside != "" {
				_ = os.Rename(d.aside, d.target)
			}
		}
	}

	for _, name := range payloadExes {
		target := filepath.Join(install, name)
		rec := swapped{target: target}
		if _, err := os.Stat(target); err == nil {
			aside, err := freeAsidePath(install, name)
			if err != nil {
				rollback()
				cleanupIncoming()
				return err
			}
			if err := os.Rename(target, aside); err != nil {
				rollback()
				cleanupIncoming()
				return fmt.Errorf("move aside %s: %w", target, err)
			}
			rec.aside = aside
		}
		if err := os.Rename(filepath.Join(install, "."+name+".new"), target); err != nil {
			// Put this file's previous image back before unwinding the
			// rest, so the rollback leaves a launchable Hive.
			if rec.aside != "" {
				_ = os.Rename(rec.aside, target)
			}
			rollback()
			cleanupIncoming()
			return fmt.Errorf("install %s: %w", target, err)
		}
		rec.installed = true
		done = append(done, rec)
	}

	// Best effort: the images belonging to processes that are still
	// running will refuse, which is exactly what pruneRenamedAside is
	// for.
	for _, d := range done {
		if d.aside != "" {
			_ = os.Remove(d.aside)
		}
	}
	return nil
}

// freeAsidePath returns a path the displaced image can be renamed to.
//
// The plain ".<name>.old" is reused whenever it can be deleted first.
// It usually can — it belongs to a process that has since exited — but
// an update applied twice without a restart in between leaves one that
// is still mapped, and renaming onto an existing file fails. Counting
// up is what keeps the second update working.
func freeAsidePath(install, name string) (string, error) {
	base := filepath.Join(install, "."+name+".old")
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base, nil
	}
	if err := os.Remove(base); err == nil {
		return base, nil
	}
	for i := 1; i < 100; i++ {
		p := fmt.Sprintf("%s.%d", base, i)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p, nil
		}
		if err := os.Remove(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no free name to move aside %s — too many pending updates in %s, restart Hive first", name, install)
}

// pruneRenamedAside removes the displaced images and any half-landed
// incoming files left in the install directory.
//
// Called once at startup, which is the first moment the previous
// process is guaranteed gone and its image therefore deletable.
// Best-effort throughout: a file that still will not go costs disk, not
// correctness, and must never stop the GUI from starting.
func pruneRenamedAside() {
	install, err := installDirFn()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(install)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !isSwapLeftover(e.Name()) {
			continue
		}
		if err := os.Remove(filepath.Join(install, e.Name())); err != nil {
			log.Printf("hivegui: could not remove leftover %s: %v", e.Name(), err)
		}
	}
}

// isSwapLeftover reports whether a file name is one swapExes creates:
// ".<exe>.new", ".<exe>.old", or a counted ".<exe>.old.N".
func isSwapLeftover(name string) bool {
	for _, exe := range payloadExes {
		prefix := "." + exe + "."
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := name[len(prefix):]
		if rest == "new" || rest == "old" || strings.HasPrefix(rest, "old.") {
			return true
		}
	}
	return false
}

// ------------------------------ helpers ----------------------------------

// verifyPayload checks that what we are about to install actually looks
// like Hive: both executables present and non-empty. A zip that
// unpacked into something else, or a build that half-failed, must not
// reach the swap.
//
// Size rather than the mode bits macOS checks: Windows has no
// executable bit, so an empty or truncated file is the failure actually
// worth catching.
func verifyPayload(dir string) error {
	for _, name := range payloadExes {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("staged build is missing %s", name)
		}
		if st.IsDir() {
			return fmt.Errorf("staged %s is a directory", name)
		}
		if st.Size() == 0 {
			return fmt.Errorf("staged %s is empty", name)
		}
	}
	return nil
}

// installDir is the directory the running executables live in.
func installDir() (string, error) {
	self, err := executablePath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(self), nil
}

// ensureWritable refuses an install directory this process cannot write
// to, by actually writing to it rather than inspecting an ACL.
//
// Hive on Windows ships as a zip with no installer, so the normal place
// for it is somewhere the user owns. An install under Program Files
// would need elevation, and the deliberate answer there is to refuse
// and say so: an elevated helper copying from a user-writable staging
// directory is a privilege-escalation vector, which is a poor trade for
// a convenience feature.
func ensureWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".hive-update-probe-*")
	if err != nil {
		return fmt.Errorf("cannot update in place: %s is not writable by Hive. "+
			"Move Hive to a folder you own — %%LOCALAPPDATA%%\\Programs\\Hive is the usual spot — and run it from there", dir)
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return nil
}

// sameDir reports whether two paths name the same directory, allowing
// for case differences and for one of them being reached through a
// symlink or a junction.
func sameDir(a, b string) bool {
	norm := func(p string) string {
		p = filepath.Clean(p)
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		return strings.ToLower(p)
	}
	return norm(a) == norm(b)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}
