package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/lucascaro/hive/internal/registry"
)

// windowGeometry is the on-disk shape of <stateDir>/window.json.
// Multi-window note: every GUI process reads the same file at startup
// (all opened windows land at the same position) and last-to-close
// wins on save. That's the simplest behavior consistent with users'
// "remember where I left it" expectation; finer-grained per-window
// memory would need a window identity story we don't have yet.
type windowGeometry struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

func windowStatePath() string {
	return filepath.Join(registry.StateDir(), "window.json")
}

func loadWindowGeometry() (*windowGeometry, bool) {
	b, err := os.ReadFile(windowStatePath())
	if err != nil {
		return nil, false
	}
	var g windowGeometry
	if err := json.Unmarshal(b, &g); err != nil {
		return nil, false
	}
	// Don't trust nonsense values from a corrupt file.
	if g.W < 320 || g.H < 240 {
		return nil, false
	}
	return &g, true
}

func saveWindowGeometry(g windowGeometry) error {
	path := windowStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(g)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
