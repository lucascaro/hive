package tui

import (
	"testing"

	"github.com/lucascaro/hive/internal/config"
)

func TestNewKeyMap_BindingsMatchConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	km := NewKeyMap(cfg.Keybindings)

	cases := []struct {
		name    string
		binding string
		want    string
	}{
		{"NewProject", km.NewProject.Keys()[0], cfg.Keybindings.NewProject},
		{"NewSession", km.NewSession.Keys()[0], cfg.Keybindings.NewSession},
		{"NewTeam", km.NewTeam.Keys()[0], cfg.Keybindings.NewTeam},
		{"KillSession", km.KillSession.Keys()[0], cfg.Keybindings.KillSession},
		{"KillTeam", km.KillTeam.Keys()[0], cfg.Keybindings.KillTeam},
		{"Rename", km.Rename.Keys()[0], cfg.Keybindings.Rename},
		{"Filter", km.Filter.Keys()[0], cfg.Keybindings.Filter},
		{"Help", km.Help.Keys()[0], cfg.Keybindings.Help},
		{"Quit", km.Quit.Keys()[0], cfg.Keybindings.Quit},
		{"QuitKill", km.QuitKill.Keys()[0], cfg.Keybindings.QuitKill},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.binding != tc.want {
				t.Errorf("%s key = %q, want %q", tc.name, tc.binding, tc.want)
			}
		})
	}
}

func TestNewKeyMap_FixedBindings(t *testing.T) {
	cfg := config.DefaultConfig()
	km := NewKeyMap(cfg.Keybindings)

	// Confirm/Cancel are always fixed regardless of config
	confirmKeys := km.Confirm.Keys()
	cancelKeys := km.Cancel.Keys()

	if len(confirmKeys) == 0 {
		t.Error("Confirm binding has no keys")
	}
	if len(cancelKeys) == 0 {
		t.Error("Cancel binding has no keys")
	}
}

func TestNewKeyMap_AttachIncludesEnter(t *testing.T) {
	cfg := config.DefaultConfig()
	km := NewKeyMap(cfg.Keybindings)
	keys := km.Attach.Keys()
	found := false
	for _, k := range keys {
		if k == "enter" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Attach keys %v should include 'enter'", keys)
	}
}
