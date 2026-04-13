package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/mux/muxtest"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

// testFlowModelWithVimNav creates a Model whose NavUp/NavDown are set to
// vim-style "k"/"j" (simulating a user config saved before #79 changed
// the defaults to arrow keys). Arrow keys must still work because
// keys.go permanently adds "up"/"down" as aliases.
func testFlowModelWithVimNav(t *testing.T) (*flowRunner, *muxtest.MockBackend) {
	t.Helper()

	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)
	t.Setenv("TERM", "dumb")

	mock := muxtest.New()
	mux.SetBackend(mock)
	t.Cleanup(func() { mux.SetBackend(nil) })

	mock.SetPaneContent("hive-sessions:0", "$ claude\nSession started.")
	mock.SetPaneContent("hive-sessions:1", "$ codex\nReady.")

	cfg := config.DefaultConfig()
	cfg.HideAttachHint = true
	cfg.PreviewRefreshMs = 1
	// Simulate old config: vim-style nav keys instead of arrow keys.
	cfg.Keybindings.NavUp = "k"
	cfg.Keybindings.NavDown = "j"

	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40

	m := New(cfg, appState, "")
	m.appState.TermWidth = 120
	m.appState.TermHeight = 40

	return newFlowRunner(t, m, mock), mock
}

// TestFlow_ArrowKeys_NavigateSidebar_WithVimConfig verifies that arrow keys
// still navigate the sidebar even when the user's config has vim-style
// NavUp="k"/NavDown="j" (old default before #79).
func TestFlow_ArrowKeys_NavigateSidebar_WithVimConfig(t *testing.T) {
	f, _ := testFlowModelWithVimNav(t)

	// Sidebar starts on sess-1. Navigate down to proj-2/sess-2 with arrow key.
	before := f.model.sidebar.Cursor
	f.SendSpecialKey(tea.KeyDown)
	after := f.model.sidebar.Cursor
	if after == before {
		t.Errorf("Down arrow did not move sidebar cursor (before=%d after=%d); arrow keys broken with vim nav config", before, after)
	}

	// Navigate back up with arrow key.
	f.SendSpecialKey(tea.KeyUp)
	if f.model.sidebar.Cursor != before {
		t.Errorf("Up arrow did not restore cursor (got=%d want=%d)", f.model.sidebar.Cursor, before)
	}
}

// TestFlow_VimKeys_NavigateSidebar_WithVimConfig verifies that vim keys
// j/k still navigate the sidebar when NavUp/NavDown are set to them.
func TestFlow_VimKeys_NavigateSidebar_WithVimConfig(t *testing.T) {
	f, _ := testFlowModelWithVimNav(t)

	before := f.model.sidebar.Cursor
	f.SendKey("j")
	after := f.model.sidebar.Cursor
	if after == before {
		t.Errorf("j key did not move sidebar cursor (before=%d after=%d)", before, after)
	}

	f.SendKey("k")
	if f.model.sidebar.Cursor != before {
		t.Errorf("k key did not restore cursor (got=%d want=%d)", f.model.sidebar.Cursor, before)
	}
}

// TestFlow_ArrowKeys_NavigateSidebar_DefaultConfig verifies arrow keys work
// with the standard default config (NavUp="up"/NavDown="down").
func TestFlow_ArrowKeys_NavigateSidebar_DefaultConfig(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	before := f.model.sidebar.Cursor
	f.SendSpecialKey(tea.KeyDown)
	if f.model.sidebar.Cursor == before {
		t.Errorf("Down arrow did not move sidebar cursor with default config")
	}

	f.SendSpecialKey(tea.KeyUp)
	if f.model.sidebar.Cursor != before {
		t.Errorf("Up arrow did not restore sidebar cursor with default config")
	}
}

// TestFlow_H_CollapsesSidebarProject verifies that pressing "h" while a
// project is selected collapses it (vim-style left = collapse).
func TestFlow_H_CollapsesSidebarProject(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Navigate to proj-1 header (first item in sidebar).
	for f.model.sidebar.Cursor != 0 {
		f.SendSpecialKey(tea.KeyUp)
	}
	sel := f.model.sidebar.Selected()
	if sel == nil || sel.Kind != components.KindProject {
		t.Skip("first sidebar item is not a project — cannot test collapse")
	}
	projID := sel.ProjectID

	proj := func() *state.Project {
		return state.FindProject(&f.model.appState, projID)
	}
	if proj().Collapsed {
		t.Fatal("precondition: project must not be collapsed")
	}

	f.SendKey("h")

	if !proj().Collapsed {
		t.Errorf("pressing 'h' on a project did not collapse it (vim-style left)")
	}
}

// TestFlow_L_ExpandsSidebarProject verifies that pressing "l" while a
// collapsed project is selected expands it (vim-style right = expand).
func TestFlow_L_ExpandsSidebarProject(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Navigate to proj-1 header.
	for f.model.sidebar.Cursor != 0 {
		f.SendSpecialKey(tea.KeyUp)
	}
	sel := f.model.sidebar.Selected()
	if sel == nil || sel.Kind != components.KindProject {
		t.Skip("first sidebar item is not a project")
	}
	projID := sel.ProjectID

	// First collapse via space.
	f.SendKey(" ")
	proj := state.FindProject(&f.model.appState, projID)
	if !proj.Collapsed {
		t.Fatal("precondition: project should be collapsed after space")
	}

	// Now expand via "l".
	f.SendKey("l")
	proj = state.FindProject(&f.model.appState, projID)
	if proj.Collapsed {
		t.Errorf("pressing 'l' on a collapsed project did not expand it (vim-style right)")
	}
}

// TestFlow_Settings_ResetKeybindings verifies that pressing "R" in Settings
// resets cfg.Keybindings to defaults and marks the view dirty.
func TestFlow_Settings_ResetKeybindings(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Modify NavUp to a custom value to prove reset works.
	f.model.cfg.Keybindings.NavUp = "k"
	openSettings(t, f)

	if f.model.settings.IsDirty() {
		t.Fatal("precondition: settings must not be dirty before reset")
	}

	f.SendKey("R")

	if !f.model.settings.IsDirty() {
		t.Error("settings should be dirty after keybinding reset")
	}
	got := f.model.settings.GetConfig().Keybindings.NavUp
	want := config.DefaultConfig().Keybindings.NavUp
	if got != want {
		t.Errorf("NavUp after reset = %q, want %q", got, want)
	}
}
