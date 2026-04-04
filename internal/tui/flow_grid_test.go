package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
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
	f.ExecCmdChain(cmd)

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
	f.ExecCmdChain(cmd)

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
	f.ExecCmdChain(cmd)

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

// TestFlow_GridAttachDoneSchedulesGridPoll verifies that returning from
// tmux attach to the grid view restarts the grid preview polling chain.
func TestFlow_GridAttachDoneSchedulesGridPoll(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Simulate returning from tmux attach with grid restore.
	cmd := f.Send(AttachDoneMsg{RestoreGridMode: state.GridRestoreProject})
	f.AssertGridActive(true)

	// The returned cmd should be a tea.Batch. Execute all sub-commands and
	// check that at least one produces a GridPreviewsUpdatedMsg.
	if cmd == nil {
		t.Fatal("expected non-nil cmd from AttachDoneMsg")
	}
	batchMsg := cmd()
	batch, ok := batchMsg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %T", batchMsg)
	}

	foundGridPoll := false
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		msg := sub()
		if _, ok := msg.(components.GridPreviewsUpdatedMsg); ok {
			foundGridPoll = true
			break
		}
	}
	if !foundGridPoll {
		t.Error("AttachDoneMsg with grid restore should schedule grid preview polling")
	}
}

// TestFlow_GridExitSyncsSidebar tests that exiting the grid (esc) syncs
// ActiveSessionID and sidebar cursor to the grid's selected session.
func TestFlow_GridExitSyncsSidebar(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Initial active session is sess-1.
	f.AssertActiveSession("sess-1")

	// Open all-projects grid (shows sess-1 and sess-2).
	f.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	f.AssertGridActive(true)

	// Navigate right to second session (sess-2).
	f.SendKey("l")

	// Exit grid with esc.
	f.SendSpecialKey(tea.KeyEscape)
	f.AssertGridActive(false)

	// ActiveSessionID should now be sess-2.
	f.AssertActiveSession("sess-2")

	// Sidebar cursor should also point to sess-2.
	model := f.Model()
	sel := model.sidebar.Selected()
	if sel == nil || sel.SessionID != "sess-2" {
		t.Errorf("sidebar cursor should be on sess-2, got %v", sel)
	}
}

// TestFlow_GridAttachSetsActiveSessionID tests that selecting a session in the
// grid (GridSessionSelectedMsg) updates ActiveSessionID before attach.
func TestFlow_GridAttachSetsActiveSessionID(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Initial active session is sess-1.
	f.AssertActiveSession("sess-1")

	// Open all-projects grid and navigate to sess-2.
	f.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	f.SendKey("l")

	// Press enter to select — grid emits GridSessionSelectedMsg via cmd.
	cmd := f.SendSpecialKey(tea.KeyEnter)
	f.AssertGridActive(false)

	// Execute the cmd chain to deliver GridSessionSelectedMsg.
	f.ExecCmdChain(cmd)

	// ActiveSessionID should be sess-2 (the one we selected in the grid).
	f.AssertActiveSession("sess-2")
}

// TestFlow_AttachDoneSyncsSidebar tests that returning from attach
// (AttachDoneMsg) rebuilds the sidebar and syncs it to ActiveSessionID.
func TestFlow_AttachDoneSyncsSidebar(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Set active session to sess-2 (as if we just attached to it).
	f.Send(components.GridSessionSelectedMsg{
		TmuxSession: "hive-proj5678",
		TmuxWindow:  0,
	})

	// Simulate detach returning.
	f.Send(AttachDoneMsg{RestoreGridMode: state.GridRestoreNone})

	// Sidebar should now point to sess-2.
	model := f.Model()
	sel := model.sidebar.Selected()
	if sel == nil || sel.SessionID != "sess-2" {
		t.Errorf("sidebar cursor should be on sess-2 after AttachDoneMsg, got %v", sel)
	}
}

// TestFlow_GridSelectAttachDetachRoundTrip tests the full round trip:
// grid cursor on sess-2 → GridSessionSelectedMsg → AttachDoneMsg → both
// cursors should be on sess-2.
func TestFlow_GridSelectAttachDetachRoundTrip(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.AssertActiveSession("sess-1")

	// Open all-projects grid and navigate to sess-2.
	f.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	f.SendKey("l")

	// Select sess-2 via enter.
	cmd := f.SendSpecialKey(tea.KeyEnter)
	f.ExecCmdChain(cmd)
	f.AssertActiveSession("sess-2")

	// Simulate detach returning (tmux backend path, grid restore).
	f.Send(AttachDoneMsg{RestoreGridMode: state.GridRestoreAll})

	// Both grid and sidebar should be on sess-2.
	f.AssertActiveSession("sess-2")

	model := f.Model()
	gridSel := model.gridView.Selected()
	if gridSel == nil || gridSel.ID != "sess-2" {
		t.Errorf("grid cursor should be on sess-2, got %v", gridSel)
	}

	sidebarSel := model.sidebar.Selected()
	if sidebarSel == nil || sidebarSel.SessionID != "sess-2" {
		t.Errorf("sidebar cursor should be on sess-2, got %v", sidebarSel)
	}
}

// TestFlow_GridExitSyncsActiveProjectID verifies that exiting the all-projects
// grid after navigating to a session in a different project updates
// ActiveProjectID so the next project-scoped grid shows the right project.
func TestFlow_GridExitSyncsActiveProjectID(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Initial state: active project is proj-1.
	if f.Model().appState.ActiveProjectID != "proj-1" {
		t.Fatalf("initial ActiveProjectID = %q, want proj-1", f.Model().appState.ActiveProjectID)
	}

	// Open all-projects grid (shows sess-1 from proj-1 and sess-2 from proj-2).
	f.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	f.AssertGridActive(true)

	// Navigate right to sess-2 (proj-2).
	f.SendKey("l")

	// Exit grid with esc.
	f.ExecCmdChain(f.SendSpecialKey(tea.KeyEscape))
	f.AssertGridActive(false)

	// ActiveProjectID should now be proj-2.
	if f.Model().appState.ActiveProjectID != "proj-2" {
		t.Errorf("ActiveProjectID = %q, want proj-2", f.Model().appState.ActiveProjectID)
	}
}

// TestFlow_GridAttachSyncsActiveProjectID verifies that selecting a session
// from a different project in the grid updates ActiveProjectID.
func TestFlow_GridAttachSyncsActiveProjectID(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	if f.Model().appState.ActiveProjectID != "proj-1" {
		t.Fatalf("initial ActiveProjectID = %q, want proj-1", f.Model().appState.ActiveProjectID)
	}

	// Open all-projects grid and navigate to sess-2 (proj-2).
	f.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	f.SendKey("l")

	// Press enter to select → attach.
	cmd := f.SendSpecialKey(tea.KeyEnter)
	f.ExecCmdChain(cmd)

	// ActiveProjectID should be proj-2.
	if f.Model().appState.ActiveProjectID != "proj-2" {
		t.Errorf("ActiveProjectID = %q after grid attach, want proj-2", f.Model().appState.ActiveProjectID)
	}
}

// TestFlow_GridExitStartsPreviewPoll verifies that exiting the grid with a
// different active session bumps the preview poll generation so the preview
// switches to the new session's content.
func TestFlow_GridExitStartsPreviewPoll(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	genBefore := f.Model().previewPollGen

	// Open all-projects grid and navigate to sess-2.
	f.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	f.SendKey("l")

	// Exit grid with esc.
	f.ExecCmdChain(f.SendSpecialKey(tea.KeyEscape))

	genAfter := f.Model().previewPollGen
	if genAfter <= genBefore {
		t.Errorf("previewPollGen should increase on grid exit with session change: before=%d after=%d", genBefore, genAfter)
	}
}

// TestFlow_BackgroundRebuildDoesNotStealCursor verifies that a background
// sidebar rebuild (e.g. from a status change) does not move the cursor away
// from a project/team row the user is navigating.
func TestFlow_BackgroundRebuildDoesNotStealCursor(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Initial cursor is on sess-1 (index varies, but sidebar should have items).
	// Move cursor to the project row (index 0).
	model := f.Model()
	model.sidebar.Cursor = 0
	model.appState.ActiveSessionID = "sess-1"

	// Verify cursor is on a project row.
	sel := model.sidebar.Selected()
	if sel == nil || sel.Kind != components.KindProject {
		t.Fatalf("expected cursor on project row, got %v", sel)
	}

	// Simulate a background status change that triggers Rebuild.
	model.sidebar.Rebuild(&model.appState)

	// Cursor should still be on the project row, not stolen to sess-1.
	sel = model.sidebar.Selected()
	if sel == nil || sel.Kind != components.KindProject {
		t.Errorf("cursor should stay on project row after background rebuild, got kind=%d", sel.Kind)
	}
	if model.sidebar.Cursor != 0 {
		t.Errorf("cursor moved from 0 to %d during background rebuild", model.sidebar.Cursor)
	}
}

