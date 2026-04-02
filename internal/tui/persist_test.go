package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/state"
)

// setHomePersist overrides $HOME and HIVE_CONFIG_DIR for the duration of the
// test so config.Dir() points into the temp dir on all platforms.
func setHomePersist(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	// HIVE_CONFIG_DIR is the universal override checked first on all platforms,
	// so also set it to avoid picking up %APPDATA% on Windows.
	t.Setenv("HIVE_CONFIG_DIR", filepath.Join(dir, ".config", "hive"))
}

func ensureConfigDir(t *testing.T) {
	t.Helper()
	if err := config.Ensure(); err != nil {
		t.Fatalf("config.Ensure(): %v", err)
	}
}

func TestSaveStateLoadState_Roundtrip(t *testing.T) {
	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)

	projects := []*state.Project{
		{
			ID:       "p1",
			Name:     "my-project",
			Teams:    []*state.Team{},
			Sessions: []*state.Session{
				{
					ID:        "s1",
					ProjectID: "p1",
					Title:     "session-one",
					AgentType: state.AgentClaude,
					Status:    state.StatusRunning,
					CreatedAt: time.Now().Truncate(time.Second),
				},
			},
			CreatedAt: time.Now().Truncate(time.Second),
		},
	}
	appState := &state.AppState{Projects: projects}

	if err := saveState(appState); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	loaded, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("LoadState() returned %d projects, want 1", len(loaded))
	}
	if loaded[0].ID != "p1" {
		t.Errorf("Project ID = %q, want %q", loaded[0].ID, "p1")
	}
	if loaded[0].Name != "my-project" {
		t.Errorf("Project Name = %q, want %q", loaded[0].Name, "my-project")
	}
	if len(loaded[0].Sessions) != 1 {
		t.Fatalf("Sessions len = %d, want 1", len(loaded[0].Sessions))
	}
	if loaded[0].Sessions[0].Title != "session-one" {
		t.Errorf("Session Title = %q, want %q", loaded[0].Sessions[0].Title, "session-one")
	}
}

func TestLoadState_MissingFileReturnsNil(t *testing.T) {
	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)

	got, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState() on missing file error: %v", err)
	}
	if got != nil {
		t.Errorf("LoadState() on missing file = %v, want nil", got)
	}
}

func TestLoadState_EnsuresNonNilSlices(t *testing.T) {
	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)

	// Save a project with nil Teams/Sessions (simulates old data format).
	appState := &state.AppState{
		Projects: []*state.Project{
			{ID: "p1", Name: "proj", Teams: nil, Sessions: nil},
		},
	}
	if err := saveState(appState); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	loaded, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}
	if loaded[0].Teams == nil {
		t.Error("LoadState() should ensure non-nil Teams slice")
	}
	if loaded[0].Sessions == nil {
		t.Error("LoadState() should ensure non-nil Sessions slice")
	}
}

func TestSaveUsageLoadUsage_Roundtrip(t *testing.T) {
	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)

	usage := map[string]state.AgentUsageRecord{
		"claude": {Count: 5, LastUsed: time.Now().Truncate(time.Second)},
		"codex":  {Count: 2, LastUsed: time.Now().Truncate(time.Second)},
	}
	if err := saveUsage(usage); err != nil {
		t.Fatalf("saveUsage() error: %v", err)
	}

	loaded := LoadUsage()
	if loaded == nil {
		t.Fatal("LoadUsage() returned nil")
	}
	if loaded["claude"].Count != 5 {
		t.Errorf("claude Count = %d, want 5", loaded["claude"].Count)
	}
	if loaded["codex"].Count != 2 {
		t.Errorf("codex Count = %d, want 2", loaded["codex"].Count)
	}
}

func TestLoadUsage_MissingFileReturnsNil(t *testing.T) {
	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)

	got := LoadUsage()
	if got != nil {
		t.Errorf("LoadUsage() on missing file = %v, want nil", got)
	}
}

func TestSaveUsage_EmptyMapNoOp(t *testing.T) {
	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)

	if err := saveUsage(map[string]state.AgentUsageRecord{}); err != nil {
		t.Fatalf("saveUsage() on empty map error: %v", err)
	}
	// No file should be created for empty map
	if _, err := os.Stat(usagePath()); !os.IsNotExist(err) {
		t.Errorf("saveUsage() with empty map should not create file")
	}
}
