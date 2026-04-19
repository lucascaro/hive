package tui

import (
	"testing"

	"github.com/lucascaro/hive/internal/config"
)

// TestCommands_AllHaveIDAndExec is a smoke test: every registry entry must be
// fully populated. A nil Exec would silently drop palette picks; an empty ID
// would collide with another entry. Catching this at test time prevents a
// "palette action does nothing" regression.
func TestCommands_AllHaveIDAndExec(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range commands {
		if c.ID == "" {
			t.Errorf("command %q has empty ID", c.Label)
		}
		if seen[c.ID] {
			t.Errorf("duplicate command ID: %q", c.ID)
		}
		seen[c.ID] = true
		if c.Exec == nil {
			t.Errorf("command %q has nil Exec", c.ID)
		}
		if c.Scopes == 0 {
			t.Errorf("command %q has no Scopes set", c.ID)
		}
	}
}

// TestCommands_BindingsResolveToRealKeys verifies each command's Binding
// produces at least one concrete key once the default config is applied.
// A disabled binding would make the command unreachable via keyboard.
func TestCommands_BindingsResolveToRealKeys(t *testing.T) {
	cfg := config.DefaultConfig()
	km := NewKeyMap(cfg.Keybindings)
	for _, c := range commands {
		if c.Binding == nil {
			continue // palette-only commands are allowed (none today)
		}
		b := c.Binding(km)
		if len(b.Keys()) == 0 {
			t.Errorf("command %q resolves to zero keys under default config", c.ID)
		}
	}
}

// TestCommands_FindCommand verifies lookup by ID works for every entry. The
// palette dispatcher (handle_palette.go) relies on this to find executors,
// and a typo in the registry would cause a silent palette no-op.
func TestCommands_FindCommand(t *testing.T) {
	for _, c := range commands {
		got := findCommand(c.ID)
		if got == nil {
			t.Errorf("findCommand(%q) returned nil", c.ID)
			continue
		}
		if got.ID != c.ID {
			t.Errorf("findCommand(%q).ID = %q", c.ID, got.ID)
		}
	}
	if findCommand("nonexistent-action-xyz") != nil {
		t.Error("findCommand for missing ID should return nil")
	}
}

// TestCommands_CoreActionsPresent is a structural guard — the palette bug
// (#119) that motivated the registry was "action reachable from one view but
// not the other." This test pins down that the core actions are all present
// in both scopes where applicable. A regression where, say, kill-session
// loses ScopeGrid would cause the fix to silently revert.
func TestCommands_CoreActionsPresent(t *testing.T) {
	wantBoth := []string{
		"new-session", "new-worktree", "kill-session", "rename",
		"color-next", "color-prev", "session-color-next", "session-color-prev",
		"help", "tmux-help", "settings", "quit", "quit-kill",
	}
	wantGlobalOnly := []string{
		"new-project", "new-team", "kill-team", "grid", "grid-all",
		"sidebar", "filter", "attach",
	}

	byID := map[string]*Command{}
	for i := range commands {
		byID[commands[i].ID] = &commands[i]
	}

	for _, id := range wantBoth {
		c, ok := byID[id]
		if !ok {
			t.Errorf("missing command %q", id)
			continue
		}
		if c.Scopes&ScopeGlobal == 0 {
			t.Errorf("command %q missing ScopeGlobal", id)
		}
		if c.Scopes&ScopeGrid == 0 {
			t.Errorf("command %q missing ScopeGrid", id)
		}
	}
	for _, id := range wantGlobalOnly {
		c, ok := byID[id]
		if !ok {
			t.Errorf("missing command %q", id)
			continue
		}
		if c.Scopes&ScopeGlobal == 0 {
			t.Errorf("command %q missing ScopeGlobal", id)
		}
	}
}
