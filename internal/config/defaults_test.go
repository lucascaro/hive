package config

import "testing"

func TestDefaultConfig_FieldsPopulated(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", cfg.SchemaVersion)
	}
	if cfg.Theme == "" {
		t.Error("Theme should not be empty")
	}
	if cfg.PreviewRefreshMs <= 0 {
		t.Errorf("PreviewRefreshMs = %d, want > 0", cfg.PreviewRefreshMs)
	}
	if cfg.Multiplexer == "" {
		t.Error("Multiplexer should not be empty")
	}
}

func TestDefaultConfig_AgentsPopulated(t *testing.T) {
	cfg := DefaultConfig()

	expectedAgents := []string{"claude", "codex", "gemini", "copilot", "aider", "opencode"}
	for _, name := range expectedAgents {
		profile, ok := cfg.Agents[name]
		if !ok {
			t.Errorf("Agents[%q] missing", name)
			continue
		}
		if len(profile.Cmd) == 0 {
			t.Errorf("Agents[%q].Cmd is empty", name)
		}
		if len(profile.InstallCmd) == 0 {
			t.Errorf("Agents[%q].InstallCmd is empty", name)
		}
	}
}

func TestDefaultConfig_TeamDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.TeamDefaults.Orchestrator == "" {
		t.Error("TeamDefaults.Orchestrator should not be empty")
	}
	if cfg.TeamDefaults.WorkerCount <= 0 {
		t.Errorf("TeamDefaults.WorkerCount = %d, want > 0", cfg.TeamDefaults.WorkerCount)
	}
	if cfg.TeamDefaults.WorkerAgent == "" {
		t.Error("TeamDefaults.WorkerAgent should not be empty")
	}
}

func TestDefaultConfig_KeybindingsPopulated(t *testing.T) {
	cfg := DefaultConfig()
	kb := cfg.Keybindings

	bindings := map[string]KeyBinding{
		"NewProject":     kb.NewProject,
		"NewSession":     kb.NewSession,
		"NewTeam":        kb.NewTeam,
		"KillSession":    kb.KillSession,
		"Rename":         kb.Rename,
		"Attach":         kb.Attach,
		"ToggleCollapse": kb.ToggleCollapse,
		"NavUp":          kb.NavUp,
		"NavDown":        kb.NavDown,
		"CursorUp":       kb.CursorUp,
		"CursorDown":     kb.CursorDown,
		"Detach":         kb.Detach,
		"InputMode":      kb.InputMode,
		"Quit":           kb.Quit,
		"QuitKill":       kb.QuitKill,
		"Filter":         kb.Filter,
		"GridOverview":   kb.GridOverview,
		"Help":           kb.Help,
		"ColorNext":      kb.ColorNext,
		"ColorPrev":      kb.ColorPrev,
	}
	for name, val := range bindings {
		if val.First() == "" {
			t.Errorf("Keybindings.%s is empty", name)
		}
	}
}

func TestDefaultConfig_HooksEnabled(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Hooks.Enabled {
		t.Error("Hooks.Enabled should be true by default")
	}
	if cfg.Hooks.Dir == "" {
		t.Error("Hooks.Dir should not be empty")
	}
}
