package config

const currentSchemaVersion = 1

// Migrate applies any needed schema migrations to cfg and returns the updated config.
func Migrate(cfg Config) Config {
	if cfg.SchemaVersion < currentSchemaVersion {
		// Future: case-by-case migration logic goes here.
		cfg.SchemaVersion = currentSchemaVersion
	}

	defaults := DefaultConfig()

	// Fill in missing install_cmd fields from defaults.
	for name, profile := range cfg.Agents {
		if len(profile.InstallCmd) == 0 {
			if def, ok := defaults.Agents[name]; ok {
				profile.InstallCmd = def.InstallCmd
				cfg.Agents[name] = profile
			}
		}
	}

	// Fill in missing keybindings from defaults.
	if cfg.Keybindings.GridOverview == "" {
		cfg.Keybindings.GridOverview = defaults.Keybindings.GridOverview
	}

	return cfg
}
