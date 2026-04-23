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

// sidebarSelected returns the sidebar's current selection, working around
// the pointer receiver on a value Model.
func sidebarSelected(f *flowRunner) *components.SidebarItem {
	return f.model.sidebar.Selected()
}

// TestFlow_MoveSessionDown_Sidebar verifies Shift+Down moves a session down
// in the sidebar and cursor follows.
func TestFlow_MoveSessionDown_Sidebar(t *testing.T) {
	m, mock := testFlowModelThreeSessions(t)
	f := newFlowRunner(t, m, mock)

	// Navigate to session-1.
	for i := 0; i < 5 && f.Model().appState.ActiveSessionID != "sess-1"; i++ {
		f.SendSpecialKey(tea.KeyUp)
	}
	f.AssertActiveSession("sess-1")

	// Press Shift+Down to move session-1 down.
	f.SendSpecialKey(tea.KeyShiftDown)

	// Verify order: session-2, session-1, session-3.
	proj := f.Model().appState.Projects[0]
	if proj.Sessions[0].ID != "sess-2" || proj.Sessions[1].ID != "sess-1" || proj.Sessions[2].ID != "sess-3" {
		t.Errorf("order after MoveDown: got [%s,%s,%s] want [session-2,session-1,session-3]",
			proj.Sessions[0].Title, proj.Sessions[1].Title, proj.Sessions[2].Title)
	}

	// Cursor should still be on session-1 (now at index 1 in the sidebar items).
	sel := sidebarSelected(f)
	if sel == nil || sel.SessionID != "sess-1" {
		selID := ""
		if sel != nil {
			selID = sel.SessionID
		}
		t.Errorf("sidebar selection = %q, want %q", selID, "sess-1")
	}
}

// TestFlow_MoveSessionUp_Sidebar verifies Shift+Up moves a session up.
func TestFlow_MoveSessionUp_Sidebar(t *testing.T) {
	m, mock := testFlowModelThreeSessions(t)
	f := newFlowRunner(t, m, mock)

	// Navigate to session-3 (last).
	for i := 0; i < 5 && f.Model().appState.ActiveSessionID != "sess-3"; i++ {
		f.SendSpecialKey(tea.KeyDown)
	}
	f.AssertActiveSession("sess-3")

	// Press Shift+Up.
	f.SendSpecialKey(tea.KeyShiftUp)

	// Verify order: session-1, session-3, session-2.
	proj := f.Model().appState.Projects[0]
	if proj.Sessions[0].ID != "sess-1" || proj.Sessions[1].ID != "sess-3" || proj.Sessions[2].ID != "sess-2" {
		t.Errorf("order after MoveUp: got [%s,%s,%s] want [session-1,session-3,session-2]",
			proj.Sessions[0].Title, proj.Sessions[1].Title, proj.Sessions[2].Title)
	}

	// Cursor should follow session-3.
	sel := sidebarSelected(f)
	if sel == nil || sel.SessionID != "sess-3" {
		selID := ""
		if sel != nil {
			selID = sel.SessionID
		}
		t.Errorf("sidebar selection = %q, want %q", selID, "sess-3")
	}
}

// TestFlow_MoveSession_BoundaryNoop verifies that moving the first session up
// or last session down is a no-op.
func TestFlow_MoveSession_BoundaryNoop(t *testing.T) {
	m, mock := testFlowModelThreeSessions(t)
	f := newFlowRunner(t, m, mock)

	// Navigate to session-1 (first).
	for i := 0; i < 5 && f.Model().appState.ActiveSessionID != "sess-1"; i++ {
		f.SendSpecialKey(tea.KeyUp)
	}
	f.AssertActiveSession("sess-1")

	// Shift+Up on first session should be no-op.
	f.SendSpecialKey(tea.KeyShiftUp)
	proj := f.Model().appState.Projects[0]
	if proj.Sessions[0].ID != "sess-1" {
		t.Errorf("first session changed after MoveUp boundary: got %s", proj.Sessions[0].Title)
	}

	// Navigate to session-3 (last) and try Shift+Down.
	for i := 0; i < 5 && f.Model().appState.ActiveSessionID != "sess-3"; i++ {
		f.SendSpecialKey(tea.KeyDown)
	}
	f.SendSpecialKey(tea.KeyShiftDown)
	if proj.Sessions[2].ID != "sess-3" {
		t.Errorf("last session changed after MoveDown boundary: got %s", proj.Sessions[2].Title)
	}
}

// TestFlow_MoveProjectDown_Sidebar verifies Shift+Down on a project row
// swaps it with the next project.
func TestFlow_MoveProjectDown_Sidebar(t *testing.T) {
	m, mock := testFlowModel(t) // 2 projects: proj-1, proj-2
	f := newFlowRunner(t, m, mock)

	// Cursor starts on session-1 in proj-1. Move to the project row.
	// Sidebar items: [project-1, session-1, project-2, session-2]
	// Navigate up to the project-1 header.
	f.SendSpecialKey(tea.KeyUp)
	sel := sidebarSelected(f)
	if sel == nil || sel.Kind != components.KindProject || sel.ProjectID != "proj-1" {
		t.Fatalf("expected cursor on proj-1 project row, got kind=%v id=%v",
			func() components.ItemKind {
				if sel != nil {
					return sel.Kind
				}
				return -1
			}(),
			func() string {
				if sel != nil {
					return sel.ProjectID
				}
				return ""
			}())
	}

	// Press Shift+Down to move project-1 down.
	f.SendSpecialKey(tea.KeyShiftDown)

	// Verify project order swapped.
	projects := f.Model().appState.Projects
	if projects[0].ID != "proj-2" || projects[1].ID != "proj-1" {
		t.Errorf("project order after MoveDown: got [%s,%s] want [proj-2,proj-1]",
			projects[0].Name, projects[1].Name)
	}

	// Cursor should follow proj-1 (now second).
	sel = sidebarSelected(f)
	if sel == nil || sel.ProjectID != "proj-1" {
		selID := ""
		if sel != nil {
			selID = sel.ProjectID
		}
		t.Errorf("sidebar selection project = %q, want %q", selID, "proj-1")
	}
}

// TestFlow_MoveSession_GridView_ShiftRight verifies Shift+Right in grid view
// moves the selected session forward and grid cursor follows.
func TestFlow_MoveSession_GridView_ShiftRight(t *testing.T) {
	m, mock := testFlowModelThreeSessions(t)
	f := newFlowRunner(t, m, mock)

	// Navigate to session-1.
	for i := 0; i < 5 && f.Model().appState.ActiveSessionID != "sess-1"; i++ {
		f.SendSpecialKey(tea.KeyUp)
	}
	f.AssertActiveSession("sess-1")

	// Open project grid.
	f.SendKey("g")
	f.AssertGridActive(true)

	// Grid cursor should be on session-1.
	sel := gridSelected(f)
	if sel == nil || sel.ID != "sess-1" {
		t.Fatal("grid cursor should be on sess-1")
	}

	// Press Shift+Right to move session-1 forward (right).
	f.SendSpecialKey(tea.KeyShiftRight)

	// Verify state order changed.
	proj := f.Model().appState.Projects[0]
	if proj.Sessions[0].ID != "sess-2" || proj.Sessions[1].ID != "sess-1" {
		t.Errorf("grid: order after MoveRight: got [%s,%s,...] want [session-2,session-1,...]",
			proj.Sessions[0].Title, proj.Sessions[1].Title)
	}

	// Grid cursor should follow session-1.
	sel = gridSelected(f)
	if sel == nil || sel.ID != "sess-1" {
		selID := ""
		if sel != nil {
			selID = sel.ID
		}
		t.Errorf("grid cursor after move = %q, want %q", selID, "sess-1")
	}
}

// TestFlow_MoveSession_GridView_ShiftLeft verifies Shift+Left in grid view
// moves the selected session backward and grid cursor follows.
func TestFlow_MoveSession_GridView_ShiftLeft(t *testing.T) {
	m, mock := testFlowModelThreeSessions(t)
	f := newFlowRunner(t, m, mock)

	// Start on session-2 (the default active session in three-session model).
	f.AssertActiveSession("sess-2")

	// Open project grid.
	f.SendKey("g")
	f.AssertGridActive(true)

	sel := gridSelected(f)
	if sel == nil || sel.ID != "sess-2" {
		t.Fatal("grid cursor should be on sess-2")
	}

	// Press Shift+Left to move session-2 backward (left).
	f.SendSpecialKey(tea.KeyShiftLeft)

	// Verify state order changed: session-2 should now be first.
	proj := f.Model().appState.Projects[0]
	if proj.Sessions[0].ID != "sess-2" || proj.Sessions[1].ID != "sess-1" {
		t.Errorf("grid: order after MoveLeft: got [%s,%s,...] want [session-2,session-1,...]",
			proj.Sessions[0].Title, proj.Sessions[1].Title)
	}

	// Grid cursor should follow session-2.
	sel = gridSelected(f)
	if sel == nil || sel.ID != "sess-2" {
		selID := ""
		if sel != nil {
			selID = sel.ID
		}
		t.Errorf("grid cursor after move = %q, want %q", selID, "sess-2")
	}
}

// TestFlow_MoveTeamDown_Sidebar verifies Shift+Down on a team row swaps it
// with the next team.
func TestFlow_MoveTeamDown_Sidebar(t *testing.T) {
	m, mock := testFlowModelWithTeams(t)
	f := newFlowRunner(t, m, mock)

	// Navigate to team-1 row.
	// Sidebar: [project, team-1, team-1-sess, team-2, team-2-sess, standalone]
	// Start on standalone (active session). Navigate up to team-1.
	for i := 0; i < 10; i++ {
		sel := sidebarSelected(f)
		if sel != nil && sel.Kind == components.KindTeam && sel.TeamID == "team-1" {
			break
		}
		f.SendSpecialKey(tea.KeyUp)
	}
	sel := sidebarSelected(f)
	if sel == nil || sel.Kind != components.KindTeam || sel.TeamID != "team-1" {
		t.Fatalf("expected cursor on team-1, got kind=%v id=%v",
			func() components.ItemKind {
				if sel != nil {
					return sel.Kind
				}
				return -1
			}(),
			func() string {
				if sel != nil {
					return sel.TeamID
				}
				return ""
			}())
	}

	// Press Shift+Down.
	f.SendSpecialKey(tea.KeyShiftDown)

	// Verify team order swapped.
	proj := f.Model().appState.Projects[0]
	if proj.Teams[0].ID != "team-2" || proj.Teams[1].ID != "team-1" {
		t.Errorf("team order after MoveDown: got [%s,%s] want [team-2,team-1]",
			proj.Teams[0].Name, proj.Teams[1].Name)
	}

	// Cursor should follow team-1.
	sel = sidebarSelected(f)
	if sel == nil || sel.TeamID != "team-1" {
		selID := ""
		if sel != nil {
			selID = sel.TeamID
		}
		t.Errorf("sidebar selection team = %q, want %q", selID, "team-1")
	}
}

// TestFlow_NavProjectDown_JumpsToNextProject verifies that pressing "J"
// (NavProjectDown) moves the sidebar cursor to the next project header.
func TestFlow_NavProjectDown_JumpsToNextProject(t *testing.T) {
	m, mock := testFlowModel(t) // 2 projects: proj-1, proj-2
	f := newFlowRunner(t, m, mock)

	// Cursor starts on sess-1 in proj-1. Move up to the project row.
	f.SendSpecialKey(tea.KeyUp)
	sel := sidebarSelected(f)
	if sel == nil || sel.Kind != components.KindProject || sel.ProjectID != "proj-1" {
		t.Fatalf("setup: expected cursor on proj-1, got kind=%v id=%v",
			sel.Kind, sel.ProjectID)
	}

	// Press "J" (NavProjectDown) to jump to proj-2.
	f.SendKey("J")
	sel = sidebarSelected(f)
	if sel == nil || sel.Kind != components.KindProject {
		t.Fatalf("after NavProjectDown: expected KindProject, got %v", sel)
	}
	if sel.ProjectID != "proj-2" {
		t.Errorf("after NavProjectDown: projectID = %q, want %q", sel.ProjectID, "proj-2")
	}
}

// TestFlow_NavProjectUp_JumpsToPrevProject verifies that pressing "K"
// (NavProjectUp) moves the sidebar cursor to the previous project header.
func TestFlow_NavProjectUp_JumpsToPrevProject(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Navigate to proj-2 first: up to proj-1, then J to proj-2.
	f.SendSpecialKey(tea.KeyUp)
	f.SendKey("J")
	sel := sidebarSelected(f)
	if sel == nil || sel.ProjectID != "proj-2" {
		t.Fatalf("setup: expected cursor on proj-2, got %v", sel)
	}

	// Press "K" (NavProjectUp) to jump back to proj-1.
	f.SendKey("K")
	sel = sidebarSelected(f)
	if sel == nil || sel.Kind != components.KindProject {
		t.Fatalf("after NavProjectUp: expected KindProject, got %v", sel)
	}
	if sel.ProjectID != "proj-1" {
		t.Errorf("after NavProjectUp: projectID = %q, want %q", sel.ProjectID, "proj-1")
	}
}

// TestFlow_NavProjectDown_NoOp_AtLastProject verifies that "J" at the last
// project is a no-op (cursor stays put).
func TestFlow_NavProjectDown_NoOp_AtLastProject(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Navigate to proj-2 header.
	f.SendSpecialKey(tea.KeyUp)
	f.SendKey("J")
	sel := sidebarSelected(f)
	if sel == nil || sel.ProjectID != "proj-2" {
		t.Fatalf("setup: expected cursor on proj-2, got %v", sel)
	}

	// Press "J" again — should stay on proj-2.
	f.SendKey("J")
	sel = sidebarSelected(f)
	if sel == nil || sel.ProjectID != "proj-2" {
		t.Errorf("NavProjectDown at last project: projectID = %q, want %q", sel.ProjectID, "proj-2")
	}
}

func TestFlow_NavProjectUp_NoOp_AtFirstProject(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Navigate to proj-1 header.
	f.SendSpecialKey(tea.KeyUp)
	sel := sidebarSelected(f)
	if sel == nil || sel.ProjectID != "proj-1" {
		t.Fatalf("setup: expected cursor on proj-1, got %v", sel)
	}

	// Press "K" — should stay on proj-1.
	f.SendKey("K")
	sel = sidebarSelected(f)
	if sel == nil || sel.ProjectID != "proj-1" {
		t.Errorf("NavProjectUp at first project: projectID = %q, want %q", sel.ProjectID, "proj-1")
	}
}

func testFlowModelWithTeams(t *testing.T) (Model, *muxtest.MockBackend) {
	t.Helper()
	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)
	t.Setenv("TERM", "dumb")

	mock := muxtest.New()
	mux.SetBackend(mock)
	t.Cleanup(func() { mux.SetBackend(nil) })

	mock.SetPaneContent("hive-proj:0", "$ claude\nTeam 1 session.")
	mock.SetPaneContent("hive-proj:1", "$ claude\nTeam 2 session.")
	mock.SetPaneContent("hive-proj:2", "$ claude\nStandalone.")

	cfg := config.DefaultConfig()
	cfg.HideAttachHint = true
	cfg.PreviewRefreshMs = 1

	appState := state.AppState{
		ActiveProjectID: "proj-1",
		ActiveSessionID: "sess-standalone",
		Projects: []*state.Project{
			{
				ID:    "proj-1",
				Name:  "test-project",
				Color: "#7C3AED",
				Teams: []*state.Team{
					{
						ID:        "team-1",
						ProjectID: "proj-1",
						Name:      "team-1",
						Sessions: []*state.Session{
							{
								ID:          "sess-t1",
								ProjectID:   "proj-1",
								TeamID:      "team-1",
								Title:       "team-1-sess",
								TmuxSession: "hive-proj",
								TmuxWindow:  0,
								Status:      state.StatusRunning,
								AgentType:   state.AgentClaude,
								AgentCmd:    []string{"claude"},
								TeamRole:    state.RoleWorker,
							},
						},
					},
					{
						ID:        "team-2",
						ProjectID: "proj-1",
						Name:      "team-2",
						Sessions: []*state.Session{
							{
								ID:          "sess-t2",
								ProjectID:   "proj-1",
								TeamID:      "team-2",
								Title:       "team-2-sess",
								TmuxSession: "hive-proj",
								TmuxWindow:  1,
								Status:      state.StatusRunning,
								AgentType:   state.AgentClaude,
								AgentCmd:    []string{"claude"},
								TeamRole:    state.RoleWorker,
							},
						},
					},
				},
				Sessions: []*state.Session{
					{
						ID:          "sess-standalone",
						ProjectID:   "proj-1",
						Title:       "standalone",
						TmuxSession: "hive-proj",
						TmuxWindow:  2,
						Status:      state.StatusRunning,
						AgentType:   state.AgentClaude,
						AgentCmd:    []string{"claude"},
						TeamRole:    state.RoleStandalone,
					},
				},
			},
		},
		AgentUsage: make(map[string]state.AgentUsageRecord),
		TermWidth:  120,
		TermHeight: 40,
	}

	m := New(cfg, appState, "", "")
	m.appState.TermWidth = 120
	m.appState.TermHeight = 40
	return m, mock
}
