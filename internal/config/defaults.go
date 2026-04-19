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
		StartupView:                 "sidebar",
		Agents: map[string]AgentProfile{
			"claude": {
				Cmd:        []string{"claude"},
				InstallCmd: []string{"npm", "install", "-g", "@anthropic-ai/claude-code"},
				Status: StatusDetection{
					RunTitle:    `^[⠁-⠿]`,
					WaitPrompt:  `^\s*[1-9]\.\s`,
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
		Keybindings: defaultKeybindings(),
	}
}

// defaultKeybindings returns the default key bindings. Each action is a slice
// so users can bind multiple keys; defaults express vim-style aliases this way
// (e.g. CursorUp = ["up", "k"]).
func defaultKeybindings() KeybindingsConfig {
	return KeybindingsConfig{
		NewWorktreeSession: KeyBinding{"W"},
		NewProject:         KeyBinding{"n"},
		NewSession:         KeyBinding{"t"},
		NewTeam:            KeyBinding{"T"},
		KillSession:        KeyBinding{"x"},
		KillTeam:           KeyBinding{"D"},
		Rename:             KeyBinding{"r"},
		Attach:             KeyBinding{"enter"},
		ToggleCollapse:     KeyBinding{" "},
		CollapseItem:       KeyBinding{"left", "h"},
		ExpandItem:         KeyBinding{"right", "l"},
		NavUp:              KeyBinding{"up"},
		NavDown:            KeyBinding{"down"},
		NavProjectUp:       KeyBinding{"K"},
		NavProjectDown:     KeyBinding{"J"},
		CursorUp:           KeyBinding{"up", "k"},
		CursorDown:         KeyBinding{"down", "j"},
		CursorLeft:         KeyBinding{"left", "h"},
		CursorRight:        KeyBinding{"right", "l"},
		JumpToProject:      KeyBinding{"1", "2", "3", "4", "5", "6", "7", "8", "9"},
		Filter:             KeyBinding{"/"},
		SidebarView:        KeyBinding{"s"},
		GridOverview:       KeyBinding{"g"},
		Palette:            KeyBinding{"ctrl+p"},
		Help:               KeyBinding{"?"},
		TmuxHelp:           KeyBinding{"H"},
		Settings:           KeyBinding{"S"},
		Quit:               KeyBinding{"q"},
		QuitKill:           KeyBinding{"Q"},
		ColorNext:          KeyBinding{"c"},
		ColorPrev:          KeyBinding{"C"},
		SessionColorNext:   KeyBinding{"v"},
		SessionColorPrev:   KeyBinding{"V"},
		// Capital-letter aliases (H/J/K/L) for Move* are deliberately deferred
		// to chunk 2 of #112 — adding them here would silently change which
		// action wins for those keys depending on handler case ordering, which
		// belongs in the same change as the handler refactor.
		MoveUp:             KeyBinding{"shift+up"},
		MoveDown:           KeyBinding{"shift+down"},
		MoveLeft:           KeyBinding{"shift+left"},
		MoveRight:          KeyBinding{"shift+right"},
		InputMode:          KeyBinding{"i"},
		Detach:             KeyBinding{"ctrl+q"},
		ToggleAll:          KeyBinding{"G"},
	}
}
