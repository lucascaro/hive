package components

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/state"
)

func TestSidebarView_ExactHeight(t *testing.T) {
	makeSessions := func(n int) []*state.Session {
		sessions := make([]*state.Session, n)
		for i := range sessions {
			sessions[i] = &state.Session{
				ID:        strings.Repeat("s", i+1),
				ProjectID: "proj-1",
				Title:     "sess",
				AgentType: state.AgentClaude,
				Status:    state.StatusIdle,
			}
		}
		return sessions
	}

	cases := []struct {
		name     string
		width    int
		height   int
		sessions int
	}{
		{"no-sessions", 40, 20, 0},
		{"few-sessions", 40, 20, 3},
		{"many-sessions-overflow", 40, 10, 30},
		{"wide-tall", 60, 40, 5},
		{"exact-fill", 40, 15, 12},
		{"minimal", 10, 5, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			appState := &state.AppState{
				Projects: []*state.Project{
					{
						ID:       "proj-1",
						Name:     "my-project",
						Sessions: makeSessions(tc.sessions),
						Teams:    []*state.Team{},
					},
				},
			}
			s := &Sidebar{Width: tc.width, Height: tc.height}
			s.Rebuild(appState)
			out := s.View("", false)
			got := strings.Count(out, "\n") + 1
			if got != tc.height {
				t.Errorf("Sidebar.View() = %d lines, want exactly %d (w=%d h=%d sessions=%d)",
					got, tc.height, tc.width, tc.height, tc.sessions)
			}
		})
	}
}

func testAppState() *state.AppState {
	return &state.AppState{
		Projects: []*state.Project{
			{
				ID:   "proj-1",
				Name: "my-project",
				Teams: []*state.Team{
					{
						ID:        "team-1",
						ProjectID: "proj-1",
						Name:      "feature-team",
						Sessions: []*state.Session{
							{ID: "s1", ProjectID: "proj-1", TeamID: "team-1", Title: "orchestrator", AgentType: state.AgentClaude, Status: state.StatusRunning, TeamRole: state.RoleOrchestrator},
							{ID: "s2", ProjectID: "proj-1", TeamID: "team-1", Title: "worker-1", AgentType: state.AgentCodex, Status: state.StatusIdle, TeamRole: state.RoleWorker},
						},
					},
				},
				Sessions: []*state.Session{
					{ID: "s3", ProjectID: "proj-1", Title: "solo-session", AgentType: state.AgentGemini, Status: state.StatusRunning},
					{ID: "s4", ProjectID: "proj-1", Title: "copilot-test", AgentType: state.AgentCopilot, Status: state.StatusIdle},
				},
			},
		},
	}
}

func TestSidebarRebuild_NoFilter(t *testing.T) {
	appState := testAppState()
	s := &Sidebar{}
	s.Rebuild(appState)

	// Should have: project + team + 2 team sessions + 2 standalone sessions = 6
	if len(s.Items) != 6 {
		for i, item := range s.Items {
			t.Logf("  [%d] kind=%d label=%q session=%q agent=%s", i, item.Kind, item.Label, item.SessionID, item.AgentType)
		}
		t.Fatalf("got %d items, want 6", len(s.Items))
	}
}

func TestSidebarRebuild_FilterByTitle(t *testing.T) {
	appState := testAppState()
	s := &Sidebar{FilterQuery: "solo"}
	s.Rebuild(appState)

	// "solo" matches session title "solo-session"
	// Project should still show (it has a matching child)
	sessionCount := 0
	for _, item := range s.Items {
		if item.Kind == KindSession {
			sessionCount++
			if item.Label != "solo-session" {
				t.Errorf("unexpected session: %q", item.Label)
			}
		}
	}
	if sessionCount != 1 {
		t.Fatalf("filter 'solo': got %d sessions, want 1", sessionCount)
	}
}

func TestSidebarRebuild_FilterByAgentType(t *testing.T) {
	appState := testAppState()
	s := &Sidebar{FilterQuery: "codex"}
	s.Rebuild(appState)

	// "codex" should match session with AgentType=codex (worker-1)
	sessionCount := 0
	for _, item := range s.Items {
		if item.Kind == KindSession {
			sessionCount++
			if item.AgentType != string(state.AgentCodex) {
				t.Errorf("unexpected session agent: %q (title=%q)", item.AgentType, item.Label)
			}
		}
	}
	if sessionCount != 1 {
		t.Fatalf("filter 'codex': got %d sessions, want 1", sessionCount)
	}
}

func TestSidebarRebuild_FilterByAgentTypeSubstring(t *testing.T) {
	tests := []struct {
		query       string
		wantAgents  []string
		description string
	}{
		{"co", []string{"codex", "copilot"}, "co matches codex and copilot"},
		{"dex", []string{"codex"}, "dex matches codex"},
		{"ode", []string{"codex"}, "ode matches codex (c-ode-x substring)"},
		{"gem", []string{"gemini"}, "gem matches gemini"},
		{"claude", []string{"claude"}, "claude matches claude"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			appState := testAppState()
			s := &Sidebar{FilterQuery: tt.query}
			s.Rebuild(appState)

			var gotAgents []string
			for _, item := range s.Items {
				if item.Kind == KindSession {
					gotAgents = append(gotAgents, item.AgentType)
				}
			}
			if len(gotAgents) != len(tt.wantAgents) {
				t.Fatalf("filter %q: got agents %v, want %v", tt.query, gotAgents, tt.wantAgents)
			}
			for i, want := range tt.wantAgents {
				if gotAgents[i] != want {
					t.Errorf("filter %q [%d]: got %s, want %s", tt.query, i, gotAgents[i], want)
				}
			}
		})
	}
}

func TestSidebarRebuild_FilterShowsParents(t *testing.T) {
	appState := testAppState()
	s := &Sidebar{FilterQuery: "codex"}
	s.Rebuild(appState)

	// Should show project + team (parents of the matching codex session) + the session
	hasProject := false
	hasTeam := false
	for _, item := range s.Items {
		switch item.Kind {
		case KindProject:
			hasProject = true
		case KindTeam:
			hasTeam = true
		}
	}
	if !hasProject {
		t.Error("filter should include parent project")
	}
	if !hasTeam {
		t.Error("filter should include parent team")
	}
}

func TestSidebarRebuild_CollapsedProject(t *testing.T) {
	appState := testAppState()
	appState.Projects[0].Collapsed = true
	s := &Sidebar{}
	s.Rebuild(appState)

	// Should only have the project item (collapsed)
	if len(s.Items) != 1 {
		t.Fatalf("collapsed project: got %d items, want 1", len(s.Items))
	}
	if s.Items[0].Kind != KindProject {
		t.Fatalf("collapsed project: item kind=%d, want KindProject", s.Items[0].Kind)
	}
}

func TestSidebarRebuild_CollapsedTeam(t *testing.T) {
	appState := testAppState()
	appState.Projects[0].Teams[0].Collapsed = true
	s := &Sidebar{}
	s.Rebuild(appState)

	// Project + collapsed team (no team sessions) + 2 standalone = 4
	if len(s.Items) != 4 {
		for i, item := range s.Items {
			t.Logf("  [%d] kind=%d label=%q", i, item.Kind, item.Label)
		}
		t.Fatalf("collapsed team: got %d items, want 4", len(s.Items))
	}
}

func TestSidebarRebuild_NoMatchFilter(t *testing.T) {
	appState := testAppState()
	s := &Sidebar{FilterQuery: "zzz_no_match"}
	s.Rebuild(appState)

	if len(s.Items) != 0 {
		t.Fatalf("no-match filter: got %d items, want 0", len(s.Items))
	}
}

func TestSidebarSelected(t *testing.T) {
	appState := testAppState()
	s := &Sidebar{}
	s.Rebuild(appState)

	// Cursor at 0 should be the project
	sel := s.Selected()
	if sel == nil {
		t.Fatal("Selected() returned nil")
	}
	if sel.Kind != KindProject {
		t.Fatalf("Selected() kind=%d, want KindProject", sel.Kind)
	}
}

func TestSidebarMoveDown(t *testing.T) {
	appState := testAppState()
	s := &Sidebar{}
	s.Rebuild(appState)

	s.MoveDown()
	sel := s.Selected()
	if sel == nil || sel.Kind != KindTeam {
		t.Fatalf("after MoveDown: kind=%v, want KindTeam", sel)
	}

	s.MoveDown()
	sel = s.Selected()
	if sel == nil || sel.Kind != KindSession {
		t.Fatalf("after 2x MoveDown: kind=%v, want KindSession", sel)
	}
}

func TestSidebarMoveUp(t *testing.T) {
	appState := testAppState()
	s := &Sidebar{}
	s.Rebuild(appState)

	// Move to a known position first.
	s.MoveDown() // → team
	s.MoveDown() // → team session (s1)
	if sel := s.Selected(); sel == nil || sel.SessionID != "s1" {
		t.Fatalf("setup: expected cursor on s1, got %v", s.Selected())
	}

	s.MoveUp()
	sel := s.Selected()
	if sel == nil || sel.Kind != KindTeam {
		t.Fatalf("after MoveUp from s1: kind=%v, want KindTeam", sel)
	}

	// MoveUp at top should be a no-op.
	s.Cursor = 0
	s.MoveUp()
	if s.Cursor != 0 {
		t.Fatalf("MoveUp at top: cursor=%d, want 0", s.Cursor)
	}
}

func TestSidebarJumpPrevProject(t *testing.T) {
	appState := &state.AppState{
		Projects: []*state.Project{
			{
				ID: "p1", Name: "alpha", Teams: []*state.Team{},
				Sessions: []*state.Session{
					{ID: "s1", ProjectID: "p1", Title: "a1", AgentType: state.AgentClaude, Status: state.StatusRunning},
				},
			},
			{
				ID: "p2", Name: "beta", Teams: []*state.Team{},
				Sessions: []*state.Session{
					{ID: "s2", ProjectID: "p2", Title: "b1", AgentType: state.AgentCodex, Status: state.StatusIdle},
				},
			},
		},
	}
	s := &Sidebar{}
	s.Rebuild(appState)

	// Items: [p1(0), s1(1), p2(2), s2(3)]
	// Start on s2 (index 3), jump prev should land on p2 (index 2).
	s.Cursor = 3
	s.JumpPrevProject()
	if s.Cursor != 2 {
		t.Fatalf("JumpPrevProject from s2: cursor=%d, want 2 (p2)", s.Cursor)
	}

	// Jump prev again should land on p1 (index 0).
	s.JumpPrevProject()
	if s.Cursor != 0 {
		t.Fatalf("JumpPrevProject from p2: cursor=%d, want 0 (p1)", s.Cursor)
	}

	// At p1 (first project), jump prev should be a no-op.
	s.JumpPrevProject()
	if s.Cursor != 0 {
		t.Fatalf("JumpPrevProject at first project: cursor=%d, want 0", s.Cursor)
	}
}

func TestSidebarJumpNextProject(t *testing.T) {
	appState := &state.AppState{
		Projects: []*state.Project{
			{
				ID: "p1", Name: "alpha", Teams: []*state.Team{},
				Sessions: []*state.Session{
					{ID: "s1", ProjectID: "p1", Title: "a1", AgentType: state.AgentClaude, Status: state.StatusRunning},
				},
			},
			{
				ID: "p2", Name: "beta", Teams: []*state.Team{},
				Sessions: []*state.Session{
					{ID: "s2", ProjectID: "p2", Title: "b1", AgentType: state.AgentCodex, Status: state.StatusIdle},
				},
			},
		},
	}
	s := &Sidebar{}
	s.Rebuild(appState)

	// Items: [p1(0), s1(1), p2(2), s2(3)]
	// Start on p1 (index 0), jump next should land on p2 (index 2).
	s.Cursor = 0
	s.JumpNextProject()
	if s.Cursor != 2 {
		t.Fatalf("JumpNextProject from p1: cursor=%d, want 2 (p2)", s.Cursor)
	}

	// At p2 (last project), jump next should be a no-op.
	s.JumpNextProject()
	if s.Cursor != 2 {
		t.Fatalf("JumpNextProject at last project: cursor=%d, want 2", s.Cursor)
	}
}

func TestSidebar_WorktreeBadge(t *testing.T) {
	s := &Sidebar{Width: 80}

	// title == branch → badge only, no duplicate branch name
	sameBranch := SidebarItem{
		Kind: KindSession, Label: "my-feature", AgentType: "claude",
		IsWorktree: true, WorktreeBranch: "my-feature",
	}
	out := s.renderItem(sameBranch, false, false, 80)
	if !strings.Contains(out, "⎇") {
		t.Error("expected ⎇ badge for worktree session with matching branch")
	}
	// The branch name should appear exactly once (in the label), not again after ⎇
	if strings.Contains(out, "⎇ my-feature") {
		t.Error("branch name should be suppressed when it matches the session title")
	}

	// title != branch → badge + branch name
	diffBranch := SidebarItem{
		Kind: KindSession, Label: "backend", AgentType: "claude",
		IsWorktree: true, WorktreeBranch: "feat/backend-refactor",
	}
	out = s.renderItem(diffBranch, false, false, 80)
	if !strings.Contains(out, "⎇ feat/backend-refactor") {
		t.Error("expected branch name 'feat/backend-refactor' when it differs from title")
	}

	// non-worktree → no badge
	noWorktree := SidebarItem{
		Kind: KindSession, Label: "plain", AgentType: "claude",
	}
	out = s.renderItem(noWorktree, false, false, 80)
	if strings.Contains(out, "⎇") {
		t.Error("expected no ⎇ badge for non-worktree session")
	}
}

func TestSidebarRebuild_SyncsOnClamp(t *testing.T) {
	appState := testAppState()
	appState.ActiveSessionID = "s3" // solo-session

	// Cursor starts out of bounds — Rebuild should clamp and sync to active session.
	s := &Sidebar{Cursor: 999}
	s.Rebuild(appState)

	sel := s.Selected()
	if sel == nil {
		t.Fatal("Selected() returned nil")
	}
	if sel.SessionID != "s3" {
		t.Errorf("cursor should sync to active session s3 after clamp, got %q", sel.SessionID)
	}
}

func TestSidebarRebuild_NoSyncWhenInBounds(t *testing.T) {
	appState := testAppState()
	appState.ActiveSessionID = "s3" // solo-session is at index 4

	// Cursor starts at 0 (project row), in bounds — Rebuild should NOT move it.
	s := &Sidebar{Cursor: 0}
	s.Rebuild(appState)

	if s.Cursor != 0 {
		t.Errorf("cursor should stay at 0 (project row) during in-bounds rebuild, got %d", s.Cursor)
	}
}

func TestSidebarRebuild_ClampsWhenActiveNotFound(t *testing.T) {
	appState := testAppState()
	appState.ActiveSessionID = "nonexistent"

	s := &Sidebar{Cursor: 999}
	s.Rebuild(appState)

	// Should clamp to valid range, not crash
	if s.Cursor < 0 || s.Cursor >= len(s.Items) {
		t.Errorf("cursor %d out of range [0, %d)", s.Cursor, len(s.Items))
	}
}

func TestSyncActiveSession_ValidID(t *testing.T) {
	appState := testAppState()
	s := &Sidebar{}
	s.Rebuild(appState)

	s.SyncActiveSession("s4")
	sel := s.Selected()
	if sel == nil || sel.SessionID != "s4" {
		t.Errorf("SyncActiveSession(s4): got %v", sel)
	}
}

func TestSyncActiveSession_InvalidID(t *testing.T) {
	appState := testAppState()
	s := &Sidebar{}
	s.Rebuild(appState)
	origCursor := s.Cursor

	s.SyncActiveSession("nonexistent")
	if s.Cursor != origCursor {
		t.Errorf("SyncActiveSession with invalid ID changed cursor from %d to %d", origCursor, s.Cursor)
	}
}

func TestSidebarRebuild_ProjectColorPropagated(t *testing.T) {
	appState := &state.AppState{
		Projects: []*state.Project{
			{
				ID:    "proj-1",
				Name:  "colored-project",
				Color: "#EF4444",
				Teams: []*state.Team{
					{
						ID:        "team-1",
						ProjectID: "proj-1",
						Name:      "red-team",
						Sessions: []*state.Session{
							{ID: "s1", ProjectID: "proj-1", TeamID: "team-1", Title: "worker", AgentType: state.AgentClaude, Status: state.StatusRunning},
						},
					},
				},
				Sessions: []*state.Session{
					{ID: "s2", ProjectID: "proj-1", Title: "standalone", AgentType: state.AgentCodex, Status: state.StatusIdle},
				},
			},
		},
	}
	s := &Sidebar{}
	s.Rebuild(appState)

	for _, item := range s.Items {
		if item.ProjectColor != "#EF4444" {
			t.Errorf("item %q (kind=%d) has ProjectColor=%q, want #EF4444", item.Label, item.Kind, item.ProjectColor)
		}
	}
}

func TestSidebarRebuild_MultipleProjectColors(t *testing.T) {
	appState := &state.AppState{
		Projects: []*state.Project{
			{
				ID: "p1", Name: "alpha", Color: "#FF0000",
				Teams: []*state.Team{},
				Sessions: []*state.Session{
					{ID: "s1", ProjectID: "p1", Title: "s1", AgentType: state.AgentClaude, Status: state.StatusRunning},
				},
			},
			{
				ID: "p2", Name: "beta", Color: "#00FF00",
				Teams: []*state.Team{},
				Sessions: []*state.Session{
					{ID: "s2", ProjectID: "p2", Title: "s2", AgentType: state.AgentCodex, Status: state.StatusIdle},
				},
			},
		},
	}
	s := &Sidebar{}
	s.Rebuild(appState)

	for _, item := range s.Items {
		var wantColor string
		switch item.ProjectID {
		case "p1":
			wantColor = "#FF0000"
		case "p2":
			wantColor = "#00FF00"
		}
		if item.ProjectColor != wantColor {
			t.Errorf("item %q (project=%s) has ProjectColor=%q, want %q",
				item.Label, item.ProjectID, item.ProjectColor, wantColor)
		}
	}
}

func TestSidebarRenderItem_SessionHasColorBar(t *testing.T) {
	s := &Sidebar{Width: 60}
	item := SidebarItem{
		Kind:         KindSession,
		Label:        "test-session",
		AgentType:    "claude",
		Status:       "running",
		Indent:       1,
		ProjectColor: "#EF4444",
	}
	out := s.renderItem(item, false, false, 60)
	if out == "" {
		t.Fatal("renderItem returned empty string")
	}
	// The session should contain the session title.
	if !strings.Contains(out, "test-session") {
		t.Error("renderItem session output missing session title")
	}
}

func TestSidebarRenderItem_ProjectUsesColor(t *testing.T) {
	s := &Sidebar{Width: 60}
	item := SidebarItem{
		Kind:         KindProject,
		Label:        "my-project",
		ProjectNum:   1,
		ProjectColor: "#3B82F6",
	}
	out := s.renderItem(item, false, false, 60)
	if !strings.Contains(out, "my-project") {
		t.Error("renderItem project output missing project name")
	}
}

func TestSidebarRenderItem_TeamHasColorBar(t *testing.T) {
	s := &Sidebar{Width: 60}
	item := SidebarItem{
		Kind:         KindTeam,
		Label:        "my-team",
		Indent:       1,
		Status:       "running",
		ProjectColor: "#10B981",
	}
	out := s.renderItem(item, false, false, 60)
	if !strings.Contains(out, "my-team") {
		t.Error("renderItem team output missing team name")
	}
}

func TestSidebarRenderItem_EmptyColorFallback(t *testing.T) {
	s := &Sidebar{Width: 60}
	item := SidebarItem{
		Kind:         KindSession,
		Label:        "test",
		AgentType:    "claude",
		Status:       "idle",
		Indent:       1,
		ProjectColor: "", // empty — should not panic
	}
	out := s.renderItem(item, false, false, 60)
	if out == "" {
		t.Error("renderItem with empty ProjectColor returned empty string")
	}
}

func TestMatchesSessionFilter(t *testing.T) {
	sess := &state.Session{
		Title:     "my-worker",
		AgentType: state.AgentCodex,
	}

	tests := []struct {
		query string
		want  bool
	}{
		{"worker", true},    // title match
		{"codex", true},     // agent type match
		{"dex", true},       // substring of agent type
		{"my", true},        // prefix of title
		{"CODEX", true},     // case insensitive
		{"zzz", false},      // no match
		{"claude", false},   // wrong agent
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := matchesSessionFilter(sess, tt.query)
			if got != tt.want {
				t.Errorf("matchesSessionFilter(%q, %q) = %v, want %v", sess.Title, tt.query, got, tt.want)
			}
		})
	}
}
