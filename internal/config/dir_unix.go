//go:build !windows

package config

import (
	"os"
	"path/filepath"
)

// Dir returns the hive config directory (~/.config/hive on Unix/macOS).
// Override with the HIVE_CONFIG_DIR environment variable.
func Dir() string {
	if d := os.Getenv("HIVE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "hive")
}
