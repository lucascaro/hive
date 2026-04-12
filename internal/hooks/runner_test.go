//go:build !windows

package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/lucascaro/hive/internal/state"
)

// makeExecutable creates a file with executable permissions.
func makeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

// makeNonExecutable creates a file without executable permissions.
func makeNonExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func TestFindScripts_FlatFileFound(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "on-session-create")
	makeExecutable(t, script, "#!/bin/sh\n")

	got := findScripts(dir, "session-create")
	if len(got) != 1 || got[0] != script {
		t.Errorf("findScripts() = %v, want [%q]", got, script)
	}
}

func TestFindScripts_NonExecutableSkipped(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "on-session-create")
	makeNonExecutable(t, script, "#!/bin/sh\n")

	got := findScripts(dir, "session-create")
	if len(got) != 0 {
		t.Errorf("findScripts() = %v, want empty (non-executable skipped)", got)
	}
}

func TestFindScripts_DotDDirScripts(t *testing.T) {
	dir := t.TempDir()
	dotD := filepath.Join(dir, "on-session-create.d")
	if err := os.MkdirAll(dotD, 0o755); err != nil {
		t.Fatal(err)
	}
	s1 := filepath.Join(dotD, "10-first")
	s2 := filepath.Join(dotD, "20-second")
	makeExecutable(t, s1, "#!/bin/sh\n")
	makeExecutable(t, s2, "#!/bin/sh\n")

	got := findScripts(dir, "session-create")
	if len(got) != 2 {
		t.Fatalf("findScripts() len = %d, want 2; got %v", len(got), got)
	}
}

func TestFindScripts_DotDSortedAlphabetically(t *testing.T) {
	dir := t.TempDir()
	dotD := filepath.Join(dir, "on-session-create.d")
	if err := os.MkdirAll(dotD, 0o755); err != nil {
		t.Fatal(err)
	}
	// Create in reverse order to verify sorting
	s3 := filepath.Join(dotD, "30-third")
	s1 := filepath.Join(dotD, "10-first")
	s2 := filepath.Join(dotD, "20-second")
	makeExecutable(t, s3, "#!/bin/sh\n")
	makeExecutable(t, s1, "#!/bin/sh\n")
	makeExecutable(t, s2, "#!/bin/sh\n")

	got := findScripts(dir, "session-create")
	if len(got) != 3 {
		t.Fatalf("findScripts() len = %d, want 3; got %v", len(got), got)
	}
	if got[0] != s1 || got[1] != s2 || got[2] != s3 {
		t.Errorf("findScripts() not sorted: %v", got)
	}
}

func TestFindScripts_FlatAndDotD(t *testing.T) {
	dir := t.TempDir()
	flat := filepath.Join(dir, "on-session-create")
	makeExecutable(t, flat, "#!/bin/sh\n")

	dotD := filepath.Join(dir, "on-session-create.d")
	if err := os.MkdirAll(dotD, 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dotD, "10-sub")
	makeExecutable(t, sub, "#!/bin/sh\n")

	got := findScripts(dir, "session-create")
	if len(got) != 2 {
		t.Fatalf("findScripts() len = %d, want 2; got %v", len(got), got)
	}
}

func TestFindScripts_MissingDir(t *testing.T) {
	got := findScripts("/nonexistent/hooks/dir", "session-create")
	if len(got) != 0 {
		t.Errorf("findScripts() = %v for missing dir, want empty", got)
	}
}

func TestFindScripts_DirectoriesInDotDSkipped(t *testing.T) {
	dir := t.TempDir()
	dotD := filepath.Join(dir, "on-session-create.d")
	if err := os.MkdirAll(filepath.Join(dotD, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := findScripts(dir, "session-create")
	if len(got) != 0 {
		t.Errorf("findScripts() = %v, directories should be skipped", got)
	}
}

func TestRun_ExecutesScript(t *testing.T) {
	dir := t.TempDir()
	markerFile := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "on-session-create")
	makeExecutable(t, script, fmt.Sprintf("#!/bin/sh\ntouch %q\n", markerFile))

	event := state.HookEvent{Name: "session-create"}
	errs := Run(dir, event)
	if len(errs) != 0 {
		t.Errorf("Run() errors: %v", errs)
	}
	if _, err := os.Stat(markerFile); err != nil {
		t.Errorf("script was not executed (marker file missing): %v", err)
	}
}

func TestRun_ReturnsErrorForFailingScript(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "on-session-create")
	makeExecutable(t, script, "#!/bin/sh\nexit 1\n")

	event := state.HookEvent{Name: "session-create"}
	errs := Run(dir, event)
	if len(errs) == 0 {
		t.Error("Run() should return error for failing script")
	}
}

func TestRun_NoScriptsNoErrors(t *testing.T) {
	dir := t.TempDir()
	event := state.HookEvent{Name: "session-create"}
	errs := Run(dir, event)
	if len(errs) != 0 {
		t.Errorf("Run() with no scripts returned errors: %v", errs)
	}
}

func TestRun_EnvironmentPassedToScript(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "env_out")
	script := filepath.Join(dir, "on-session-create")
	// Write the HIVE_EVENT env var to a file so we can read it back.
	makeExecutable(t, script, fmt.Sprintf("#!/bin/sh\necho \"$HIVE_EVENT\" > %q\n", outFile))

	event := state.HookEvent{Name: "session-create"}
	errs := Run(dir, event)
	if len(errs) != 0 {
		t.Fatalf("Run() errors: %v", errs)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", outFile, err)
	}
	got := string(data)
	if len(got) == 0 || got == "\n" {
		t.Errorf("HIVE_EVENT not passed to script, got: %q", got)
	}
}
