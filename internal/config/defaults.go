package config

import (
	"github.com/lucascaro/hive/internal/audio"
	"github.com/lucascaro/hive/internal/mux"
)

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() Config {
	return Config{
		SchemaVersion:               1,
		Theme:                       "dark",
		PreviewRefreshMs:            500,
		AgentTitleOverridesUserTitle: false,
		Multiplexer:                 "tmux",
		DetachKey:                   mux.DefaultDetachKey,
		BellSound:                   audio.BellNormal,
		Agents: map[string]AgentProfile{
			"claude": {
				Cmd:        []string{"claude"},
				InstallCmd: []string{"npm", "install", "-g", "@anthropic-ai/claude-code"},
				Status: StatusDetection{
					RunTitle:    `^[⠁-⠿]`,
					StableTicks: 2,
				},
			},
			"codex": {
				Cmd:        []string{"codex"},
				InstallCmd: []string{"npm", "install", "-g", "@openai/codex"},
				Status:     StatusDetection{StableTicks: 3},
			},
			"gemini": {
				Cmd:        []string{"gemini"},
				InstallCmd: []string{"npm", "install", "-g", "@google/gemini-cli"},
				Status:     StatusDetection{StableTicks: 3},
			},
			"copilot": {
				Cmd:        []string{"copilot"},
				InstallCmd: []string{"npm", "install", "-g", "@github/copilot"},
				Status:     StatusDetection{StableTicks: 3},
			},
			"aider": {
				Cmd:        []string{"aider"},
				InstallCmd: []string{"pip", "install", "aider-chat"},
				Status:     StatusDetection{StableTicks: 3},
			},
			"opencode": {
				Cmd:        []string{"opencode"},
				InstallCmd: []string{"npm", "install", "-g", "opencode"},
				Status:     StatusDetection{StableTicks: 3},
			},
		},
		TeamDefaults: TeamDefaultsConfig{
			Orchestrator: "claude",
			WorkerCount:  2,
			WorkerAgent:  "claude",
		},
		Hooks: HooksConfig{
			Enabled: true,
			Dir:     HooksPath(),
		},
		Keybindings: KeybindingsConfig{
			NewWorktreeSession: "W",
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
			Settings:       "S",
			Quit:           "q",
			QuitKill:       "Q",
			ColorNext:      "c",
			ColorPrev:      "C",
			MoveUp:         "shift+up",
			MoveDown:       "shift+down",
			MoveLeft:       "shift+left",
			MoveRight:      "shift+right",
		},
	}
}
