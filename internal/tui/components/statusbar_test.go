package components

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/styles"
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
		hints string
	}{
		{"normal-80", 80, &state.AppState{}, "?:help  n:new  t:session  q:quit"},
		{"narrow-40", 40, &state.AppState{}, "?:help  q:quit"},
		{"wide-200", 200, &state.AppState{}, "?:help  n:new  t:session  q:quit"},
		{"with-project-session", 80, &state.AppState{
			Projects:        []*state.Project{project},
			ActiveProjectID: "p1",
			ActiveSessionID: "s1",
		}, "?:help  n:new  t:session  q:quit"},
		{"with-error", 80, &state.AppState{LastError: "something went wrong with a very long error message that might cause wrapping"}, "?:help  q:quit"},
		{"with-installing", 80, &state.AppState{InstallingAgent: "claude-desktop"}, "?:help  q:quit"},
		{"filter-active", 80, &state.AppState{}, "Filter: query_  [esc: clear]"},
		{"show-confirm", 80, &state.AppState{ShowConfirm: true}, "y/enter: confirm  esc/n: cancel"},
		{"minimal-width", 20, &state.AppState{}, "?:help"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sb := &StatusBar{Width: tc.width}
			out := sb.View(tc.as, tc.hints)
			got := strings.Count(out, "\n") + 1
			if got != 2 {
				t.Errorf("StatusBar.View() = %d lines, want exactly 2 (width=%d)", got, tc.width)
			}
		})
	}
}

func TestStatusBarView_ShowsPassedHints(t *testing.T) {
	sb := &StatusBar{Width: 200}
	appState := &state.AppState{}

	hints := "?:help  n:new project  t:new session  q:quit"
	out := sb.View(appState, hints)
	if !strings.Contains(out, hints) {
		t.Fatalf("status bar does not contain passed hints %q in output: %q", hints, out)
	}
}

func TestStatusBarView_ShowsStatusLegend(t *testing.T) {
	sb := &StatusBar{Width: 200}
	appState := &state.AppState{}

	// The caller (app.go) is responsible for appending the status legend to the hints.
	legend := styles.StatusLegend()
	hints := "?:help  " + legend
	out := sb.View(appState, hints)
	for _, want := range []string{"idle", "working", "waiting", "dead"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status bar legend missing %q in output: %q", want, out)
		}
	}
}
