package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestSaveLoadState_CustomDir_DoesNotTouchRealConfig(t *testing.T) {
	realStatePath := config.StatePath()

	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)

	// Save state to custom dir.
	appState := &state.AppState{
		Projects: []*state.Project{
			{ID: "p1", Name: "demo", Color: "#FF0000", Teams: []*state.Team{}, Sessions: []*state.Session{}},
		},
	}
	if _, err := saveState(appState); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	// State file should exist in custom dir.
	customState := config.StatePath()
	if _, err := os.Stat(customState); err != nil {
		t.Fatalf("state.json not in custom dir: %v", err)
	}

	// Custom state path should differ from real.
	if customState == realStatePath {
		t.Error("StatePath() still points to real config dir under HIVE_CONFIG_DIR override")
	}

	// Load back and verify isolation.
	loaded, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}
	if len(loaded) != 1 || loaded[0].Name != "demo" {
		t.Errorf("LoadState() from custom dir returned unexpected data: %v", loaded)
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

	if _, err := saveState(appState); err != nil {
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
	if _, err := saveState(appState); err != nil {
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

func TestSaveState_ConcurrentWritesNoCorruption(t *testing.T) {
	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)

	const goroutines = 10
	const writesPerGoroutine = 20

	var wg sync.WaitGroup
	errs := make(chan error, goroutines*writesPerGoroutine)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < writesPerGoroutine; i++ {
				appState := &state.AppState{
					Projects: []*state.Project{
						{
							ID:       fmt.Sprintf("p-%d-%d", id, i),
							Name:     fmt.Sprintf("project-%d-%d", id, i),
							Teams:    []*state.Team{},
							Sessions: []*state.Session{},
						},
					},
				}
				if _, err := saveState(appState); err != nil {
					errs <- fmt.Errorf("goroutine %d write %d: %w", id, i, err)
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	// The file must be valid JSON after all concurrent writes.
	loaded, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState() after concurrent writes: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("LoadState() returned %d projects, want 1 (last writer wins)", len(loaded))
	}
}

func TestSaveState_NilProjectsWritesEmptyArray(t *testing.T) {
	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)

	appState := &state.AppState{Projects: nil}
	if _, err := saveState(appState); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	data, err := os.ReadFile(config.StatePath())
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	got := strings.TrimSpace(string(data))
	if got != "[]" {
		t.Errorf("state file content = %q, want %q", got, "[]")
	}
}

func TestMigrateProjectColors_AssignsDistinctColors(t *testing.T) {
	projects := []*state.Project{
		{ID: "p1", Name: "a", Color: "#7C3AED"},
		{ID: "p2", Name: "b", Color: "#7C3AED"},
		{ID: "p3", Name: "c", Color: ""},
	}
	migrateProjectColors(projects)

	seen := make(map[string]bool)
	for _, p := range projects {
		if p.Color == "" {
			t.Errorf("project %s has empty color after migration", p.ID)
		}
		if p.Color == "#7C3AED" && p.ID != "p1" {
			// p1 gets palette[0] which happens to be #7C3AED, but p2/p3 should differ.
		}
		seen[p.Color] = true
	}
	if len(seen) < 3 {
		t.Errorf("expected 3 distinct colors, got %d: %v", len(seen), seen)
	}
}

func TestMigrateProjectColors_PreservesCustomColors(t *testing.T) {
	projects := []*state.Project{
		{ID: "p1", Name: "a", Color: "#CUSTOM1"},
		{ID: "p2", Name: "b", Color: "#7C3AED"}, // default — should be migrated
	}
	migrateProjectColors(projects)

	if projects[0].Color != "#CUSTOM1" {
		t.Errorf("custom color was changed: got %q, want #CUSTOM1", projects[0].Color)
	}
	if projects[1].Color == "#7C3AED" {
		t.Error("default color should have been migrated")
	}
}

func TestMigrateProjectColors_EmptySlice(t *testing.T) {
	// Should not panic on empty input.
	migrateProjectColors(nil)
	migrateProjectColors([]*state.Project{})
}

func TestLoadState_MigratesProjectColors(t *testing.T) {
	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)

	// Write state with two projects that have the old default color.
	appState := &state.AppState{
		Projects: []*state.Project{
			{ID: "p1", Name: "a", Color: "#7C3AED", Teams: []*state.Team{}, Sessions: []*state.Session{}},
			{ID: "p2", Name: "b", Color: "#7C3AED", Teams: []*state.Team{}, Sessions: []*state.Session{}},
		},
	}
	if _, err := saveState(appState); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	loaded, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState() error: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("LoadState() returned %d projects, want 2", len(loaded))
	}
	if loaded[0].Color == loaded[1].Color {
		t.Errorf("LoadState() should migrate to distinct colors, both are %q", loaded[0].Color)
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
