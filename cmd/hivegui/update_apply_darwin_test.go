package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubBundle writes a minimal hivegui.app that verifyBundle accepts.
func stubBundle(t *testing.T, dir, marker string) string {
	t.Helper()
	bundle := filepath.Join(dir, bundleName)
	macos := filepath.Join(bundle, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"hivegui", "hived"} {
		if err := os.WriteFile(filepath.Join(macos, name), []byte(marker), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return bundle
}

// releaseServer serves a releases/latest payload plus its assets.
// assets maps asset name → body; urlHost lets a test point one asset
// somewhere off the allowlist.
func releaseServer(t *testing.T, assets map[string][]byte, override map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		type asset struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		}
		var list []asset
		for name := range assets {
			url := srv.URL + "/dl/" + name
			if o, ok := override[name]; ok {
				url = o
			}
			list = append(list, asset{Name: name, URL: url})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"assets": list})
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		body, ok := assets[strings.TrimPrefix(r.URL.Path, "/dl/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(body)
	})

	prevAPI, prevPrefix := updateReleasesAPI, updateURLPrefix
	updateReleasesAPI = srv.URL + "/releases/latest"
	updateURLPrefix = srv.URL + "/"
	t.Cleanup(func() { updateReleasesAPI, updateURLPrefix = prevAPI, prevPrefix })
	return srv
}

func zipOfBundle(t *testing.T) []byte {
	t.Helper()
	var buf strings.Builder
	w := zip.NewWriter(&buf)
	f, err := w.Create(bundleName + "/Contents/MacOS/hivegui")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("new"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return []byte(buf.String())
}

func sha256Of(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestStageReleaseRejectsChecksumMismatch(t *testing.T) {
	isolateStateDir(t)
	body := zipOfBundle(t)
	name := "Hive-9.9.9-macos-universal.zip"
	releaseServer(t, map[string][]byte{
		name:           body,
		checksumsAsset: []byte(sha256Of([]byte("something else")) + "  " + name + "\n"),
	}, nil)

	extracted := false
	prev := extractZipFn
	extractZipFn = func(string, string) error { extracted = true; return nil }
	t.Cleanup(func() { extractZipFn = prev })

	_, err := stageRelease(UpdateInfo{Channel: ChannelRelease, Latest: "9.9.9"}, func(string) {})
	if err == nil {
		t.Fatal("stageRelease = nil error on a checksum mismatch, want a refusal")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error = %q, want it to name the checksum mismatch", err)
	}
	if extracted {
		t.Error("stageRelease unpacked an archive that failed verification")
	}
	if _, statErr := os.Stat(filepath.Join(updatesRoot(), "9.9.9")); statErr == nil {
		t.Error("staging dir survived a failed verification; a later run could mistake it for verified")
	}
}

func TestStageReleaseRejectsOffPrefixAssetURL(t *testing.T) {
	isolateStateDir(t)
	body := zipOfBundle(t)
	name := "Hive-9.9.9-macos-universal.zip"
	releaseServer(t, map[string][]byte{
		name:           body,
		checksumsAsset: []byte(sha256Of(body) + "  " + name + "\n"),
	}, map[string]string{name: "https://evil.example.com/Hive.zip"})

	_, err := stageRelease(UpdateInfo{Channel: ChannelRelease, Latest: "9.9.9"}, func(string) {})
	if err == nil {
		t.Fatal("stageRelease = nil error for an off-allowlist asset URL, want a refusal")
	}
	if !strings.Contains(err.Error(), "unexpected download URL") {
		t.Errorf("error = %q, want it to name the URL as the problem", err)
	}
}

func TestStageReleaseRequiresChecksumManifest(t *testing.T) {
	isolateStateDir(t)
	body := zipOfBundle(t)
	name := "Hive-9.9.9-macos-universal.zip"
	releaseServer(t, map[string][]byte{name: body}, nil)

	_, err := stageRelease(UpdateInfo{Channel: ChannelRelease, Latest: "9.9.9"}, func(string) {})
	if err == nil {
		t.Fatal("stageRelease = nil error with no checksums.txt, want a refusal")
	}
	if !strings.Contains(err.Error(), "unverifiable") {
		t.Errorf("error = %q, want it to say the download can't be verified", err)
	}
}

func TestStageLatestRefusesDirtyWorktree(t *testing.T) {
	dir := isolateStateDir(t)
	repo := fakeHiveCheckout(t)
	writeFile(t, filepath.Join(dir, "update.json"),
		fmt.Sprintf(`{"channel":"latest","source_repo":%q}`, repo))

	g := &fakeGit{answers: map[string]string{
		"status --porcelain": " M cmd/hivegui/app.go",
	}}
	g.install(t)
	built := false
	prev := runBuildFn
	runBuildFn = func(string, func(string)) error { built = true; return nil }
	t.Cleanup(func() { runBuildFn = prev })

	_, err := stageLatest(UpdateInfo{Channel: ChannelLatest}, func(string) {})
	if err == nil {
		t.Fatal("stageLatest = nil error on a dirty tree, want a refusal")
	}
	if !strings.Contains(err.Error(), "uncommitted changes") {
		t.Errorf("error = %q, want it to name the uncommitted changes", err)
	}
	if g.ran("pull") {
		t.Error("stageLatest pulled over a dirty working tree")
	}
	if built {
		t.Error("stageLatest built despite refusing to pull")
	}
}

func TestApplyRefusesOutsideAppBundle(t *testing.T) {
	dir := t.TempDir()
	executablePath = func() (string, error) { return filepath.Join(dir, "hivegui"), nil }
	t.Cleanup(func() { executablePath = os.Executable })

	err := applyStagedBundle(stubBundle(t, t.TempDir(), "new"))
	if err == nil {
		t.Fatal("applyStagedBundle = nil error outside an .app bundle, want a refusal")
	}
	if !strings.Contains(err.Error(), ".app bundle") {
		t.Errorf("error = %q, want it to explain there is no bundle to replace", err)
	}
}

func TestSwapBundleReplacesInstalled(t *testing.T) {
	staged := stubBundle(t, t.TempDir(), "new")
	installDir := t.TempDir()
	installed := stubBundle(t, installDir, "old")

	if err := swapBundle(staged, installed); err != nil {
		t.Fatalf("swapBundle: %v", err)
	}
	got := readMarker(t, installed)
	if got != "new" {
		t.Errorf("installed binary = %q after swap, want %q", got, "new")
	}
	// No scratch left behind next to the app.
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

func TestSwapBundleRollsBackOnFailure(t *testing.T) {
	staged := stubBundle(t, t.TempDir(), "new")
	installDir := t.TempDir()
	installed := stubBundle(t, installDir, "old")

	// Land the incoming copy somewhere the second rename can't find it,
	// so the swap fails exactly after the installed app has been moved
	// aside — the window this rollback exists to close.
	prev := copyBundleFn
	copyBundleFn = func(_, _ string) error { return nil }
	t.Cleanup(func() { copyBundleFn = prev })

	err := swapBundle(staged, installed)
	if err == nil {
		t.Fatal("swapBundle = nil error when the install rename fails, want the failure surfaced")
	}
	if got := readMarker(t, installed); got != "old" {
		t.Fatalf("installed binary = %q after a failed swap, want the original %q back", got, "old")
	}
}

func readMarker(t *testing.T, bundle string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(bundle, "Contents", "MacOS", "hivegui"))
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	return string(b)
}

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
