package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lucascaro/hive/internal/buildinfo"
)

// bundleName is the .app produced by build.sh and shipped inside the
// release zip. Keep in sync with build.sh's `wails build` output.
const bundleName = "hivegui.app"

// Seams for tests, mirroring looksLikeHivedFn in restart_unix.go.
var (
	// extractZipFn unpacks a release zip into a directory. ditto is the
	// macOS-native answer, and the counterpart to the `ditto -c -k`
	// that build.sh packages with: archive/zip loses the symlinks and
	// mode bits an .app bundle needs, and a bundle whose binary lost
	// its +x is a broken install with no in-app way back. It also
	// preserves the stapled notarization ticket, which the signature
	// check below depends on.
	extractZipFn = dittoExtract
	// copyBundleFn duplicates a bundle. Used to land the staged app
	// next to the installed one before the rename swap.
	copyBundleFn = dittoCopy
	// runBuildFn runs ./build.sh in a checkout, streaming progress.
	runBuildFn = runBuildScript
	// verifySignatureFn refuses a staged bundle not signed by Hive's
	// Apple Developer team. Seamed so tests need no real signature.
	verifySignatureFn = verifyDeveloperIDSignature
)

// stageUpdate prepares the new build and returns the staged bundle
// path. Called from StartUpdate's goroutine; progress is reported by
// calling progress with a short human-readable line.
func stageUpdate(info UpdateInfo, progress func(string)) (string, error) {
	if info.Channel == ChannelLatest {
		return stageLatest(info, progress)
	}
	return stageRelease(info, progress)
}

// ---------------------------- release channel ----------------------------

// stageRelease downloads the macOS zip for the newest release,
// verifies it, and unpacks it.
//
// Two independent checks, in order of what they catch:
//
//   - The SHA-256 manifest catches a truncated or corrupted download,
//     cheaply, and is the error users actually hit. It is NOT a
//     supply-chain defense: the zip and the manifest come from the
//     same release, so whoever can publish one publishes the other.
//   - The Developer ID signature check is the supply-chain defense. It
//     pins the bundle to Hive's Apple Developer team, so a release
//     signed by anyone else — including an attacker with their own
//     Apple account — is refused before it can be applied.
//
// The signature is checked after unpacking because codesign and spctl
// read a bundle, not a zip stream. Staging is still fail-closed with
// respect to the installed app: everything lands in a temp directory
// that the deferred cleanup below removes unless every check passed.
func stageRelease(info UpdateInfo, progress func(string)) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	progress("Looking up release…")
	assets, err := fetchReleaseAssets(ctx)
	if err != nil {
		return "", err
	}
	zipName := fmt.Sprintf("Hive-%s-macos-universal.zip", info.Latest)
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
	bundle := filepath.Join(appDir, bundleName)
	if err := verifyBundle(bundle); err != nil {
		return "", err
	}

	// Only announce the step that actually runs: verification is a no-op
	// on a build with no pin, and claiming otherwise tells the user a
	// signature was checked when none was.
	if buildinfo.SigningTeamID() != "" {
		progress("Verifying signature…")
	}
	if err := verifySignatureFn(ctx, bundle); err != nil {
		return "", err
	}
	// The zip is tens of MB and has served its purpose; the bundle is
	// what we keep until the user restarts.
	_ = os.Remove(zipPath)
	ok = true
	return bundle, nil
}

// ---------------------------- latest channel -----------------------------

// stageLatest fast-forwards the source checkout and builds it.
//
// A dirty tree aborts before anything runs: `git pull` on top of
// uncommitted work is how you lose it, and this button is meant to be
// safe to press without thinking.
func stageLatest(info UpdateInfo, progress func(string)) (string, error) {
	settings, err := loadUpdateSettings()
	if err != nil {
		return "", err
	}
	repo, err := resolveSourceRepo(settings.SourceRepo)
	if err != nil {
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
	bundle := filepath.Join(repo, "cmd", "hivegui", "build", "bin", bundleName)
	if err := verifyBundle(bundle); err != nil {
		return "", err
	}
	return bundle, nil
}

// runBuildScript runs ./build.sh and streams its output into progress.
// Only the most recent line is reported — build.sh is chatty and the
// button has one line to show it in.
func runBuildScript(repo string, progress func(string)) error {
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()
	env := envWithLoginPATH(os.Environ())
	// Fail here rather than forty lines into npm's output: a PATH that
	// is still missing the toolchain is the one failure mode this whole
	// probe exists to prevent, and "build.sh failed" does not tell the
	// user which tool to install.
	if missing := missingBuildTools(pathOf(env)); len(missing) > 0 {
		return fmt.Errorf("build.sh needs %s: not found on the PATH %s. "+
			"Install and restart Hive — the PATH is read once at startup",
			strings.Join(missing, " and "), pathSourceDescription())
	}
	cmd := exec.CommandContext(ctx, "./build.sh")
	cmd.Dir = repo
	cmd.Env = env
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
	// buffer and cmd.Wait sits there until the 30-minute timeout, with
	// the button stuck on "Updating…" the whole time. Draining lets the
	// child finish and Wait return.
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

// ------------------------------- apply -----------------------------------

// applyStagedBundle replaces the installed app with the staged one.
//
// Refuses when the running binary is not inside a .app: a `wails dev`
// or `go run` process has no bundle to swap, and guessing at one would
// mean writing over something we did not install.
func applyStagedBundle(staged string) error {
	self, err := executablePath()
	if err != nil {
		return fmt.Errorf("locate running binary: %w", err)
	}
	installed := enclosingAppBundle(self)
	if installed == "" {
		return fmt.Errorf("not running from an .app bundle — rebuild and relaunch manually")
	}
	// Re-verify immediately before the swap. stageRelease already
	// checked this bundle, but that was a separate step and the
	// staging directory is writable in between — a verify-to-install
	// gap. Same-uid only, so this is defense in depth rather than a
	// hole in the stated threat model, and it costs one call.
	//
	// Release stagings only. applyStagedBundle serves both channels,
	// and a latest-channel bundle was built locally from a git
	// checkout with no credentials, so it carries no Developer ID at
	// all — stageLatest deliberately skips this check, its trust root
	// being verifyUpstreamRemote. Verifying it here would tell a user
	// who just waited out a multi-minute build that their own build is
	// "not signed by the Hive developer", and it would only start
	// doing so the day a Team ID is pinned.
	if isDownloadedStaging(staged) {
		if err := verifySignatureFn(context.Background(), staged); err != nil {
			return err
		}
	}
	return swapBundle(staged, installed)
}

// swapBundle lands staged over installed.
//
// The staged bundle is copied to a *sibling* of the installed one
// first, because staging lives under the state dir and the app under
// /Applications: those can be different volumes, where os.Rename fails
// with EXDEV. Once the copy is a sibling, the two renames below are
// same-directory and effectively atomic, and the first is undone if the
// second fails — the failure mode this ordering exists to prevent is an
// app that has been moved aside and not replaced.
func swapBundle(staged, installed string) error {
	parent := filepath.Dir(installed)
	incoming := filepath.Join(parent, "."+filepath.Base(installed)+".new")
	previous := filepath.Join(parent, "."+filepath.Base(installed)+".old")
	_ = os.RemoveAll(incoming)
	_ = os.RemoveAll(previous)

	if err := copyBundleFn(staged, incoming); err != nil {
		os.RemoveAll(incoming)
		return fmt.Errorf("stage into %s: %w", parent, err)
	}
	if err := os.Rename(installed, previous); err != nil {
		os.RemoveAll(incoming)
		return fmt.Errorf("move aside %s: %w", installed, err)
	}
	if err := os.Rename(incoming, installed); err != nil {
		// Put the working app back before reporting; the caller keeps
		// running and the user keeps a launchable Hive.
		if rbErr := os.Rename(previous, installed); rbErr != nil {
			return fmt.Errorf("install failed (%v) AND rollback failed (%v) — %s holds the previous app", err, rbErr, previous)
		}
		os.RemoveAll(incoming)
		return fmt.Errorf("install %s: %w", installed, err)
	}
	os.RemoveAll(previous)
	return nil
}

// ------------------------------ helpers ----------------------------------

// verifyBundle checks that what we are about to install actually looks
// like Hive: both binaries present and executable. A zip that unpacked
// into something else, or a build that half-failed, must not reach the
// swap.
func verifyBundle(bundle string) error {
	for _, name := range []string{"hivegui", "hived"} {
		p := filepath.Join(bundle, "Contents", "MacOS", name)
		st, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("staged bundle is missing %s", name)
		}
		if st.Mode()&0o111 == 0 {
			return fmt.Errorf("staged %s is not executable", name)
		}
	}
	return nil
}

// dittoExtract unpacks a zip. `ditto -x -k` is the macOS counterpart to
// the `ditto -c -k --keepParent` build.sh packages with, and unlike
// archive/zip it preserves the symlinks and permissions an .app bundle
// depends on.
func dittoExtract(zipPath, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return runQuiet("ditto", "-x", "-k", zipPath, dest)
}

func dittoCopy(src, dest string) error {
	return runQuiet("ditto", src, dest)
}

func runQuiet(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s: %s", name, msg)
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// Lives here rather than in update_apply_installable.go: the codesign
// check it gates is macOS-only, so on the Windows leg it was dead code
// — the same unused-symbol problem the split was made to fix.
// isDownloadedStaging reports whether a staged bundle came from
// stageRelease — i.e. we downloaded it into our own staging area —
// rather than from stageLatest, which returns a path inside the
// user's git checkout.
func isDownloadedStaging(staged string) bool {
	root := updatesRoot()
	return strings.HasPrefix(staged, root+string(filepath.Separator))
}
