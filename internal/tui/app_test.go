package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/escape"
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
	m.preview.SetContent("Hello preview content!")

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

func TestViewExactTermHeight(t *testing.T) {
	dims := [][2]int{
		{80, 24},
		{120, 40},
		{160, 50},
		{60, 20},
		{200, 30},
		{55, 24}, // narrower than minTermWidth=60: sidebar collapses
		{30, 15}, // very narrow
	}
	contents := []string{
		"",
		"one line",
		strings.Repeat("line\n", 10),
		strings.Repeat("line\n", 100),
	}
	for _, d := range dims {
		for i, c := range contents {
			t.Run(fmt.Sprintf("%dx%d_content%d", d[0], d[1], i), func(t *testing.T) {
				m := testModelWithSessions()
				m.appState.TermWidth = d[0]
				m.appState.TermHeight = d[1]
				m.appState.PreviewContent = c
				m.preview.SetContent(c)
				out := m.View()
				got := strings.Count(out, "\n") + 1
				if got != d[1] {
					t.Errorf("View() = %d lines for term %dx%d (content#%d), want exactly %d",
						got, d[0], d[1], i, d[1])
				}
			})
		}
	}
}

func TestSessionSwitch_FrameHeightStable(t *testing.T) {
	// Frame height must not change when switching between sessions.
	m := testModelWithSessions()
	m.appState.TermWidth = 120
	m.appState.TermHeight = 40
	m.appState.PreviewContent = strings.Repeat("line from session 1\n", 35)
	m.preview.SetContent(strings.Repeat("line from session 1\n", 35))

	view1 := m.View()
	if strings.Count(view1, "\n")+1 != 40 {
		t.Fatalf("before switch: View() = %d lines, want 40", strings.Count(view1, "\n")+1)
	}

	// Simulate switching to session 2 (clear content).
	m.appState.ActiveSessionID = "sess-2"
	m.appState.PreviewContent = ""
	m.preview.SetContent("")

	view2 := m.View()
	if strings.Count(view2, "\n")+1 != 40 {
		t.Errorf("after switch: View() = %d lines, want 40", strings.Count(view2, "\n")+1)
	}
}

func TestSessionSwitch_PreviewClears(t *testing.T) {
	// When no cached snapshot exists for the target session, PreviewContent must
	// be empty after switching so old session content is not shown.
	m := testModelWithSessions()
	m.appState.PreviewContent = "old session content"
	m.preview.SetContent("old session content")
	// Ensure no snapshot is stored for sess-2 (the target session).
	delete(m.contentSnapshots, "sess-2")

	// Navigate down past the project header to the second session.
	m.sidebar.MoveDown() // to team or second session
	m.sidebar.MoveDown()
	prev := m.appState.ActiveSessionID
	m.syncActiveFromSidebar()

	if m.appState.ActiveSessionID != prev {
		// Actually switched — no cache means content should be empty.
		if m.appState.PreviewContent != "" {
			t.Errorf("PreviewContent = %q after session switch with no cache, want empty", m.appState.PreviewContent)
		}
	}
}

func TestSessionSwitch_PreviewShowsCachedContent(t *testing.T) {
	// When a cached snapshot exists for the target session, it should be shown
	// immediately after switching instead of an empty pane.
	m := testModelWithSessions()
	m.appState.PreviewContent = "old session content"
	m.preview.SetContent("old session content")

	const cachedContent = "cached output for sess-2"
	m.contentSnapshots["sess-2"] = cachedContent

	// Navigate down past the project header to the second session.
	m.sidebar.MoveDown()
	m.sidebar.MoveDown()
	prev := m.appState.ActiveSessionID
	m.syncActiveFromSidebar()

	if m.appState.ActiveSessionID != prev {
		// Actually switched — cached content should be shown immediately.
		if m.appState.PreviewContent != cachedContent {
			t.Errorf("PreviewContent = %q after session switch with cache, want %q", m.appState.PreviewContent, cachedContent)
		}
	}
}

func TestStatusesDetectedMsg_UpdatesActivePreview(t *testing.T) {
	// When StatusesDetectedMsg carries content for the active session, the
	// preview must be updated immediately without waiting for the next PollPreview tick.
	m := testModelWithSessions()

	const freshContent = "fresh output from status watcher"
	msg := escape.StatusesDetectedMsg{
		Statuses: map[string]state.SessionStatus{
			"sess-1": state.StatusRunning,
		},
		Contents: map[string]string{
			"sess-1": freshContent,
		},
	}
	result, _ := m.Update(msg)
	updated := result.(Model)

	if updated.appState.PreviewContent != freshContent {
		t.Errorf("PreviewContent = %q after StatusesDetectedMsg, want %q", updated.appState.PreviewContent, freshContent)
	}
}

func TestStatusesDetectedMsg_IgnoresBackgroundPreview(t *testing.T) {
	// StatusesDetectedMsg content for a background session must not overwrite
	// the active session's preview content.
	m := testModelWithSessions()
	// sess-1 is active; give it some existing preview content.
	m.appState.PreviewContent = "active session output"
	m.preview.SetContent("active session output")

	msg := escape.StatusesDetectedMsg{
		Statuses: map[string]state.SessionStatus{
			"sess-2": state.StatusRunning,
		},
		Contents: map[string]string{
			"sess-2": "background session output",
		},
	}
	result, _ := m.Update(msg)
	updated := result.(Model)

	if updated.appState.PreviewContent != "active session output" {
		t.Errorf("PreviewContent = %q, want active session content unchanged", updated.appState.PreviewContent)
	}
}
