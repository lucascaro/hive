package main

import (
	"path/filepath"
	"testing"
)

// Tests for the portable half of the staging path
// (update_apply_common.go). They moved here from
// update_apply_darwin_test.go with the code they cover: the helpers are
// the same work on every platform, and running them on the macOS leg
// alone left the Linux and Windows legs blind to a regression in the
// checksum manifest parser or the staging-path sanitiser.

func TestChecksumForAcceptsBinaryMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, checksumsAsset)
	writeFile(t, path, "aaaa  plain.zip\nbbbb *binary.zip\n")

	if got, err := checksumFor(path, "plain.zip"); err != nil || got != "aaaa" {
		t.Errorf("checksumFor(plain.zip) = %q, %v; want aaaa, nil", got, err)
	}
	if got, err := checksumFor(path, "binary.zip"); err != nil || got != "bbbb" {
		t.Errorf("checksumFor(binary.zip) = %q, %v; want bbbb, nil", got, err)
	}
	if _, err := checksumFor(path, "missing.zip"); err == nil {
		t.Error("checksumFor(missing.zip) = nil error, want a refusal")
	}
}

// A tag_name is remote input and lands in a filesystem path; it must not
// be able to choose where we write.
func TestStagingDirSanitizesVersion(t *testing.T) {
	isolateStateDir(t)
	dir, err := stagingDir("../../escape")
	if err != nil {
		t.Fatalf("stagingDir: %v", err)
	}
	if filepath.Dir(dir) != updatesRoot() {
		t.Errorf("stagingDir = %q, want it inside %q", dir, updatesRoot())
	}
}
