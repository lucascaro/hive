package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	configFileName = "config.json"
	stateFileName  = "state.json"
	logFileName    = "hive.log"
	hooksDir       = "hooks"
)

// ConfigPath returns the full path to config.json.
func ConfigPath() string { return filepath.Join(Dir(), configFileName) }

// StatePath returns the full path to state.json.
func StatePath() string { return filepath.Join(Dir(), stateFileName) }

// LogPath returns the full path to hive.log.
func LogPath() string { return filepath.Join(Dir(), logFileName) }

// HooksPath returns the hooks directory path.
func HooksPath() string { return filepath.Join(Dir(), hooksDir) }

// Ensure creates the config directory and all subdirectories if they don't exist,
// then tightens permissions on any sensitive files that may have been created with
// overly-broad modes by older versions.
func Ensure() error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(HooksPath(), 0o755); err != nil {
		return err
	}
	return FixPermissions()
}

// FixPermissions chmods sensitive config files to 0o600 (owner read/write only).
// It is idempotent and skips files that do not yet exist.
func FixPermissions() error {
	sensitive := []string{
		StatePath(),
		filepath.Join(Dir(), "usage.json"),
		LogPath(),
	}
	for _, path := range sensitive {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("fix permissions on %s: %w", path, err)
		}
	}
	return nil
}

// Load reads the config file, returning defaults if it doesn't exist.
func Load() (Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(ConfigPath())
	if os.IsNotExist(err) {
		// Write defaults on first run.
		if werr := Save(cfg); werr != nil {
			return cfg, werr
		}
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Save writes the config atomically.
func Save(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(ConfigPath(), data)
}

// writeAtomic writes data to path via a temp file + rename.
func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
