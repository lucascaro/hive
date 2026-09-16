package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// settingsDir points the loader at a fresh directory, optionally
// seeding agent-settings.json with body.
func settingsDir(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, SettingsFileName), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", SettingsFileName, err)
		}
	}
	SetCustomDir(dir)
	t.Cleanup(func() { SetCustomDir("") })
	return dir
}

// withEnv models the variable as set (or unset) in the daemon's
// environment without mutating the real process environment.
func withEnv(t *testing.T, value string, set bool) {
	t.Helper()
	prev := lookupEnv
	lookupEnv = func(key string) (string, bool) {
		if key == ClaudeTaskToolsEnv {
			return value, set
		}
		return prev(key)
	}
	t.Cleanup(func() { lookupEnv = prev })
}

var hooked = SpawnInfo{HivedPath: "/usr/local/bin/hived", StateDir: "/tmp/state"}

// withClaudeVersion pins what `claude --version` reports, so these tests
// do not depend on whether — or which — Claude is installed on the
// machine running them.
func withClaudeVersion(t *testing.T, version string) {
	t.Helper()
	t.Cleanup(SetClaudeVersionProbeForTest(func() ([]byte, error) {
		return []byte(version + " (Claude Code)"), nil
	}))
}

func TestSettingsDefaultWhenMissing(t *testing.T) {
	settingsDir(t, "")
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("a missing file is the defaults, not an error: %v", err)
	}
	if !s.ClaudeTaskTools {
		t.Error("ClaudeTaskTools defaults to OFF; the feature would show nothing on the default model")
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	settingsDir(t, "")
	if err := SaveSettings(Settings{ClaudeTaskTools: false}); err != nil {
		t.Fatalf("save: %v", err)
	}
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if s.ClaudeTaskTools {
		t.Error("saved off, loaded on")
	}
}

// TestSettingsMissingKeyDefaultsOn: a file written before a setting
// existed must not silently switch that setting off. This is why the
// on-disk field is a pointer.
func TestSettingsMissingKeyDefaultsOn(t *testing.T) {
	settingsDir(t, "{}\n")
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !s.ClaudeTaskTools {
		t.Error("a file without the key read as off; want the default (on)")
	}
}

// TestSettingsBOMTolerated: Notepad and PowerShell write UTF-8 with a
// BOM.
func TestSettingsBOMTolerated(t *testing.T) {
	settingsDir(t, "\ufeff{\"claude_task_tools\": false}")
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("a BOM must not fail the load: %v", err)
	}
	if s.ClaudeTaskTools {
		t.Error("BOM-prefixed file ignored its value")
	}
}

// TestSettingsMalformedIsAnError: the Settings screen must be told, or
// a save over the defaults would overwrite the file the user was
// trying to fix.
func TestSettingsMalformedIsAnError(t *testing.T) {
	settingsDir(t, "{ not json")
	if _, err := LoadSettings(); err == nil {
		t.Error("a malformed file loaded without error")
	}
}

// TestSpawnSettingsMalformedDegrades: the spawn path must never fail a
// session launch over a preferences file.
func TestSpawnSettingsMalformedDegrades(t *testing.T) {
	settingsDir(t, "{ not json")
	if s := spawnSettings(); !s.ClaudeTaskTools {
		t.Error("a malformed file on the spawn path did not degrade to the defaults")
	}
}

func TestClaudeSpawnEnvOnByDefault(t *testing.T) {
	withClaudeVersion(t, "2.1.273")
	settingsDir(t, "")
	withEnv(t, "", false)
	got := claudeSpawnEnv(hooked)
	if len(got) != 1 || got[0] != ClaudeTaskToolsEnv+"=1" {
		t.Errorf("env = %v, want [%s=1]", got, ClaudeTaskToolsEnv)
	}
}

func TestClaudeSpawnEnvOffBySetting(t *testing.T) {
	withClaudeVersion(t, "2.1.273")
	settingsDir(t, `{"claude_task_tools": false}`)
	withEnv(t, "", false)
	if got := claudeSpawnEnv(hooked); len(got) != 0 {
		t.Errorf("env = %v with the setting off, want none", got)
	}
}

// TestClaudeSpawnEnvNeverOverridesTheUser: Options.Env is appended to
// the inherited environment and the later duplicate wins, so appending
// unconditionally would silently flip a user's explicit =0. That must
// hold whatever value they chose.
func TestClaudeSpawnEnvNeverOverridesTheUser(t *testing.T) {
	for _, userValue := range []string{"0", "1", ""} {
		t.Run("user set "+userValue, func(t *testing.T) {
			withClaudeVersion(t, "2.1.273")
			settingsDir(t, "")
			withEnv(t, userValue, true)
			if got := claudeSpawnEnv(hooked); len(got) != 0 {
				t.Errorf("env = %v; the user's own %s=%q must be left alone", got, ClaudeTaskToolsEnv, userValue)
			}
		})
	}
}

// TestClaudeSpawnEnvNeedsHooks: the tools cost context in every
// session and pay off only through the hooks. No hooks, no opt-in.
func TestClaudeSpawnEnvNeedsHooks(t *testing.T) {
	withClaudeVersion(t, "2.1.273")
	settingsDir(t, "")
	withEnv(t, "", false)
	if got := claudeSpawnEnv(SpawnInfo{}); len(got) != 0 {
		t.Errorf("env = %v with no hived path (so no hooks), want none", got)
	}
}

// TestSpawnEnvReadsSettingsAtSpawn: the toggle must reach the NEXT
// session without a daemon restart — the whole reason no IPC exists.
func TestSpawnEnvReadsSettingsAtSpawn(t *testing.T) {
	withClaudeVersion(t, "2.1.273")
	settingsDir(t, "")
	withEnv(t, "", false)
	if len(claudeSpawnEnv(hooked)) != 1 {
		t.Fatal("setup: expected the opt-in with defaults")
	}
	if err := SaveSettings(Settings{ClaudeTaskTools: false}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := claudeSpawnEnv(hooked); len(got) != 0 {
		t.Errorf("env = %v after switching the setting off, want none", got)
	}
}

// TestCustomAgentInheritsSpawnEnv: `claude --model haiku` gets the
// opt-in exactly when it gets the hooks, from the same name match. A
// wrapper that is not recognised gets neither.
func TestCustomAgentInheritsSpawnEnv(t *testing.T) {
	writeCustom(t, `[
		{"id":"haiku","name":"Haiku","cmd":["claude","--model","haiku"]},
		{"id":"lite","name":"Lite","cmd":["claude-lite"]},
		{"id":"mytool","name":"My Tool","cmd":["mytool"]}
	]`)
	defs := customDefs()

	haiku, ok := findDef(defs, "haiku")
	if !ok || haiku.SpawnEnv == nil || haiku.SpawnArgs == nil {
		t.Errorf("claude-based custom agent: SpawnArgs=%v SpawnEnv=%v, want both", haiku.SpawnArgs != nil, haiku.SpawnEnv != nil)
	}
	for _, id := range []ID{"lite", "mytool"} {
		d, ok := findDef(defs, id)
		if !ok {
			t.Fatalf("custom agent %q missing", id)
		}
		if d.SpawnEnv != nil || d.SpawnArgs != nil {
			t.Errorf("%s: got hooks=%v env=%v, want neither", id, d.SpawnArgs != nil, d.SpawnEnv != nil)
		}
	}
}

// TestOnlyClaudeHasSpawnEnv: no other built-in grows the variable.
func TestOnlyClaudeHasSpawnEnv(t *testing.T) {
	for id, d := range defsByID {
		if (d.SpawnEnv != nil) != (id == IDClaude) {
			t.Errorf("%s: SpawnEnv set = %v", id, d.SpawnEnv != nil)
		}
	}
}

// TestClaudeSpawnEnvNeedsASupportedVersion: the gate the env side once
// skipped. A Claude too old for Hive's hooks gets no hooks, so it must
// not get the opt-in either.
func TestClaudeSpawnEnvNeedsASupportedVersion(t *testing.T) {
	withClaudeVersion(t, "1.9.0")
	settingsDir(t, "")
	withEnv(t, "", false)
	if got := claudeSpawnEnv(hooked); len(got) != 0 {
		t.Errorf("env = %v on an unsupported Claude, want none", got)
	}
	if got := claudeSpawnArgs(hooked); len(got) != 0 {
		t.Fatalf("setup: hooks = %v on an unsupported Claude, want none", got)
	}
}
