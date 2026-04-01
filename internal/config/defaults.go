package config

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() Config {
	return Config{
		SchemaVersion:               1,
		Theme:                       "dark",
		PreviewRefreshMs:            500,
		AgentTitleOverridesUserTitle: false,
		Multiplexer:                 "tmux",
		Agents: map[string]AgentProfile{
			"claude":   {Cmd: []string{"claude"}, InstallCmd: []string{"npm", "install", "-g", "@anthropic-ai/claude-code"}},
			"codex":    {Cmd: []string{"codex"}, InstallCmd: []string{"npm", "install", "-g", "@openai/codex"}},
			"gemini":   {Cmd: []string{"gemini"}, InstallCmd: []string{"npm", "install", "-g", "@google/gemini-cli"}},
			"copilot":  {Cmd: []string{"copilot"}, InstallCmd: []string{"npm", "install", "-g", "@github/copilot"}},
			"aider":    {Cmd: []string{"aider"}, InstallCmd: []string{"pip", "install", "aider-chat"}},
			"opencode": {Cmd: []string{"opencode"}, InstallCmd: []string{"npm", "install", "-g", "opencode"}},
		},
		TeamDefaults: TeamDefaultsConfig{
			Orchestrator: "claude",
			WorkerCount:  2,
			WorkerAgent:  "claude",
		},
		Hooks: HooksConfig{
			Enabled: true,
			Dir:     "~/.config/hive/hooks",
		},
		Keybindings: KeybindingsConfig{
			NewProject:     "n",
			NewSession:     "t",
			NewTeam:        "T",
			KillSession:    "x",
			KillTeam:       "D",
			Rename:         "r",
			Attach:         "a",
			ToggleCollapse: " ",
			FocusPreview:   "tab",
			FocusSidebar:   "tab",
			NavUp:          "k",
			NavDown:        "j",
			NavProjectUp:   "K",
			NavProjectDown: "J",
			Filter:         "/",
			GridOverview:   "g",
			Palette:        "ctrl+p",
			Help:           "?",
			TmuxHelp:       "H",
			Quit:           "q",
			QuitKill:       "Q",
		},
	}
}
