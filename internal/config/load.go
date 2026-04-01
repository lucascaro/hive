package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	configFileName = "config.json"
	stateFileName  = "state.json"
	logFileName    = "hive.log"
	hooksDir       = "hooks"
)

// Dir returns the hive config directory, expanding ~ if needed.
func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "hive")
}

// ConfigPath returns the full path to config.json.
func ConfigPath() string { return filepath.Join(Dir(), configFileName) }

// StatePath returns the full path to state.json.
func StatePath() string { return filepath.Join(Dir(), stateFileName) }

// LogPath returns the full path to hive.log.
func LogPath() string { return filepath.Join(Dir(), logFileName) }

// HooksPath returns the hooks directory path.
func HooksPath() string { return filepath.Join(Dir(), hooksDir) }

// Ensure creates the config directory and all subdirectories if they don't exist.
func Ensure() error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	return os.MkdirAll(HooksPath(), 0o755)
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
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
