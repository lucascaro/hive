package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/tui/components"
)

// These tests pin down the central invariant of PR #127: a command invoked
// via palette-pick produces the same observable state as invoking it via
// its direct keybinding — from both sidebar and grid contexts. This is the
// #119-class regression the command registry was built to structurally
// prevent.

// TestParity_KillSession_Sidebar verifies that selecting "Kill session" in the
// palette from sidebar view produces the same ConfirmActionMsg as pressing
// the kill-session key directly.
func TestParity_KillSession_Sidebar(t *testing.T) {
	// Direct-key path.
	m1, mock1 := testFlowModel(t)
	f1 := newFlowRunner(t, m1, mock1)
	cmd1 := f1.SendKey("x")
	msg1 := execConfirmCmd(t, cmd1)

	// Palette path.
	m2, mock2 := testFlowModel(t)
	f2 := newFlowRunner(t, m2, mock2)
	f2.Send(tea.KeyMsg{Type: tea.KeyCtrlP})
	cmd2 := f2.Send(components.CommandPalettePickedMsg{Action: "kill-session"})
	msg2 := execConfirmCmd(t, cmd2)

	if msg1.Action != msg2.Action {
		t.Errorf("direct-key Action=%q, palette Action=%q", msg1.Action, msg2.Action)
	}
	if msg1.Message != msg2.Message {
		t.Errorf("direct-key Message=%q, palette Message=%q", msg1.Message, msg2.Message)
	}
}

// TestParity_Rename_Sidebar verifies rename produces the same terminal state
// (ViewRename on the stack) whether triggered by key or palette.
func TestParity_Rename_Sidebar(t *testing.T) {
	m1, mock1 := testFlowModel(t)
	f1 := newFlowRunner(t, m1, mock1)
	f1.SendKey("r")
	top1 := f1.model.TopView()

	m2, mock2 := testFlowModel(t)
	f2 := newFlowRunner(t, m2, mock2)
	f2.Send(tea.KeyMsg{Type: tea.KeyCtrlP})
	f2.Send(components.CommandPalettePickedMsg{Action: "rename"})
	top2 := f2.model.TopView()

	if top1 != top2 {
		t.Errorf("direct-key TopView=%s, palette TopView=%s", top1, top2)
	}
	if top1 != ViewRename {
		t.Errorf("expected ViewRename on top, got %s", top1)
	}
}

// TestParity_NewSession_Sidebar verifies new-session opens AgentPicker the
// same way from key and palette.
func TestParity_NewSession_Sidebar(t *testing.T) {
	m1, mock1 := testFlowModel(t)
	f1 := newFlowRunner(t, m1, mock1)
	f1.SendKey("t")
	top1 := f1.model.TopView()
	pid1 := f1.model.pendingProjectID

	m2, mock2 := testFlowModel(t)
	f2 := newFlowRunner(t, m2, mock2)
	f2.Send(tea.KeyMsg{Type: tea.KeyCtrlP})
	f2.Send(components.CommandPalettePickedMsg{Action: "new-session"})
	top2 := f2.model.TopView()
	pid2 := f2.model.pendingProjectID

	if top1 != top2 {
		t.Errorf("direct-key TopView=%s, palette TopView=%s", top1, top2)
	}
	if top1 != ViewAgentPicker {
		t.Errorf("expected ViewAgentPicker on top, got %s", top1)
	}
	if pid1 != pid2 {
		t.Errorf("direct-key pendingProjectID=%q, palette pendingProjectID=%q", pid1, pid2)
	}
}

// TestParity_KillSession_Grid verifies kill-session from grid view produces
// the same ConfirmActionMsg via key and palette — this is the exact case
// #119 fixed (palette working from grid).
func TestParity_KillSession_Grid(t *testing.T) {
	// Direct-key from grid.
	m1, mock1 := testFlowModel(t)
	f1 := newFlowRunner(t, m1, mock1)
	f1.SendKey("g")
	f1.AssertGridActive(true)
	cmd1 := f1.SendKey("x")
	msg1 := execConfirmCmd(t, cmd1)

	// Palette from grid.
	m2, mock2 := testFlowModel(t)
	f2 := newFlowRunner(t, m2, mock2)
	f2.SendKey("g")
	f2.AssertGridActive(true)
	f2.Send(tea.KeyMsg{Type: tea.KeyCtrlP})
	cmd2 := f2.Send(components.CommandPalettePickedMsg{Action: "kill-session"})
	msg2 := execConfirmCmd(t, cmd2)

	if msg1.Action != msg2.Action {
		t.Errorf("direct-key Action=%q, palette Action=%q", msg1.Action, msg2.Action)
	}
	if msg1.Message != msg2.Message {
		t.Errorf("direct-key Message=%q, palette Message=%q", msg1.Message, msg2.Message)
	}
}

// TestParity_ColorNext_Grid verifies color cycling mutates state identically
// from grid key and grid palette.
func TestParity_ColorNext_Grid(t *testing.T) {
	m1, mock1 := testFlowModel(t)
	f1 := newFlowRunner(t, m1, mock1)
	f1.SendKey("g")
	f1.SendKey("c")
	color1 := f1.model.appState.Projects[0].Color

	m2, mock2 := testFlowModel(t)
	f2 := newFlowRunner(t, m2, mock2)
	f2.SendKey("g")
	f2.Send(tea.KeyMsg{Type: tea.KeyCtrlP})
	f2.Send(components.CommandPalettePickedMsg{Action: "color-next"})
	color2 := f2.model.appState.Projects[0].Color

	if color1 != color2 {
		t.Errorf("direct-key color=%q, palette color=%q", color1, color2)
	}
}

// TestPalette_DisabledItemsPresent verifies that Enabled=false commands are
// rendered dimmed rather than hidden — so users discover the keybinding even
// when the current selection can't act on it. Guards the UX decision that
// deviated from the original "hide" plan.
func TestPalette_DisabledItemsPresent(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Select the project row (not a session) → kill-session's target is a
	// project, which IS killable (kill-project branch), so that's still
	// enabled. Switch to a state where kill-team has no target: initial
	// state has no teams, so "kill-team" should be disabled-but-present.
	items := f.model.paletteItems()

	foundKillTeam := false
	for _, it := range items {
		pi, ok := it.(components.PaletteItem)
		if !ok {
			continue
		}
		if pi.FilterValue() == "Kill team" {
			foundKillTeam = true
			if !pi.Disabled() {
				t.Error("Kill team should be Disabled when no team is selected")
			}
		}
	}
	if !foundKillTeam {
		t.Error("Kill team should still appear in palette when disabled, not be hidden")
	}
}

// execConfirmCmd runs a tea.Cmd expected to return a ConfirmActionMsg. Fails
// the test if the cmd is nil or produces a different message type.
func execConfirmCmd(t *testing.T, cmd tea.Cmd) ConfirmActionMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected non-nil cmd, got nil")
	}
	msg := cmd()
	confirm, ok := msg.(ConfirmActionMsg)
	if !ok {
		t.Fatalf("expected ConfirmActionMsg, got %T", msg)
	}
	return confirm
}
