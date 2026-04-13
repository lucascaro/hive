package config

import "github.com/lucascaro/hive/internal/mux"

const currentSchemaVersion = 4

// Migrate applies any needed schema migrations to cfg and returns the updated config.
func Migrate(cfg Config) Config {
	if cfg.SchemaVersion < 2 {
		// 1 → 2: the detach shortcut changed from tmux's two-step `Ctrl+B D`
		// to a single-key combo (default `Ctrl+Q`, see #41). Re-show the
		// pre-attach splash so existing users discover the new shortcut.
		// This only fires once per user — the bumped SchemaVersion is
		// persisted by MigrateAndPersist after this call.
		cfg.HideAttachHint = false
	}
	if cfg.SchemaVersion < 3 {
		// 2 → 3: BellSound was introduced (#75). Fill in the default so
		// existing users keep today's audible `\a` behavior until they
		// opt into a custom sound via Settings.
		if cfg.BellSound == "" {
			cfg.BellSound = DefaultConfig().BellSound
		}
	}
	if cfg.SchemaVersion < 4 {
		// 3 → 4: StartupView introduced (#78). Fill in the explicit default
		// so existing users keep the sidebar-first behavior they already have.
		if cfg.StartupView == "" {
			cfg.StartupView = DefaultConfig().StartupView
		}
	}
	if cfg.SchemaVersion < currentSchemaVersion {
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
	if cfg.Keybindings.SidebarView == "" {
		cfg.Keybindings.SidebarView = defaults.Keybindings.SidebarView
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

// MigrateAndPersist runs Migrate and writes the result back to disk if a
// schema upgrade was applied. This is the right entry point for interactive
// commands (like `hive start`) that want one-shot migration side effects —
// e.g. resetting `hide_attach_hint` after the detach key changed in v2 — to
// be remembered across runs.
//
// Non-interactive one-shot commands (like `hive attach`) should call Migrate
// directly so they don't write to the user's config file as a side effect.
func MigrateAndPersist(cfg Config) (Config, error) {
	needsSave := cfg.SchemaVersion < currentSchemaVersion
	cfg = Migrate(cfg)
	if needsSave {
		if err := Save(cfg); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}
