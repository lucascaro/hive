package main

// Portable halves of the update staging path.
//
// These moved out of update_apply_darwin.go unchanged when Windows grew
// its own implementation (update_apply_windows.go). Nothing in here is
// platform-specific: fetching the release asset list, enforcing the URL
// allowlist, the staging directory layout, the capped download, the
// checksum manifest, the upstream-remote pin, and the progress-line
// sanitiser are the same work on every platform.
//
// What stayed platform-specific is the orchestration that uses them —
// stageRelease/stageLatest/applyStagedBundle — because the steps
// genuinely differ: ditto + codesign + an .app bundle on macOS, stdlib
// zip + two loose .exe files on Windows.

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/lucascaro/hive/internal/registry"
)

// checksumsAsset is the SHA-256 manifest scripts/release-artifacts.sh
// attaches alongside the binaries.
const checksumsAsset = "checksums.txt"

// updatesRoot is where downloaded/unpacked updates are staged until the
// user restarts. Under the state dir so it inherits HIVE_STATE_DIR
// isolation and never lands in the app bundle we are about to replace.
func updatesRoot() string {
	return filepath.Join(registry.StateDir(), "updates")
}

// pruneStagingDirs removes everything under <stateDir>/updates. Called
// once the staged bundle has been installed, at which point every
// directory in there is spent: the release channel re-downloads on the
// next update, and the latest channel stages inside the checkout.
//
// Best-effort — a failure here costs disk, not correctness, and must
// never turn a successful install into a reported failure.
func pruneStagingDirs() {
	if err := os.RemoveAll(updatesRoot()); err != nil {
		log.Printf("hivegui: could not prune %s: %v", updatesRoot(), err)
	}
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
