package config

// Config is the user-facing configuration loaded from ~/.config/hive/config.json.
type Config struct {
	SchemaVersion               int                       `json:"schema_version"`
	Theme                       string                    `json:"theme"`
	PreviewRefreshMs            int                       `json:"preview_refresh_ms"`
	AgentTitleOverridesUserTitle bool                     `json:"agent_title_overrides_user_title"`
	HideAttachHint              bool                      `json:"hide_attach_hint,omitempty"`
	Agents                      map[string]AgentProfile   `json:"agents"`
	TeamDefaults                TeamDefaultsConfig        `json:"team_defaults"`
	Hooks                       HooksConfig               `json:"hooks"`
	Keybindings                 KeybindingsConfig         `json:"keybindings"`
}

// AgentProfile defines how to launch a specific agent type.
type AgentProfile struct {
	Cmd        []string `json:"cmd"`
	InstallCmd []string `json:"install_cmd,omitempty"`
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
	Quit           string `json:"quit"`
	QuitKill       string `json:"quit_kill"`
}
