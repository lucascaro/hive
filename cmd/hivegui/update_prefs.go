package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lucascaro/hive/internal/registry"
)

// Update channels. "release" tracks tagged GitHub releases; "latest"
// tracks the tip of the source checkout's upstream branch.
const (
	ChannelRelease = "release"
	ChannelLatest  = "latest"
)

// UpdateSettings is the on-disk shape of <stateDir>/update.json and the
// payload of the GetUpdateSettings/SaveUpdateSettings bindings.
//
// It lives in a GUI-owned file rather than the registry: the registry
// is the daemon's state, and a GUI preference routed through the wire
// protocol would have to be added to all three wire clients for no
// benefit. window.json (window_state.go) is the same shape of thing.
type UpdateSettings struct {
	Channel string `json:"channel"`
	// SourceRepo overrides the auto-detected hive checkout for the
	// "latest" channel. Empty means "auto-detect"; see source_repo.go.
	SourceRepo string `json:"source_repo"`
}

func updateSettingsPath() string {
	return filepath.Join(registry.StateDir(), "update.json")
}

// normalizeChannel maps anything unrecognised — including the empty
// string from a fresh install or a hand-edited typo — onto the release
// channel. The latest channel shells out to git and build.sh, so it is
// never the value we guess into.
func normalizeChannel(c string) string {
	if c == ChannelLatest {
		return ChannelLatest
	}
	return ChannelRelease
}

// loadUpdateSettings reads update.json. A missing file is the default
// settings, not an error. A file that exists but will not parse *is* an
// error: silently defaulting would let a save overwrite a config the
// user hand-edited and mistyped, which is how agents.json corruption
// used to eat custom agents (see ListCustomAgents).
func loadUpdateSettings() (UpdateSettings, error) {
	b, err := os.ReadFile(updateSettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return UpdateSettings{Channel: ChannelRelease}, nil
		}
		return UpdateSettings{Channel: ChannelRelease}, fmt.Errorf("read update.json: %w", err)
	}
	var s UpdateSettings
	if err := json.Unmarshal(b, &s); err != nil {
		return UpdateSettings{Channel: ChannelRelease}, fmt.Errorf("parse %s: %w", updateSettingsPath(), err)
	}
	s.Channel = normalizeChannel(s.Channel)
	return s, nil
}

// saveUpdateSettings writes update.json atomically (temp + rename), the
// same way the registry and window.json do, so a crash mid-write can
// never leave a half-written file that the next load refuses to parse.
func saveUpdateSettings(s UpdateSettings) error {
	s.Channel = normalizeChannel(s.Channel)
	dir := registry.StateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(dir, "update-*.json")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename below succeeds
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, updateSettingsPath()); err != nil {
		return fmt.Errorf("rename update.json: %w", err)
	}
	return nil
}

// GetUpdateSettings is the Wails binding behind the Settings modal's
// update section.
func (a *App) GetUpdateSettings() (UpdateSettings, error) {
	return loadUpdateSettings()
}

// SaveUpdateSettings persists the channel and source-repo override.
//
// A latest-channel selection is refused unless a source repo actually
// resolves, so the setting can never be saved into a state where the
// update check has nothing to check against. The error text names the
// reason; the modal shows it verbatim.
func (a *App) SaveUpdateSettings(s UpdateSettings) error {
	s.Channel = normalizeChannel(s.Channel)
	if s.Channel == ChannelLatest {
		if _, err := resolveSourceRepo(s.SourceRepo); err != nil {
			return err
		}
	}
	return saveUpdateSettings(s)
}
