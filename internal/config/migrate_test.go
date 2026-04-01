package config

import "testing"

func TestMigrate_BumpsSchemaVersion(t *testing.T) {
	cfg := Config{SchemaVersion: 0}
	got := Migrate(cfg)
	if got.SchemaVersion != currentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, currentSchemaVersion)
	}
}

func TestMigrate_CurrentVersionUnchanged(t *testing.T) {
	cfg := Config{SchemaVersion: currentSchemaVersion}
	got := Migrate(cfg)
	if got.SchemaVersion != currentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, currentSchemaVersion)
	}
}

func TestMigrate_FillsMissingInstallCmd(t *testing.T) {
	cfg := Config{
		SchemaVersion: currentSchemaVersion,
		Agents: map[string]AgentProfile{
			"claude": {Cmd: []string{"claude"}}, // no InstallCmd
		},
	}
	got := Migrate(cfg)
	profile, ok := got.Agents["claude"]
	if !ok {
		t.Fatal("claude agent missing after migrate")
	}
	if len(profile.InstallCmd) == 0 {
		t.Error("Migrate should fill missing InstallCmd from defaults")
	}
}

func TestMigrate_PreservesExistingInstallCmd(t *testing.T) {
	custom := []string{"my", "custom", "install"}
	cfg := Config{
		SchemaVersion: currentSchemaVersion,
		Agents: map[string]AgentProfile{
			"claude": {Cmd: []string{"claude"}, InstallCmd: custom},
		},
	}
	got := Migrate(cfg)
	profile := got.Agents["claude"]
	if len(profile.InstallCmd) != len(custom) {
		t.Errorf("InstallCmd = %v, want %v", profile.InstallCmd, custom)
	}
	for i, v := range custom {
		if profile.InstallCmd[i] != v {
			t.Errorf("InstallCmd[%d] = %q, want %q", i, profile.InstallCmd[i], v)
		}
	}
}

func TestMigrate_FillsMissingGridOverview(t *testing.T) {
	cfg := Config{
		SchemaVersion: currentSchemaVersion,
		Keybindings:   KeybindingsConfig{GridOverview: ""},
	}
	got := Migrate(cfg)
	if got.Keybindings.GridOverview == "" {
		t.Error("Migrate should fill missing GridOverview keybinding from defaults")
	}
}

func TestMigrate_PreservesExistingGridOverview(t *testing.T) {
	cfg := Config{
		SchemaVersion: currentSchemaVersion,
		Keybindings:   KeybindingsConfig{GridOverview: "G"},
	}
	got := Migrate(cfg)
	if got.Keybindings.GridOverview != "G" {
		t.Errorf("GridOverview = %q, want %q", got.Keybindings.GridOverview, "G")
	}
}

func TestMigrate_UnknownAgentInstallCmdNotFilled(t *testing.T) {
	cfg := Config{
		SchemaVersion: currentSchemaVersion,
		Agents: map[string]AgentProfile{
			"myagent": {Cmd: []string{"myagent"}}, // not in defaults
		},
	}
	got := Migrate(cfg)
	profile := got.Agents["myagent"]
	// No default exists for "myagent", so InstallCmd should remain empty.
	if len(profile.InstallCmd) != 0 {
		t.Errorf("InstallCmd for unknown agent should stay empty, got %v", profile.InstallCmd)
	}
}
