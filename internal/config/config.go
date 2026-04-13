package config

// Config is the user-facing configuration loaded from ~/.config/hive/config.json.
type Config struct {
	SchemaVersion               int                       `json:"schema_version"`
	Theme                       string                    `json:"theme"`
	PreviewRefreshMs            int                       `json:"preview_refresh_ms"`
	AgentTitleOverridesUserTitle bool                     `json:"agent_title_overrides_user_title"`
	HideAttachHint              bool                      `json:"hide_attach_hint,omitempty"`
	HideWhatsNew                bool                      `json:"hide_whats_new,omitempty"`
	LastSeenVersion             string                    `json:"last_seen_version,omitempty"`
	// StartupView selects the initial view shown on startup.
	// "sidebar" (default) shows the normal session list.
	// "grid" opens the current-project grid.
	// "grid-all" opens the all-projects grid.
	StartupView                 string                    `json:"startup_view,omitempty"`
	// BellSound selects the audio played when a background session rings
	// its terminal bell. See internal/audio for the accepted identifiers
	// (normal, bee, chime, ping, knock, silent).
	BellSound                   string                    `json:"bell_sound,omitempty"`
	// BellVolume controls playback loudness as a percentage (1–100).
	// A value of 0 is treated as 100 (full volume) for backwards compatibility
	// with configs written before this field existed.
	BellVolume                  int                       `json:"bell_volume,omitempty"`
	// Multiplexer selects the backend for managing terminal sessions.
	// "tmux" (default) uses the external tmux binary.
	// "native" uses Go's built-in PTY daemon; no external binary needed.
	Multiplexer                 string                    `json:"multiplexer,omitempty"`
	// DetachKey is the single-key combination that returns the user from an
	// attached session back to the Hive TUI. Accepted form is
	// "ctrl+<lowercase-letter>" (e.g. "ctrl+q", "ctrl+d"). Defaults to
	// "ctrl+q". The key is enforced by the active multiplexer backend
	// (tmux installs a server-side bind-key; the native backend intercepts
	// the matching control byte on stdin), so it is not part of the
	// in-TUI Keybindings struct.
	DetachKey                   string                    `json:"detach_key,omitempty"`
	Agents                      map[string]AgentProfile   `json:"agents"`
	TeamDefaults                TeamDefaultsConfig        `json:"team_defaults"`
	Hooks                       HooksConfig               `json:"hooks"`
	Keybindings                 KeybindingsConfig         `json:"keybindings"`
}

// AgentProfile defines how to launch a specific agent type.
type AgentProfile struct {
	Cmd        []string        `json:"cmd"`
	InstallCmd []string        `json:"install_cmd,omitempty"`
	Status     StatusDetection `json:"status,omitempty"`
}

// StatusDetection configures how hive detects whether a session is running,
// waiting for input, or idle. Regex patterns are matched against pane titles
// or last-line content.
type StatusDetection struct {
	WaitTitle   string `json:"wait_title,omitempty"`   // regex on pane title → waiting
	RunTitle    string `json:"run_title,omitempty"`     // regex on pane title → running
	WaitPrompt  string `json:"wait_prompt,omitempty"`  // regex on last non-empty line → waiting
	IdlePrompt  string `json:"idle_prompt,omitempty"`  // regex on last non-empty line → idle (if configured but not matched → waiting)
	StableTicks int    `json:"stable_ticks,omitempty"` // polls before running→idle/waiting (default 2)
}

// TeamDefaultsConfig specifies defaults when creating a new team.
type TeamDefaultsConfig struct {
	Orchestrator string `json:"orchestrator"`
	WorkerCount  int    `json:"worker_count"`
	WorkerAgent  string `json:"worker_agent"`
}

// HooksConfig controls the hook system.
type HooksConfig struct {
	Enabled bool   `json:"enabled"`
	Dir     string `json:"dir"`
}

// KeybindingsConfig maps action names to key strings.
type KeybindingsConfig struct {
	NewWorktreeSession string `json:"new_worktree_session"`
	NewProject     string `json:"new_project"`
	NewSession     string `json:"new_session"`
	NewTeam        string `json:"new_team"`
	KillSession    string `json:"kill_session"`
	KillTeam       string `json:"kill_team"`
	Rename         string `json:"rename"`
	Attach         string `json:"attach"`
	ToggleCollapse string `json:"toggle_collapse"`
	FocusPreview   string `json:"focus_preview"`
	FocusSidebar   string `json:"focus_sidebar"`
	NavUp          string `json:"nav_up"`
	NavDown        string `json:"nav_down"`
	NavProjectUp   string `json:"nav_project_up"`
	NavProjectDown string `json:"nav_project_down"`
	JumpProject1   string `json:"jump_project_1"`
	Filter         string `json:"filter"`
	GridOverview   string `json:"grid_overview"`
	Palette        string `json:"palette"`
	Help           string `json:"help"`
	TmuxHelp       string `json:"tmux_help"`
	Settings       string `json:"settings"`
	Quit           string `json:"quit"`
	QuitKill       string `json:"quit_kill"`
	ColorNext      string `json:"color_next"`
	ColorPrev      string `json:"color_prev"`
	MoveUp         string `json:"move_up"`
	MoveDown       string `json:"move_down"`
	MoveLeft       string `json:"move_left"`
	MoveRight      string `json:"move_right"`
}
