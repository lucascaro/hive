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
