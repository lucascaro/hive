package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeLocator_PicksMostRecentSinceStart(t *testing.T) {
	root := t.TempDir()
	cwd := "/Users/x/proj"
	dir := filepath.Join(root, "projects", "-Users-x-proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	since := time.Now()

	older := filepath.Join(dir, "older-uuid.jsonl")
	if err := os.WriteFile(older, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Backdate "older" so it's before `since`.
	pre := since.Add(-1 * time.Hour)
	if err := os.Chtimes(older, pre, pre); err != nil {
		t.Fatal(err)
	}

	newer := filepath.Join(dir, "newer-uuid.jsonl")
	if err := os.WriteFile(newer, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	post := since.Add(1 * time.Second)
	if err := os.Chtimes(newer, post, post); err != nil {
		t.Fatal(err)
	}

	got, err := claudeLocator(root, cwd, since)
	if err != nil {
		t.Fatalf("locator: %v", err)
	}
	if got != "newer-uuid" {
		t.Errorf("got %q, want newer-uuid", got)
	}
}

func TestClaudeLocator_LayoutMissing_ReturnsEmptyNoError(t *testing.T) {
	root := t.TempDir()
	got, err := claudeLocator(root, "/Users/x/proj", time.Now())
	if err != nil {
		t.Fatalf("expected nil error on missing layout, got %v", err)
	}
	if got != "" {
		t.Errorf("expected empty id, got %q", got)
	}
}

func TestClaudeLocator_NoFilesAfterSince(t *testing.T) {
	root := t.TempDir()
	cwd := "/Users/x/proj"
	dir := filepath.Join(root, "projects", "-Users-x-proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "old-uuid.jsonl")
	if err := os.WriteFile(old, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(old, stale, stale); err != nil {
		t.Fatal(err)
	}

	got, err := claudeLocator(root, cwd, time.Now())
	if err != nil {
		t.Fatalf("locator: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty (no recent files), got %q", got)
	}
}

func TestCodexLocator_NestedDateDirs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sessions", "2026", "04", "02")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	since := time.Now().Add(-1 * time.Minute)
	want := "019d4d18-0b7d-7751-89ca-8a4386362f54"
	name := "rollout-2026-04-02T00-28-34-" + want + ".jsonl"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := codexLocator(root, "/anywhere", since)
	if err != nil {
		t.Fatalf("locator: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCodexLocator_LayoutMissing_ReturnsEmptyNoError(t *testing.T) {
	root := t.TempDir()
	got, err := codexLocator(root, "/anywhere", time.Now())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != "" {
		t.Errorf("expected empty id, got %q", got)
	}
}

func TestParseCodexUUID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"valid", "rollout-2026-04-02T00-28-34-019d4d18-0b7d-7751-89ca-8a4386362f54.jsonl", "019d4d18-0b7d-7751-89ca-8a4386362f54"},
		{"valid no prefix", "x-019d4d18-0b7d-7751-89ca-8a4386362f54.jsonl", "019d4d18-0b7d-7751-89ca-8a4386362f54"},
		{"too few parts", "short.jsonl", ""},
		{"non-hex", "rollout-2026-04-02T00-28-34-zzzzzzzz-0b7d-7751-89ca-8a4386362f54.jsonl", ""},
		{"wrong group lengths", "rollout-2026-04-02T00-28-34-019d4d18-0b7d-7751-89ca-8a4386362f5.jsonl", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCodexUUID(tt.in)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLocatorFor(t *testing.T) {
	if LocatorFor(IDClaude) == nil {
		t.Error("expected claude locator")
	}
	if LocatorFor(IDCodex) == nil {
		t.Error("expected codex locator")
	}
	if LocatorFor(IDAider) != nil {
		t.Error("aider should have no locator")
	}
	if LocatorFor("unknown") != nil {
		t.Error("unknown agent should have no locator")
	}
}

func TestResumeArgsWithID(t *testing.T) {
	tests := []struct {
		id     ID
		convID string
		want   []string
	}{
		{IDClaude, "abc", []string{"claude", "--resume", "abc"}},
		{IDCodex, "abc", []string{"codex", "resume", "abc"}},
		{IDClaude, "", nil},
		{IDAider, "abc", nil},
		{IDGemini, "abc", nil},
	}
	for _, tt := range tests {
		got := ResumeArgsWithID(tt.id, tt.convID)
		if !equalStrSlice(got, tt.want) {
			t.Errorf("ResumeArgsWithID(%q, %q): got %v, want %v", tt.id, tt.convID, got, tt.want)
		}
	}
}

func equalStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
