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
		Keybindings:   KeybindingsConfig{GridOverview: nil},
	}
	got := Migrate(cfg)
	if got.Keybindings.GridOverview.First() == "" {
		t.Error("Migrate should fill missing GridOverview keybinding from defaults")
	}
}

func TestMigrate_PreservesExistingGridOverview(t *testing.T) {
	cfg := Config{
		SchemaVersion: currentSchemaVersion,
		Keybindings:   KeybindingsConfig{GridOverview: KeyBinding{"G"}},
	}
	got := Migrate(cfg)
	if got.Keybindings.GridOverview.First() != "G" {
		t.Errorf("GridOverview = %v, want [G]", got.Keybindings.GridOverview)
	}
}

func TestMigrate_FillsMissingStatusDetection(t *testing.T) {
	cfg := Config{
		SchemaVersion: currentSchemaVersion,
		Agents: map[string]AgentProfile{
			"claude": {Cmd: []string{"claude"}}, // no Status
		},
	}
	got := Migrate(cfg)
	profile := got.Agents["claude"]
	if profile.Status.StableTicks == 0 {
		t.Error("Migrate should fill missing StatusDetection from defaults")
	}
	if profile.Status.RunTitle == "" {
		t.Error("Migrate should fill RunTitle for claude")
	}
}

func TestMigrate_PreservesExistingStatusDetection(t *testing.T) {
	cfg := Config{
		SchemaVersion: currentSchemaVersion,
		Agents: map[string]AgentProfile{
			"claude": {
				Cmd:    []string{"claude"},
				Status: StatusDetection{WaitTitle: "custom", StableTicks: 5},
			},
		},
	}
	got := Migrate(cfg)
	profile := got.Agents["claude"]
	if profile.Status.WaitTitle != "custom" {
		t.Errorf("WaitTitle = %q, want %q", profile.Status.WaitTitle, "custom")
	}
	if profile.Status.StableTicks != 5 {
		t.Errorf("StableTicks = %d, want 5", profile.Status.StableTicks)
	}
}

// TestMigrate_V1ToV2_ResetsHideAttachHint verifies that upgrading a config
// from schema v1 to v2 clears HideAttachHint so existing users see the
// re-shown pre-attach splash and learn the new single-key detach shortcut
// (#41). The reset is one-shot: after the user dismisses the hint again,
// the new value is saved alongside SchemaVersion=2 and not reset on
// subsequent startups.
func TestMigrate_V1ToV2_ResetsHideAttachHint(t *testing.T) {
	cfg := Config{SchemaVersion: 1, HideAttachHint: true}
	got := Migrate(cfg)
	if got.HideAttachHint {
		t.Error("Migrate v1→v2 should reset HideAttachHint to false so users see the new detach key splash")
	}
	if got.SchemaVersion != currentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, currentSchemaVersion)
	}
}

// TestMigrate_AlreadyV2_PreservesHideAttachHint verifies the v1→v2 reset
// only fires once. Users who have already upgraded and dismissed the splash
// should keep their preference on subsequent startups.
func TestMigrate_AlreadyV2_PreservesHideAttachHint(t *testing.T) {
	cfg := Config{SchemaVersion: 2, HideAttachHint: true}
	got := Migrate(cfg)
	if !got.HideAttachHint {
		t.Error("Migrate should preserve HideAttachHint=true when SchemaVersion is already 2")
	}
}

// TestMigrate_V2ToV3_FillsBellSound verifies that upgrading from schema v2
// to v3 populates the newly introduced BellSound field with the default so
// existing users preserve today's audible `\a` behavior (#75).
func TestMigrate_V2ToV3_FillsBellSound(t *testing.T) {
	cfg := Config{SchemaVersion: 2, BellSound: ""}
	got := Migrate(cfg)
	if got.BellSound == "" {
		t.Error("Migrate v2→v3 should fill empty BellSound from defaults")
	}
	if want := DefaultConfig().BellSound; got.BellSound != want {
		t.Errorf("BellSound = %q, want default %q", got.BellSound, want)
	}
	if got.SchemaVersion != currentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, currentSchemaVersion)
	}
}

// TestMigrate_PreservesUserBellChoice ensures the v2→v3 fill does not
// clobber a user who has already picked a custom bell sound (e.g., via a
// hand-edited config.json on a pre-release build).
func TestMigrate_PreservesUserBellChoice(t *testing.T) {
	cfg := Config{SchemaVersion: 2, BellSound: "chime"}
	got := Migrate(cfg)
	if got.BellSound != "chime" {
		t.Errorf("BellSound = %q, want %q (user choice must be preserved)", got.BellSound, "chime")
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

// TestMigrate_V3ToV4_FillsStartupView verifies that upgrading from schema v3
// to v4 populates the empty StartupView field with the default "sidebar" (#78).
func TestMigrate_V3ToV4_FillsStartupView(t *testing.T) {
	cfg := Config{SchemaVersion: 3, StartupView: ""}
	got := Migrate(cfg)
	if got.StartupView != "sidebar" {
		t.Errorf("StartupView = %q, want %q", got.StartupView, "sidebar")
	}
	if got.SchemaVersion != currentSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, currentSchemaVersion)
	}
}

// TestMigrate_V3ToV4_PreservesExistingStartupView verifies that the v3→v4
// migration does not clobber a user who has already set StartupView.
func TestMigrate_V3ToV4_PreservesExistingStartupView(t *testing.T) {
	cfg := Config{SchemaVersion: 3, StartupView: "grid-all"}
	got := Migrate(cfg)
	if got.StartupView != "grid-all" {
		t.Errorf("StartupView = %q, want %q (user choice must be preserved)", got.StartupView, "grid-all")
	}
}
