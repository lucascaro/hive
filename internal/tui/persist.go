package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/styles"
)

func usagePath() string     { return filepath.Join(config.Dir(), "usage.json") }
func stateLockPath() string { return config.StatePath() + ".lock" }

// saveUsage writes agent usage stats to usage.json.
func saveUsage(usage map[string]state.AgentUsageRecord) error {
	if len(usage) == 0 {
		return nil
	}
	data, err := json.MarshalIndent(usage, "", "  ")
	if err != nil {
		return err
	}
	tmp := usagePath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, usagePath())
}

// LoadUsage reads agent usage stats from usage.json.
func LoadUsage() map[string]state.AgentUsageRecord {
	data, err := os.ReadFile(usagePath())
	if err != nil {
		return nil
	}
	var usage map[string]state.AgentUsageRecord
	_ = json.Unmarshal(data, &usage)
	return usage
}

// saveState writes the current state to disk atomically and under an
// exclusive advisory lock so concurrent hive instances do not corrupt each
// other's writes.  It returns the modification time of the written file so
// the caller can update its mtime baseline without a second stat call.
func saveState(appState *state.AppState) (time.Time, error) {
	lf, err := os.OpenFile(stateLockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return time.Time{}, fmt.Errorf("open state lock: %w", err)
	}
	defer lf.Close()

	if err := lockExclusive(lf); err != nil {
		return time.Time{}, fmt.Errorf("acquire state lock: %w", err)
	}
	defer func() {
		if err := unlockFile(lf); err != nil {
			debugLog.Printf("saveState: release lock: %v", err)
		}
	}()

	projects := appState.Projects
	if projects == nil {
		projects = []*state.Project{}
	}
	data, err := json.MarshalIndent(projects, "", "  ")
	if err != nil {
		return time.Time{}, err
	}
	path := config.StatePath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return time.Time{}, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return time.Time{}, err
	}
	// Stat the file we just wrote so the caller has a precise mtime baseline
	// without needing a second syscall outside this function.
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, fmt.Errorf("stat after write: %w", err)
	}
	return info.ModTime(), nil
}

// LoadState reads saved projects from state.json.
func LoadState() ([]*state.Project, error) {
	data, err := os.ReadFile(config.StatePath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var projects []*state.Project
	if err := json.Unmarshal(data, &projects); err != nil {
		return nil, err
	}
	// Migrate projects with default/empty color to distinct palette colors.
	migrateProjectColors(projects)

	// Ensure non-nil slices.
	for _, p := range projects {
		if p.Teams == nil {
			p.Teams = []*state.Team{}
		}
		if p.Sessions == nil {
			p.Sessions = []*state.Session{}
		}
		for _, t := range p.Teams {
			if t.Sessions == nil {
				t.Sessions = []*state.Session{}
			}
		}
	}
	return projects, nil
}

// migrateProjectColors assigns distinct palette colors to projects that still
// have the old default "#7C3AED" or an empty color.
func migrateProjectColors(projects []*state.Project) {
	const oldDefault = "#7C3AED"
	for i, p := range projects {
		if p.Color == "" || p.Color == oldDefault {
			p.Color = styles.NextProjectColor(i)
		}
	}
}
