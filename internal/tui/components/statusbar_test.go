package components

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/state"
)

func TestStatusBarView_ExactHeight(t *testing.T) {
	project := &state.Project{
		ID:   "p1",
		Name: "my-project",
		Sessions: []*state.Session{
			{ID: "s1", Title: "my-session", AgentType: state.AgentClaude, Status: state.StatusRunning},
		},
		Teams: []*state.Team{},
	}

	cases := []struct {
		name  string
		width int
		as    *state.AppState
	}{
		{"normal-80", 80, &state.AppState{}},
		{"narrow-40", 40, &state.AppState{}},
		{"wide-200", 200, &state.AppState{}},
		{"with-project-session", 80, &state.AppState{
			Projects:        []*state.Project{project},
			ActiveProjectID: "p1",
			ActiveSessionID: "s1",
		}},
		{"with-error", 80, &state.AppState{LastError: "something went wrong with a very long error message that might cause wrapping"}},
		{"with-installing", 80, &state.AppState{InstallingAgent: "claude-desktop"}},
		{"filter-active", 80, &state.AppState{}},
		{"show-confirm", 80, &state.AppState{ShowConfirm: true}},
		{"minimal-width", 20, &state.AppState{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sb := &StatusBar{Width: tc.width}
			filterActive := tc.name == "filter-active"
			out := sb.View(tc.as, state.PaneSidebar, filterActive, "query")
			got := strings.Count(out, "\n") + 1
			if got != 2 {
				t.Errorf("StatusBar.View() = %d lines, want exactly 2 (width=%d)", got, tc.width)
			}
		})
	}
}

func TestStatusBarView_ShowsStatusLegend(t *testing.T) {
	sb := &StatusBar{Width: 200}
	appState := &state.AppState{}

	out := sb.View(appState, state.PaneSidebar, false, "")
	for _, want := range []string{"idle", "working", "waiting", "dead"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status bar legend missing %q in output: %q", want, out)
		}
	}
}
