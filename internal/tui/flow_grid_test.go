package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/state"
)

// TestFlow_GridAttachDetachRestoresGrid tests the full grid → attach → detach → grid restore flow.
// This is the exact regression test: after detaching from a session opened via the grid,
// the grid must re-open automatically.
func TestFlow_GridAttachDetachRestoresGrid(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Step 1: Starting state — sidebar + preview visible, no grid.
	f.AssertGridActive(false)
	f.ViewContains("test-project-1")
	f.Snapshot("01-sidebar")

	// Step 2: Press "g" to open project grid.
	f.SendKey("g")
	f.AssertGridActive(true)
	f.AssertGridMode(state.GridRestoreProject)
	f.Snapshot("02-grid-open")

	// Step 3: Press "enter" in grid to select session.
	// The grid's Update hides the grid and returns a cmd that produces
	// GridSessionSelectedMsg. The app handler then calls doAttach which
	// (with UseExecAttach=false) sets attachPending and returns tea.Quit.
	cmd := f.SendSpecialKey(tea.KeyEnter)

	// Grid should be hidden after selection (gridView.Update calls Hide).
	f.AssertGridActive(false)

	// Execute the cmd chain: grid cmd → GridSessionSelectedMsg → doAttach → tea.Quit.
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		if _, ok := msg.(tea.QuitMsg); ok {
			break
		}
		cmd = f.Send(msg)
	}

	// attachPending should be set with RestoreGridMode.
	updated := f.Model()
	if updated.attachPending == nil {
		t.Fatal("attachPending should be set after grid session selection")
	}
	if updated.attachPending.RestoreGridMode != state.GridRestoreProject {
		t.Fatalf("RestoreGridMode = %q, want %q",
			updated.attachPending.RestoreGridMode, state.GridRestoreProject)
	}

	// Step 4: Simulate re-entry after detach.
	// cmd/start.go creates a new Model with appState.RestoreGridMode set.
	cfg := config.DefaultConfig()
	cfg.HideAttachHint = true
	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40
	appState.RestoreGridMode = state.GridRestoreProject

	m2 := New(cfg, appState)
	m2.appState.TermWidth = 120
	m2.appState.TermHeight = 40
	f2 := newFlowRunner(t, m2, mock)

	// Grid should be restored.
	f2.AssertGridActive(true)
	f2.AssertGridMode(state.GridRestoreProject)
	f2.ViewContains("session-1")
	f2.Snapshot("03-grid-restored")
}

// TestFlow_GridAllProjectsRestores tests the "G" (all-projects) grid variant.
func TestFlow_GridAllProjectsRestores(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Open all-projects grid with "G".
	f.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	f.AssertGridActive(true)
	f.AssertGridMode(state.GridRestoreAll)

	// Both projects' sessions should be visible.
	f.ViewContains("session-1")
	f.ViewContains("session-2")
	f.Snapshot("01-all-projects-grid")

	// Press enter to select session → attach.
	cmd := f.SendSpecialKey(tea.KeyEnter)
	f.AssertGridActive(false)

	// Execute cmd chain to completion.
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		if _, ok := msg.(tea.QuitMsg); ok {
			break
		}
		cmd = f.Send(msg)
	}

	updated := f.Model()
	if updated.attachPending == nil {
		t.Fatal("attachPending should be set")
	}
	if updated.attachPending.RestoreGridMode != state.GridRestoreAll {
		t.Fatalf("RestoreGridMode = %q, want %q",
			updated.attachPending.RestoreGridMode, state.GridRestoreAll)
	}

	// Simulate re-entry with RestoreGridMode = All.
	cfg := config.DefaultConfig()
	cfg.HideAttachHint = true
	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40
	appState.RestoreGridMode = state.GridRestoreAll

	m2 := New(cfg, appState)
	m2.appState.TermWidth = 120
	m2.appState.TermHeight = 40
	f2 := newFlowRunner(t, m2, mock)

	f2.AssertGridActive(true)
	f2.AssertGridMode(state.GridRestoreAll)
	f2.ViewContains("session-1")
	f2.ViewContains("session-2")
	f2.Snapshot("02-all-restored")
}

// TestFlow_SidebarAttachDoesNotRestoreGrid tests that attaching from the sidebar
// (not from the grid) does NOT restore the grid on re-entry.
func TestFlow_SidebarAttachDoesNotRestoreGrid(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Grid should not be active.
	f.AssertGridActive(false)

	// Simulate sidebar attach via SessionAttachMsg with no grid restore.
	cmd := f.Send(SessionAttachMsg{
		TmuxSession:     "hive-sessions",
		TmuxWindow:      0,
		RestoreGridMode: state.GridRestoreNone,
		SessionTitle:    "session-1",
	})

	updated := f.Model()
	if updated.attachPending == nil {
		t.Fatal("attachPending should be set after sidebar attach")
	}
	if updated.attachPending.RestoreGridMode != state.GridRestoreNone {
		t.Fatalf("RestoreGridMode = %q, want %q",
			updated.attachPending.RestoreGridMode, state.GridRestoreNone)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}

	// Simulate re-entry with no grid restore.
	cfg := config.DefaultConfig()
	cfg.HideAttachHint = true
	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40
	appState.RestoreGridMode = state.GridRestoreNone

	m2 := New(cfg, appState)
	m2.appState.TermWidth = 120
	m2.appState.TermHeight = 40
	f2 := newFlowRunner(t, m2, mock)

	// Grid should NOT be active.
	f2.AssertGridActive(false)
	// Sidebar should be visible.
	f2.ViewContains("test-project-1")
	f2.Snapshot("01-sidebar-no-grid")
}

// TestFlow_GridOpenCloseKeyIsolation tests that opening/closing the grid
// correctly isolates and restores global key handling.
func TestFlow_GridOpenCloseKeyIsolation(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Step 1: "/" activates filter when no grid.
	f.SendKey("/")
	if !f.Model().appState.FilterActive {
		t.Fatal("FilterActive should be true after pressing / with no overlay")
	}
	// Cancel filter.
	f.SendSpecialKey(tea.KeyEscape)

	// Step 2: Open grid.
	f.SendKey("g")
	f.AssertGridActive(true)
	f.Snapshot("01-grid-open")

	// Step 3: "/" should NOT activate filter while grid is active.
	f.SendKey("/")
	if f.Model().appState.FilterActive {
		t.Fatal("FilterActive should be false — key should be consumed by grid")
	}

	// Step 4: Close grid with esc.
	f.SendSpecialKey(tea.KeyEscape)
	f.AssertGridActive(false)
	f.ViewContains("test-project-1")
	f.Snapshot("02-grid-closed")

	// Step 5: "/" should work again.
	f.SendKey("/")
	if !f.Model().appState.FilterActive {
		t.Fatal("FilterActive should be true after grid is closed")
	}
}

// TestFlow_GridNavigateAndSelect tests navigating within the grid and selecting a session.
func TestFlow_GridNavigateAndSelect(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Open all-projects grid (has 2 sessions).
	f.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	f.AssertGridActive(true)

	// Navigate right to second session.
	f.SendKey("l")

	// Verify grid still active and renders both sessions.
	f.AssertGridActive(true)
	f.ViewContains("session-1")
	f.ViewContains("session-2")
	f.Snapshot("01-grid-navigated")
}

// TestFlow_GridToggleBetweenModes tests switching from project grid to all-projects grid.
func TestFlow_GridToggleBetweenModes(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Open project grid.
	f.SendKey("g")
	f.AssertGridActive(true)
	f.AssertGridMode(state.GridRestoreProject)

	// Switch to all-projects with "G".
	f.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	f.AssertGridActive(true)
	f.AssertGridMode(state.GridRestoreAll)
	f.ViewContains("session-1")
	f.ViewContains("session-2")
	f.Snapshot("01-all-projects-after-toggle")
}

// TestFlow_GridAttachWithHint tests grid → attach with the attach hint enabled.
func TestFlow_GridAttachWithHint(t *testing.T) {
	m, mock := testFlowModelWithHint(t)

	// Pre-populate mock so the mux session exists.
	mock.SetPaneContent("hive-sessions:0", "$ claude\nSession started.")

	// Install mock backend (testFlowModelWithHint already does this, but ensure).
	f := newFlowRunner(t, m, mock)

	// Open grid.
	f.SendKey("g")
	f.AssertGridActive(true)

	// Press enter to select session — grid hides and emits GridSessionSelectedMsg.
	cmd := f.SendSpecialKey(tea.KeyEnter)
	f.AssertGridActive(false)

	// Execute cmd chain: GridSessionSelectedMsg → app handler shows hint.
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		cmd = f.Send(msg)
	}

	updated := f.Model()
	if !updated.showAttachHint {
		t.Fatal("showAttachHint should be true when HideAttachHint=false")
	}
	if updated.pendingAttach == nil {
		t.Fatal("pendingAttach should be set")
	}
	f.Snapshot("01-attach-hint")

	// Confirm attach with Enter.
	cmd = f.SendSpecialKey(tea.KeyEnter)

	updated2 := f.Model()
	if updated2.showAttachHint {
		t.Fatal("showAttachHint should be false after confirming")
	}
	if updated2.attachPending == nil {
		t.Fatal("attachPending should be set after confirming hint")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command after confirming attach")
	}
}

// TestFlow_GridAttachDoneRestoresGrid tests the tmux backend path:
// AttachDoneMsg with RestoreGridMode set should restore the grid without
// going through New() (the TUI continues in the same Model instance).
func TestFlow_GridAttachDoneRestoresGrid(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Grid should not be active initially.
	f.AssertGridActive(false)

	// Simulate returning from tmux attach with RestoreGridMode = Project.
	f.Send(AttachDoneMsg{RestoreGridMode: state.GridRestoreProject})

	f.AssertGridActive(true)
	f.AssertGridMode(state.GridRestoreProject)
	f.ViewContains("session-1")
	f.Snapshot("01-attach-done-grid-restored-project")
}

// TestFlow_GridAttachDoneRestoresAllGrid tests the tmux backend path with
// RestoreGridMode = All (all-projects grid).
func TestFlow_GridAttachDoneRestoresAllGrid(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.AssertGridActive(false)

	// Simulate returning from tmux attach with RestoreGridMode = All.
	f.Send(AttachDoneMsg{RestoreGridMode: state.GridRestoreAll})

	f.AssertGridActive(true)
	f.AssertGridMode(state.GridRestoreAll)
	f.ViewContains("session-1")
	f.ViewContains("session-2")
	f.Snapshot("02-attach-done-grid-restored-all")
}

// TestFlow_GridAttachDoneNoRestoreWhenNone tests that AttachDoneMsg with
// RestoreGridMode = None does NOT activate the grid.
func TestFlow_GridAttachDoneNoRestoreWhenNone(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.AssertGridActive(false)

	f.Send(AttachDoneMsg{RestoreGridMode: state.GridRestoreNone})

	f.AssertGridActive(false)
	f.ViewContains("test-project-1")
	f.Snapshot("03-attach-done-no-grid")
}

