package tui

import (
	"testing"

	"github.com/charmbracelet/x/exp/golden"
	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/state"
)

// Golden snapshot tests capture View() output for key static UI states.
// These catch rendering regressions in layout, styling, and content.
//
// To update golden files after intentional UI changes:
//   go test ./internal/tui/ -run TestGolden -update
//
// Golden files live in testdata/ and should be committed to git.
// PR diffs show exactly what changed visually.

func goldenModel(t *testing.T, appState state.AppState) Model {
	t.Helper()
	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)
	t.Setenv("TERM", "dumb")

	cfg := config.DefaultConfig()
	cfg.HideAttachHint = true
	m := New(cfg, appState)
	m.appState.TermWidth = appState.TermWidth
	m.appState.TermHeight = appState.TermHeight
	return m
}

func TestGolden_DefaultView(t *testing.T) {
	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40
	m := goldenModel(t, appState)
	golden.RequireEqual(t, m.View())
}

func TestGolden_DefaultView_80x24(t *testing.T) {
	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 80
	appState.TermHeight = 24
	m := goldenModel(t, appState)
	golden.RequireEqual(t, m.View())
}

func TestGolden_GridView_ProjectScope(t *testing.T) {
	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40
	m := goldenModel(t, appState)

	sessions := m.gridSessions(state.GridRestoreProject)
	m.gridView.Show(sessions, state.GridRestoreProject)
	m.gridView.SetProjectNames(m.gridProjectNames())
	m.gridView.Width = 120
	m.gridView.Height = 40
	m.PushView(ViewGrid)
	golden.RequireEqual(t, m.View())
}

func TestGolden_GridView_AllProjects(t *testing.T) {
	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40
	m := goldenModel(t, appState)

	sessions := m.gridSessions(state.GridRestoreAll)
	m.gridView.Show(sessions, state.GridRestoreAll)
	m.gridView.SetProjectNames(m.gridProjectNames())
	m.gridView.Width = 120
	m.gridView.Height = 40
	m.PushView(ViewGrid)
	golden.RequireEqual(t, m.View())
}

func TestGolden_EmptyState(t *testing.T) {
	appState := state.AppState{
		Projects:   []*state.Project{},
		AgentUsage: make(map[string]state.AgentUsageRecord),
		TermWidth:  120,
		TermHeight: 40,
	}
	m := goldenModel(t, appState)
	golden.RequireEqual(t, m.View())
}

func TestGolden_FilterActive(t *testing.T) {
	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40
	appState.FilterActive = true
	appState.FilterQuery = "sess"
	m := goldenModel(t, appState)
	m.PushView(ViewFilter)
	m.appState.FilterQuery = "sess"
	m.sidebar.FilterQuery = "sess"
	m.sidebar.Rebuild(&m.appState)
	golden.RequireEqual(t, m.View())
}

func TestGolden_HelpOverlay(t *testing.T) {
	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40
	m := goldenModel(t, appState)
	m.PushView(ViewHelp)
	golden.RequireEqual(t, m.View())
}

func TestGolden_ConfirmDialog(t *testing.T) {
	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40
	m := goldenModel(t, appState)
	m.appState.ConfirmMsg = "Kill session \"session-1\"?"
	m.appState.ConfirmAction = "kill-session:sess-1"
	m.confirm.Message = "Kill session \"session-1\"?"
	m.confirm.Action = "kill-session:sess-1"
	m.PushView(ViewConfirm)
	golden.RequireEqual(t, m.View())
}

func TestGolden_NarrowTerminal(t *testing.T) {
	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 60
	appState.TermHeight = 20
	m := goldenModel(t, appState)
	golden.RequireEqual(t, m.View())
}

func TestGolden_VeryNarrowTerminal(t *testing.T) {
	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 30
	appState.TermHeight = 15
	m := goldenModel(t, appState)
	golden.RequireEqual(t, m.View())
}

func TestGolden_PreviewWithContent(t *testing.T) {
	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40
	appState.PreviewContent = "$ claude\n\nHello! I'm Claude, an AI assistant.\n\nHow can I help you today?\n"
	m := goldenModel(t, appState)
	m.preview.SetContent(appState.PreviewContent)
	golden.RequireEqual(t, m.View())
}
