package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

func testModelForStack() Model {
	cfg := config.DefaultConfig()
	cfg.PreviewRefreshMs = 1
	appState := state.AppState{
		Projects: []*state.Project{{
			ID:   "proj-1",
			Name: "test",
			Sessions: []*state.Session{{
				ID:          "sess-1",
				ProjectID:   "proj-1",
				Title:       "session-1",
				AgentType:   state.AgentClaude,
				TmuxSession: "hive-test",
				TmuxWindow:  0,
				Status:      state.StatusRunning,
			}},
		}},
		ActiveProjectID: "proj-1",
		ActiveSessionID: "sess-1",
		AgentUsage:      make(map[string]state.AgentUsageRecord),
		TermWidth:       120,
		TermHeight:      40,
	}
	return New(cfg, appState, "", "")
}

func TestPushView_AddsToStack(t *testing.T) {
	m := testModelForStack()
	if m.TopView() != ViewMain {
		t.Fatalf("initial top = %q, want %q", m.TopView(), ViewMain)
	}

	m.PushView(ViewGrid)
	if m.TopView() != ViewGrid {
		t.Fatalf("after push grid, top = %q, want %q", m.TopView(), ViewGrid)
	}

	m.PushView(ViewRename)
	if m.TopView() != ViewRename {
		t.Fatalf("after push rename, top = %q, want %q", m.TopView(), ViewRename)
	}
}

func TestPopView_LIFO(t *testing.T) {
	m := testModelForStack()
	m.PushView(ViewGrid)
	m.PushView(ViewRename)
	m.PushView(ViewConfirm)

	got := m.PopView()
	if got != ViewConfirm {
		t.Fatalf("first pop = %q, want %q", got, ViewConfirm)
	}
	got = m.PopView()
	if got != ViewRename {
		t.Fatalf("second pop = %q, want %q", got, ViewRename)
	}
	got = m.PopView()
	if got != ViewGrid {
		t.Fatalf("third pop = %q, want %q", got, ViewGrid)
	}
	if m.TopView() != ViewMain {
		t.Fatalf("after popping all, top = %q, want %q", m.TopView(), ViewMain)
	}
}

func TestPopView_NeverPopsBelowMain(t *testing.T) {
	m := testModelForStack()
	got := m.PopView()
	if got != ViewMain {
		t.Fatalf("pop on base stack = %q, want %q", got, ViewMain)
	}
	if m.TopView() != ViewMain {
		t.Fatalf("top after over-pop = %q, want %q", m.TopView(), ViewMain)
	}
}

func TestHasView(t *testing.T) {
	m := testModelForStack()
	m.PushView(ViewGrid)
	m.PushView(ViewRename)

	if !m.HasView(ViewMain) {
		t.Error("HasView(ViewMain) should be true")
	}
	if !m.HasView(ViewGrid) {
		t.Error("HasView(ViewGrid) should be true")
	}
	if !m.HasView(ViewRename) {
		t.Error("HasView(ViewRename) should be true")
	}
	if m.HasView(ViewSettings) {
		t.Error("HasView(ViewSettings) should be false")
	}
}

func TestReplaceTop(t *testing.T) {
	m := testModelForStack()
	m.PushView(ViewGrid)
	m.PushView(ViewAgentPicker)

	m.ReplaceTop(ViewCustomCmd)

	if m.TopView() != ViewCustomCmd {
		t.Fatalf("after ReplaceTop, top = %q, want %q", m.TopView(), ViewCustomCmd)
	}
	if !m.HasView(ViewGrid) {
		t.Error("grid should still be in stack after ReplaceTop")
	}
	if m.HasView(ViewAgentPicker) {
		t.Error("agent picker should not be in stack after ReplaceTop")
	}
}

func TestReplaceTop_OnBaseStack_PushesInstead(t *testing.T) {
	m := testModelForStack()
	m.ReplaceTop(ViewHelp)
	if m.TopView() != ViewHelp {
		t.Fatalf("ReplaceTop on base = %q, want %q", m.TopView(), ViewHelp)
	}
	if !m.HasView(ViewMain) {
		t.Error("main should still be in stack")
	}
}

func TestSyncLegacyFlags_PushSetsFlags(t *testing.T) {
	m := testModelForStack()

	m.PushView(ViewHelp)
	if !m.appState.ShowHelp {
		t.Error("ShowHelp should be true after pushing ViewHelp")
	}

	m.PopView()
	if m.appState.ShowHelp {
		t.Error("ShowHelp should be false after popping ViewHelp")
	}
}

func TestSyncLegacyFlags_GridActive(t *testing.T) {
	m := testModelForStack()
	m.PushView(ViewGrid)
	if !m.gridView.Active {
		t.Error("gridView.Active should be true after pushing ViewGrid")
	}
	m.PopView()
	if m.gridView.Active {
		t.Error("gridView.Active should be false after popping ViewGrid")
	}
}

func TestSyncLegacyFlags_Filter(t *testing.T) {
	m := testModelForStack()
	m.PushView(ViewFilter)
	if !m.appState.FilterActive {
		t.Error("FilterActive should be true after pushing ViewFilter")
	}
	m.PopView()
	if m.appState.FilterActive {
		t.Error("FilterActive should be false after popping ViewFilter")
	}
}

// Scenario tests for the grid→dialog→back-to-grid flow.

func TestScenario_GridRename_ReturnsToGrid(t *testing.T) {
	m := testModelForStack()

	// Open grid, then push rename on top.
	m.PushView(ViewGrid)
	m.PushView(ViewRename)

	if m.TopView() != ViewRename {
		t.Fatalf("top = %q, want ViewRename", m.TopView())
	}
	if !m.HasView(ViewGrid) {
		t.Fatal("grid should be in stack under rename")
	}

	// Pop rename (user pressed esc or enter).
	m.PopView()
	if m.TopView() != ViewGrid {
		t.Fatalf("after pop, top = %q, want ViewGrid", m.TopView())
	}
}

func TestScenario_GridConfirm_ReturnsToGrid(t *testing.T) {
	m := testModelForStack()

	m.PushView(ViewGrid)
	m.PushView(ViewConfirm)

	if m.TopView() != ViewConfirm {
		t.Fatalf("top = %q, want ViewConfirm", m.TopView())
	}

	m.PopView()
	if m.TopView() != ViewGrid {
		t.Fatalf("after pop, top = %q, want ViewGrid", m.TopView())
	}
}

func TestScenario_GridAgentPicker_ReturnsToGrid(t *testing.T) {
	m := testModelForStack()

	m.PushView(ViewGrid)
	m.PushView(ViewAgentPicker)

	m.PopView()
	if m.TopView() != ViewGrid {
		t.Fatalf("after pop, top = %q, want ViewGrid", m.TopView())
	}
}

func TestScenario_ProjectWizard_NameToDirToConfirm(t *testing.T) {
	m := testModelForStack()

	m.PushView(ViewProjectName)
	if m.TopView() != ViewProjectName {
		t.Fatal("top should be project-name")
	}

	// Step 1 → Step 2: replace with dir picker.
	m.ReplaceTop(ViewDirPicker)
	if m.TopView() != ViewDirPicker {
		t.Fatal("top should be dir-picker")
	}
	if m.HasView(ViewProjectName) {
		t.Error("project-name should not be in stack after ReplaceTop")
	}

	// Dir doesn't exist → replace with dir confirm.
	m.ReplaceTop(ViewDirConfirm)
	if m.TopView() != ViewDirConfirm {
		t.Fatal("top should be dir-confirm")
	}

	// User confirms → pop.
	m.PopView()
	if m.TopView() != ViewMain {
		t.Fatalf("after wizard done, top = %q, want ViewMain", m.TopView())
	}
}

func TestRefreshGrid_NoopWhenGridNotInStack(t *testing.T) {
	m := testModelForStack()
	// Should not panic when grid is not in the stack.
	m.refreshGrid()
}

// Integration tests that exercise the actual Update() pipeline.

func TestIntegration_GridRename_Esc_ReturnsToGrid(t *testing.T) {
	m := testModelForStack()

	// Open grid.
	m.openGrid(state.GridRestoreProject)

	if m.TopView() != ViewGrid {
		t.Fatalf("after opening grid, top = %q, want ViewGrid", m.TopView())
	}

	// Press "r" to rename from grid.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = result.(Model)

	if m.TopView() != ViewRename {
		t.Fatalf("after pressing r in grid, top = %q, want ViewRename", m.TopView())
	}
	if !m.HasView(ViewGrid) {
		t.Fatal("grid should still be in stack under rename")
	}

	// Press esc to cancel rename.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = result.(Model)

	if m.TopView() != ViewGrid {
		t.Fatalf("after esc from rename, top = %q, want ViewGrid", m.TopView())
	}
}

func TestIntegration_GridKill_Cancel_ReturnsToGrid(t *testing.T) {
	m := testModelForStack()

	// Open grid.
	m.openGrid(state.GridRestoreProject)

	// Press "x" to kill from grid.
	result, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = result.(Model)

	// The "x" handler returns a func that produces ConfirmActionMsg.
	// Process it to push the confirm dialog.
	if cmd != nil {
		msg := cmd()
		result, _ = m.Update(msg)
		m = result.(Model)
	}

	if m.TopView() != ViewConfirm {
		t.Fatalf("after x+confirm, top = %q, want ViewConfirm", m.TopView())
	}
	if !m.HasView(ViewGrid) {
		t.Fatal("grid should still be in stack under confirm")
	}

	// Press esc to cancel the confirm.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = result.(Model)

	if m.TopView() != ViewGrid {
		t.Fatalf("after cancelling confirm, top = %q, want ViewGrid", m.TopView())
	}
}

// Scenario tests for orphan/recovery overlays over grid.

func TestScenario_GridOrphan_ReturnsToGrid(t *testing.T) {
	m := testModelForStack()

	m.PushView(ViewGrid)
	m.PushView(ViewOrphan)

	if m.TopView() != ViewOrphan {
		t.Fatalf("top = %q, want ViewOrphan", m.TopView())
	}
	if !m.HasView(ViewGrid) {
		t.Fatal("grid should be in stack under orphan")
	}

	m.PopView()
	if m.TopView() != ViewGrid {
		t.Fatalf("after pop, top = %q, want ViewGrid", m.TopView())
	}
}

func TestScenario_GridRecovery_ReturnsToGrid(t *testing.T) {
	m := testModelForStack()

	m.PushView(ViewGrid)
	m.PushView(ViewRecovery)

	if m.TopView() != ViewRecovery {
		t.Fatalf("top = %q, want ViewRecovery", m.TopView())
	}
	if !m.HasView(ViewGrid) {
		t.Fatal("grid should be in stack under recovery")
	}

	m.PopView()
	if m.TopView() != ViewGrid {
		t.Fatalf("after pop, top = %q, want ViewGrid", m.TopView())
	}
}

func TestScenario_GridRecoveryOrphan_DismissBothReturnsToGrid(t *testing.T) {
	m := testModelForStack()

	// Simulate startup: grid first, then recovery, then orphan on top.
	m.PushView(ViewGrid)
	m.PushView(ViewRecovery)
	m.PushView(ViewOrphan)

	if m.TopView() != ViewOrphan {
		t.Fatalf("top = %q, want ViewOrphan", m.TopView())
	}

	m.PopView()
	if m.TopView() != ViewRecovery {
		t.Fatalf("after first pop, top = %q, want ViewRecovery", m.TopView())
	}

	m.PopView()
	if m.TopView() != ViewGrid {
		t.Fatalf("after second pop, top = %q, want ViewGrid", m.TopView())
	}
}

func TestIntegration_GridRename_Enter_ReturnsToGrid(t *testing.T) {
	m := testModelForStack()

	// Open grid.
	m.openGrid(state.GridRestoreProject)

	// Press "r" to rename from grid.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = result.(Model)

	// Type a new title.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	m = result.(Model)

	// Press enter to confirm rename.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = result.(Model)

	if m.TopView() != ViewGrid {
		t.Fatalf("after enter from rename, top = %q, want ViewGrid", m.TopView())
	}
}

// Integration tests for orphan/recovery overlays initialized via New() with grid startup.

func TestIntegration_GridStartup_OrphanOverlayOnTop(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PreviewRefreshMs = 1
	cfg.StartupView = "grid"

	appState := state.AppState{
		Projects: []*state.Project{{
			ID:   "proj-1",
			Name: "test",
			Sessions: []*state.Session{{
				ID:          "sess-1",
				ProjectID:   "proj-1",
				Title:       "session-1",
				AgentType:   state.AgentClaude,
				TmuxSession: "hive-test",
				TmuxWindow:  0,
				Status:      state.StatusRunning,
			}},
		}},
		ActiveProjectID: "proj-1",
		ActiveSessionID: "sess-1",
		AgentUsage:      make(map[string]state.AgentUsageRecord),
		TermWidth:       120,
		TermHeight:      40,
		OrphanSessions:  []string{"hive-stale-abc"},
	}

	m := New(cfg, appState, "", "")

	// Orphan overlay must be on top of grid, not hidden beneath it.
	if m.TopView() != ViewOrphan {
		t.Fatalf("top = %q, want ViewOrphan (orphan overlay should be above grid)", m.TopView())
	}
	if !m.HasView(ViewGrid) {
		t.Fatal("grid should be in the stack beneath the orphan overlay")
	}

	// Dismiss orphan overlay with esc — should return to grid.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = result.(Model)
	// Process the OrphanPickerDoneMsg.
	result, _ = m.Update(components.OrphanPickerDoneMsg{})
	m = result.(Model)

	if m.TopView() != ViewGrid {
		t.Fatalf("after dismissing orphan, top = %q, want ViewGrid", m.TopView())
	}
}

func TestIntegration_GridStartup_RecoveryOverlayOnTop(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PreviewRefreshMs = 1
	cfg.StartupView = "grid"

	appState := state.AppState{
		Projects: []*state.Project{{
			ID:   "proj-1",
			Name: "test",
			Sessions: []*state.Session{{
				ID:          "sess-1",
				ProjectID:   "proj-1",
				Title:       "session-1",
				AgentType:   state.AgentClaude,
				TmuxSession: "hive-test",
				TmuxWindow:  0,
				Status:      state.StatusRunning,
			}},
		}},
		ActiveProjectID: "proj-1",
		ActiveSessionID: "sess-1",
		AgentUsage:      make(map[string]state.AgentUsageRecord),
		TermWidth:       120,
		TermHeight:      40,
		RecoverableSessions: []state.RecoverableSession{
			{
				TmuxSession:       "hive-old-abc",
				WindowIndex:       0,
				WindowName:        "main",
				DetectedAgentType: state.AgentClaude,
				PanePreview:       "$ claude",
			},
		},
	}

	m := New(cfg, appState, "", "")

	// Recovery overlay must be on top of grid.
	if m.TopView() != ViewRecovery {
		t.Fatalf("top = %q, want ViewRecovery (recovery overlay should be above grid)", m.TopView())
	}
	if !m.HasView(ViewGrid) {
		t.Fatal("grid should be in the stack beneath the recovery overlay")
	}
}

func TestIntegration_GridStartup_BothOverlays_CorrectOrder(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.PreviewRefreshMs = 1
	cfg.StartupView = "grid"

	appState := state.AppState{
		Projects: []*state.Project{{
			ID:   "proj-1",
			Name: "test",
			Sessions: []*state.Session{{
				ID:          "sess-1",
				ProjectID:   "proj-1",
				Title:       "session-1",
				AgentType:   state.AgentClaude,
				TmuxSession: "hive-test",
				TmuxWindow:  0,
				Status:      state.StatusRunning,
			}},
		}},
		ActiveProjectID: "proj-1",
		ActiveSessionID: "sess-1",
		AgentUsage:      make(map[string]state.AgentUsageRecord),
		TermWidth:       120,
		TermHeight:      40,
		OrphanSessions:  []string{"hive-stale-abc"},
		RecoverableSessions: []state.RecoverableSession{
			{
				TmuxSession:       "hive-old-abc",
				WindowIndex:       0,
				WindowName:        "main",
				DetectedAgentType: state.AgentClaude,
				PanePreview:       "$ claude",
			},
		},
	}

	m := New(cfg, appState, "", "")

	// Orphan is pushed after recovery, so orphan is on top.
	if m.TopView() != ViewOrphan {
		t.Fatalf("top = %q, want ViewOrphan", m.TopView())
	}
	if !m.HasView(ViewRecovery) {
		t.Fatal("recovery should be in the stack")
	}
	if !m.HasView(ViewGrid) {
		t.Fatal("grid should be in the stack")
	}
}
