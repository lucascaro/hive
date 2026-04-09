package config

import "github.com/lucascaro/hive/internal/mux"

const currentSchemaVersion = 1

// Migrate applies any needed schema migrations to cfg and returns the updated config.
func Migrate(cfg Config) Config {
	if cfg.SchemaVersion < currentSchemaVersion {
		// Future: case-by-case migration logic goes here.
		cfg.SchemaVersion = currentSchemaVersion
	}

	defaults := DefaultConfig()

	// Fill in missing install_cmd and status detection fields from defaults.
	for name, profile := range cfg.Agents {
		changed := false
		if len(profile.InstallCmd) == 0 {
			if def, ok := defaults.Agents[name]; ok {
				profile.InstallCmd = def.InstallCmd
				changed = true
			}
		}
		if profile.Status == (StatusDetection{}) {
			if def, ok := defaults.Agents[name]; ok {
				profile.Status = def.Status
				changed = true
			}
		}
		if changed {
			cfg.Agents[name] = profile
		}
	}

	// Fill in missing keybindings from defaults.
	if cfg.Keybindings.GridOverview == "" {
		cfg.Keybindings.GridOverview = defaults.Keybindings.GridOverview
	}
	if cfg.Keybindings.ColorNext == "" {
		cfg.Keybindings.ColorNext = defaults.Keybindings.ColorNext
	}
	if cfg.Keybindings.ColorPrev == "" {
		cfg.Keybindings.ColorPrev = defaults.Keybindings.ColorPrev
	}

	// Default the detach key when missing. Invalid values are reported and
	// fall back to the default in cmd/start.go's initMuxBackend so the user
	// sees a clear stderr warning at startup; we deliberately don't silently
	// rewrite the value here.
	if cfg.DetachKey == "" {
		cfg.DetachKey = mux.DefaultDetachKey
	}

	return cfg
}
