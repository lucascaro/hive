package components

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/state"
)

func TestGridViewView_ShowsStatusLegend(t *testing.T) {
	gv := &GridView{
		Active: true,
		Width:  100,
		Height: 30,
	}
	gv.Show([]*state.Session{
		{ID: "s1", Title: "alpha", AgentType: state.AgentClaude, Status: state.StatusRunning},
	})

	out := gv.View()
	for _, want := range []string{"idle", "working", "waiting", "dead"} {
		if !strings.Contains(out, want) {
			t.Fatalf("grid legend missing %q in output: %q", want, out)
		}
	}
}

// TestGridView_ExactHeight ensures the grid never produces more or fewer lines
// than gv.Height regardless of integer-division remainders in cellH.
func TestGridView_ExactHeight(t *testing.T) {
	sessions := []*state.Session{
		{ID: "s1", Title: "alpha", AgentType: state.AgentClaude, Status: state.StatusRunning},
		{ID: "s2", Title: "beta", AgentType: state.AgentClaude, Status: state.StatusIdle},
		{ID: "s3", Title: "gamma", AgentType: state.AgentClaude, Status: state.StatusWaiting},
		{ID: "s4", Title: "delta", AgentType: state.AgentClaude, Status: state.StatusDead},
	}
	dims := []struct{ w, h int }{
		{80, 24}, {80, 25}, {80, 30}, {80, 31},
		{160, 40}, {160, 41}, {160, 50}, {160, 51},
		{214, 60}, {214, 61}, {214, 62},
	}
	for _, d := range dims {
		gv := &GridView{Active: true, Width: d.w, Height: d.h}
		gv.Show(sessions)
		out := gv.View()
		got := strings.Count(out, "\n") + 1
		if got != d.h {
			t.Errorf("w=%d h=%d: View() = %d lines, want exactly %d",
				d.w, d.h, got, d.h)
		}
	}
}

// TestGridView_ExactHeight_VariousCounts checks the invariant for 1–9 sessions.
func TestGridView_ExactHeight_VariousCounts(t *testing.T) {
	allSessions := make([]*state.Session, 9)
	for i := range allSessions {
		allSessions[i] = &state.Session{
			ID: "s" + string(rune('1'+i)), Title: "session",
			AgentType: state.AgentClaude, Status: state.StatusRunning,
		}
	}
	for n := 1; n <= 9; n++ {
		for _, h := range []int{24, 30, 40, 50, 62} {
			gv := &GridView{Active: true, Width: 160, Height: h}
			gv.Show(allSessions[:n])
			out := gv.View()
			got := strings.Count(out, "\n") + 1
			if got != h {
				t.Errorf("n=%d h=%d: View() = %d lines, want exactly %d",
					n, h, got, h)
			}
		}
	}
}

