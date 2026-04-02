//go:build windows

package config

import (
	"os"
	"path/filepath"
)

// Dir returns the hive config directory (%APPDATA%\hive on Windows).
// Override with the HIVE_CONFIG_DIR environment variable.
func Dir() string {
	if d := os.Getenv("HIVE_CONFIG_DIR"); d != "" {
		return d
	}
	base, err := os.UserConfigDir() // returns %AppData% on Windows
	if err != nil {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(base, "hive")
}
