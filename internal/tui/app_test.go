package tui

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

func testModelWithSessions() Model {
	cfg := config.DefaultConfig()
	appState := state.AppState{
		Projects: []*state.Project{
			{
				ID:   "proj-1",
				Name: "test-project",
				Teams: []*state.Team{},
				Sessions: []*state.Session{
					{
						ID:          "sess-1",
						ProjectID:   "proj-1",
						Title:       "session-1",
						TmuxSession: "hive-proj1234",
						TmuxWindow:  0,
						Status:      state.StatusRunning,
						AgentType:   state.AgentClaude,
						AgentCmd:    []string{"claude"},
					},
					{
						ID:          "sess-2",
						ProjectID:   "proj-1",
						Title:       "session-2",
						TmuxSession: "hive-proj1234",
						TmuxWindow:  1,
						Status:      state.StatusRunning,
						AgentType:   state.AgentCodex,
						AgentCmd:    []string{"codex"},
					},
				},
			},
		},
		AgentUsage: make(map[string]state.AgentUsageRecord),
	}
	return New(cfg, appState)
}

func TestNewAutoSelectsFirstSession(t *testing.T) {
	m := testModelWithSessions()

	if m.appState.ActiveSessionID == "" {
		t.Fatal("New() should auto-select first session, but ActiveSessionID is empty")
	}
	if m.appState.ActiveSessionID != "sess-1" {
		t.Fatalf("New() auto-selected %q, want %q", m.appState.ActiveSessionID, "sess-1")
	}
	if m.appState.ActiveProjectID != "proj-1" {
		t.Fatalf("New() auto-selected project %q, want %q", m.appState.ActiveProjectID, "proj-1")
	}
}

func TestNewAutoSelectsFirstSession_Empty(t *testing.T) {
	cfg := config.DefaultConfig()
	appState := state.AppState{
		Projects:   []*state.Project{},
		AgentUsage: make(map[string]state.AgentUsageRecord),
	}
	m := New(cfg, appState)

	if m.appState.ActiveSessionID != "" {
		t.Fatalf("New() with no sessions should leave ActiveSessionID empty, got %q", m.appState.ActiveSessionID)
	}
}

func TestPreviewUpdatedMsg_SetsContent(t *testing.T) {
	m := testModelWithSessions()

	// Send PreviewUpdatedMsg for active session
	msg := components.PreviewUpdatedMsg{
		SessionID: "sess-1",
		Content:   "Hello from tmux!",
	}
	result, _ := m.Update(msg)
	updated := result.(Model)

	if updated.appState.PreviewContent != "Hello from tmux!" {
		t.Fatalf("PreviewContent=%q, want %q", updated.appState.PreviewContent, "Hello from tmux!")
	}
}

func TestPreviewUpdatedMsg_IgnoresWrongSession(t *testing.T) {
	m := testModelWithSessions()

	// Send PreviewUpdatedMsg for non-active session
	msg := components.PreviewUpdatedMsg{
		SessionID: "sess-999",
		Content:   "should be ignored",
	}
	result, _ := m.Update(msg)
	updated := result.(Model)

	if updated.appState.PreviewContent != "" {
		t.Fatalf("PreviewContent should be empty for wrong session, got %q", updated.appState.PreviewContent)
	}
}

func TestPreviewUpdatedMsg_SchedulesNextPoll(t *testing.T) {
	m := testModelWithSessions()

	msg := components.PreviewUpdatedMsg{
		SessionID: "sess-1",
		Content:   "content",
	}
	_, cmd := m.Update(msg)

	if cmd == nil {
		t.Fatal("PreviewUpdatedMsg handler should return a cmd to schedule next poll")
	}
}

func TestViewRendersPreviewContent(t *testing.T) {
	m := testModelWithSessions()
	m.appState.TermWidth = 120
	m.appState.TermHeight = 40
	m.appState.PreviewContent = "Hello preview content!"

	view := m.View()
	if !strings.Contains(view, "Hello preview content!") {
		t.Error("View() should contain the preview content")
	}
}

func TestViewShowsWaitingWhenNoContent(t *testing.T) {
	m := testModelWithSessions()
	m.appState.TermWidth = 120
	m.appState.TermHeight = 40
	m.appState.PreviewContent = ""

	view := m.View()
	if !strings.Contains(view, "Waiting for output") {
		t.Error("View() should show 'Waiting for output' when no content")
	}
}

func TestViewShowsNoActiveSession(t *testing.T) {
	cfg := config.DefaultConfig()
	appState := state.AppState{
		Projects:   []*state.Project{{ID: "p1", Name: "test", Teams: []*state.Team{}, Sessions: []*state.Session{}}},
		AgentUsage: make(map[string]state.AgentUsageRecord),
	}
	m := New(cfg, appState)
	m.appState.TermWidth = 120
	m.appState.TermHeight = 40

	view := m.View()
	if !strings.Contains(view, "No active session") {
		t.Error("View() with no sessions should show 'No active session'")
	}
}

func TestSyncActiveFromSidebar_SetsActiveSession(t *testing.T) {
	m := testModelWithSessions()

	// Navigate down to second session
	m.sidebar.MoveDown() // project -> team header? no, it's sessions directly
	m.sidebar.MoveDown()
	m.syncActiveFromSidebar()

	sel := m.sidebar.Selected()
	if sel == nil {
		t.Fatal("Selected() returned nil")
	}
	if sel.SessionID != "" && m.appState.ActiveSessionID != sel.SessionID {
		t.Fatalf("ActiveSessionID=%q, want %q", m.appState.ActiveSessionID, sel.SessionID)
	}
}

func TestSchedulePollPreview_ReturnsNilWithoutSession(t *testing.T) {
	cfg := config.DefaultConfig()
	appState := state.AppState{
		Projects:   []*state.Project{},
		AgentUsage: make(map[string]state.AgentUsageRecord),
	}
	m := New(cfg, appState)

	cmd := m.schedulePollPreview()
	if cmd != nil {
		t.Fatal("schedulePollPreview with no active session should return nil")
	}
}

func TestSchedulePollPreview_ReturnsCmdWithSession(t *testing.T) {
	m := testModelWithSessions()

	cmd := m.schedulePollPreview()
	if cmd == nil {
		t.Fatal("schedulePollPreview with active session should return a cmd")
	}
}
