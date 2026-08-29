package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/registry"
)

// bundleName is the .app produced by build.sh and shipped inside the
// release zip. Keep in sync with build.sh's `wails build` output.
const bundleName = "hivegui.app"

// checksumsAsset is the SHA-256 manifest scripts/release.sh attaches
// alongside the binaries.
const checksumsAsset = "checksums.txt"

// maxDownloadBytes caps what we will pull from a release asset. The
// macOS zip is tens of megabytes; this is a sanity bound so a wrong or
// hostile Content-Length can't fill the user's disk.
const maxDownloadBytes = 512 << 20

// downloadTimeout bounds a whole staging download.
var downloadTimeout = 15 * time.Minute

// buildTimeout bounds a latest-channel `./build.sh`. A cold universal
// Wails build is minutes, not seconds.
var buildTimeout = 30 * time.Minute

// Seams for tests, mirroring looksLikeHivedFn in restart_unix.go.
var (
	// extractZipFn unpacks a release zip into a directory. ditto is the
	// macOS-native answer: archive/zip loses the symlinks and mode bits
	// an .app bundle needs, and a bundle whose binary lost its +x is a
	// broken install with no in-app way back.
	extractZipFn = dittoExtract
	// copyBundleFn duplicates a bundle. Used to land the staged app
	// next to the installed one before the rename swap.
	copyBundleFn = dittoCopy
	// runBuildFn runs ./build.sh in a checkout, streaming progress.
	runBuildFn = runBuildScript
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

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// stageRelease downloads the macOS zip for the newest release, verifies
// it against the published SHA-256 manifest, and unpacks it.
//
// The checksum is not a supply-chain defense — the bundle is neither
// signed nor notarized, and a compromised release would publish a
// matching manifest. It is there so a truncated or corrupted download
// fails loudly here instead of becoming a broken hivegui.app that the
// user can only fix by reinstalling by hand.
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
	// The zip is tens of MB and has served its purpose; the bundle is
	// what we keep until the user restarts.
	_ = os.Remove(zipPath)
	ok = true
	return bundle, nil
}

func fetchReleaseAssets(ctx context.Context) ([]releaseAsset, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, updateReleasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases: HTTP %d", resp.StatusCode)
	}
	var rel struct {
		Assets []releaseAsset `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	return rel.Assets, nil
}

// assetURL finds one asset by name and enforces the same URL allowlist
// the banner's Download button uses. A download URL pointing anywhere
// other than this repo's releases is refused outright: unlike the
// browser hand-off, what comes back here gets unpacked and executed.
func assetURL(assets []releaseAsset, name string) (string, error) {
	for _, a := range assets {
		if a.Name != name {
			continue
		}
		if !strings.HasPrefix(a.URL, updateURLPrefix) {
			return "", fmt.Errorf("release asset %s has an unexpected download URL", name)
		}
		return a.URL, nil
	}
	return "", fmt.Errorf("release has no %s asset", name)
}

// updatesRoot is where downloaded/unpacked updates are staged until the
// user restarts. Under the state dir so it inherits HIVE_STATE_DIR
// isolation and never lands in the app bundle we are about to replace.
func updatesRoot() string {
	return filepath.Join(registry.StateDir(), "updates")
}

func stagingDir(version string) (string, error) {
	// Slug the version into the path rather than interpolating it raw:
	// it comes from a remote tag_name, and a "../.." in there would
	// otherwise choose where we write.
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, version)
	dir := filepath.Join(updatesRoot(), safe)
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("clear staging dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}
	return dir, nil
}

func download(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", filepath.Base(dest), resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	// +1 so a body that is exactly at the cap is still detected as over
	// it rather than silently truncated into a checksum failure.
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxDownloadBytes+1))
	if err != nil {
		return fmt.Errorf("download %s: %w", filepath.Base(dest), err)
	}
	if n > maxDownloadBytes {
		return fmt.Errorf("download %s: larger than %d bytes", filepath.Base(dest), int64(maxDownloadBytes))
	}
	return f.Close()
}

// checksumFor reads a `shasum -a 256` style manifest ("<hex>  <name>")
// and returns the digest recorded for name.
func checksumFor(path, name string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		// shasum writes "*name" in binary mode; accept both spellings.
		if strings.TrimPrefix(fields[1], "*") == name {
			return fields[0], nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s lists no checksum for %s", checksumsAsset, name)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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

	progress("Pulling latest commits…")
	if _, err := runGitFn(repo, "pull", "--ff-only"); err != nil {
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
	cmd := exec.CommandContext(ctx, "./build.sh")
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
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		tail = line
		progress(line)
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
// the `zip -rq` build.sh packages with, and unlike archive/zip it
// preserves the symlinks and permissions an .app bundle depends on.
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
