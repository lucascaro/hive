package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeHiveCheckout builds the minimum tree resolveSourceRepo accepts:
// a .git dir, a build.sh, and a go.mod declaring the hive module.
func fakeHiveCheckout(t *testing.T) string {
	t.Helper()
	return checkoutWithModule(t, hiveModulePath)
}

func checkoutWithModule(t *testing.T, module string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "build.sh"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(dir, "go.mod"), "module "+module+"\n\ngo 1.24\n")
	// t.TempDir() on macOS lives under /var, a symlink to /private/var;
	// resolveSourceRepo walks the resolved path, so the expectation has
	// to be resolved too or every comparison here is off by that link.
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func TestResolveSourceRepoPrefersConfigured(t *testing.T) {
	configured := fakeHiveCheckout(t)
	// Auto-detect would find this other one; the explicit choice must win.
	other := fakeHiveCheckout(t)
	executablePath = func() (string, error) {
		return filepath.Join(other, "cmd", "hivegui", "hivegui"), nil
	}
	t.Cleanup(func() { executablePath = os.Executable })

	got, err := resolveSourceRepo(configured)
	if err != nil {
		t.Fatalf("resolveSourceRepo: %v", err)
	}
	if got != configured {
		t.Errorf("resolveSourceRepo = %q, want the configured %q", got, configured)
	}
}

func TestResolveSourceRepoWalksUpToGoMod(t *testing.T) {
	repo := fakeHiveCheckout(t)
	deep := filepath.Join(repo, "cmd", "hivegui", "build", "bin", "hivegui.app", "Contents", "MacOS")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	executablePath = func() (string, error) { return filepath.Join(deep, "hivegui"), nil }
	t.Cleanup(func() { executablePath = os.Executable })

	got, err := resolveSourceRepo("")
	if err != nil {
		t.Fatalf("resolveSourceRepo: %v", err)
	}
	if got != repo {
		t.Errorf("resolveSourceRepo = %q, want %q", got, repo)
	}
}

func TestResolveSourceRepoRejectsForeignModule(t *testing.T) {
	// A Go project that is not hive must not be pulled and built just
	// because it happens to enclose the binary.
	foreign := checkoutWithModule(t, "example.com/not-hive")
	executablePath = func() (string, error) { return filepath.Join(foreign, "bin", "hivegui"), nil }
	t.Cleanup(func() { executablePath = os.Executable })

	if _, err := resolveSourceRepo(""); err == nil {
		t.Fatal("resolveSourceRepo = nil error for a foreign module, want a refusal")
	}

	if _, err := resolveSourceRepo(foreign); err == nil {
		t.Fatal("resolveSourceRepo(configured foreign repo) = nil error, want a refusal")
	} else if !strings.Contains(err.Error(), hiveModulePath) {
		t.Errorf("error = %q, want it to name the expected module", err)
	}
}

func TestResolveSourceRepoRejectsIncompleteCheckout(t *testing.T) {
	repo := fakeHiveCheckout(t)
	if err := os.Remove(filepath.Join(repo, "build.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveSourceRepo(repo); err == nil {
		t.Fatal("resolveSourceRepo = nil error for a checkout with no build.sh, want a refusal")
	}
}
