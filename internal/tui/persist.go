package tui

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/state"
)

func usagePath() string { return filepath.Join(config.Dir(), "usage.json") }

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

// saveState writes the current state to disk atomically.
func saveState(appState *state.AppState) error {
	data, err := json.MarshalIndent(appState.Projects, "", "  ")
	if err != nil {
		return err
	}
	path := config.StatePath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
