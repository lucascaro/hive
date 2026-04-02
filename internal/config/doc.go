// Package config manages Hive's user configuration.
//
// Configuration is stored as JSON at ~/.config/hive/config.json (or
// $HIVE_CONFIG_DIR/config.json). Writes are atomic: data is written to a
// temporary file and then renamed over the target to prevent corruption.
//
// # Key types
//
//   - [Config]           — top-level configuration struct
//   - [AgentProfile]     — launch command + optional install command per agent
//   - [KeybindingsConfig] — maps action names to key strings (loaded into tui.KeyMap)
//   - [TeamDefaultsConfig] — defaults for the team-creation wizard
//   - [HooksConfig]      — hooks directory path and enabled flag
//
// # Key functions
//
//   - [Load]   — reads config.json; writes defaults on first run; applies migrations
//   - [Save]   — atomic write to config.json
//   - [Dir]    — returns the config directory path
//   - [Ensure] — creates the config directory tree on first launch
//
// # Schema migrations
//
// migrate.go handles forward-only schema version bumps. Each migration function
// transforms the raw JSON map before unmarshaling into [Config].
package config
