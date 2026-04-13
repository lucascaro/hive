package tui

import (
	"testing"

	"github.com/lucascaro/hive/internal/state"
)

// TestUnit_GridSessionsFiltersByActiveProject pins down gridSessions' filter
// behavior independently of the key handler. The fix for #80 relies on the
// handler mutating ActiveProjectID before calling gridSessions, so this test
// asserts the filter actually follows that field.
func TestUnit_GridSessionsFiltersByActiveProject(t *testing.T) {
	m, _ := testFlowModel(t)

	m.appState.ActiveProjectID = "proj-1"
	got := m.gridSessions(state.GridRestoreProject)
	if len(got) != 1 || got[0].ProjectID != "proj-1" {
		t.Errorf("proj-1 filter: got %+v, want one session from proj-1", got)
	}

	m.appState.ActiveProjectID = "proj-2"
	got = m.gridSessions(state.GridRestoreProject)
	if len(got) != 1 || got[0].ProjectID != "proj-2" {
		t.Errorf("proj-2 filter: got %+v, want one session from proj-2", got)
	}

	// GridRestoreAll must return every live session regardless of ActiveProjectID.
	m.appState.ActiveProjectID = "proj-1"
	all := m.gridSessions(state.GridRestoreAll)
	if len(all) != 2 {
		t.Errorf("GridRestoreAll: got %d sessions, want 2", len(all))
	}
}
