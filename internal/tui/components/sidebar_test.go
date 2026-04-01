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
