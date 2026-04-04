package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/escape"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

// TestMain sets HIVE_CONFIG_DIR to a temporary directory so that no test in this
// package (including helpers like New() that stat config.StatePath()) ever
// resolves against the real ~/.config/hive.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hive-tui-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot create temp dir: %v\n", err)
		os.Exit(1)
	}
	if err := os.Setenv("HIVE_CONFIG_DIR", dir); err != nil {
		fmt.Fprintf(os.Stderr, "cannot set HIVE_CONFIG_DIR: %v\n", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func testModelWithSessions() Model {
	cfg := config.DefaultConfig()
	appState := state.AppState{
		Projects: []*state.Project{
			{
				ID:    "proj-1",
				Name:  "test-project",
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

func testAppStateWithTwoProjects() state.AppState {
	return state.AppState{
		ActiveProjectID: "proj-1",
		ActiveSessionID: "sess-1",
		Projects: []*state.Project{
			{
				ID:    "proj-1",
				Name:  "test-project-1",
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
				},
			},
			{
				ID:    "proj-2",
				Name:  "test-project-2",
				Teams: []*state.Team{},
				Sessions: []*state.Session{
					{
						ID:          "sess-2",
						ProjectID:   "proj-2",
						Title:       "session-2",
						TmuxSession: "hive-proj5678",
						TmuxWindow:  0,
						Status:      state.StatusRunning,
						AgentType:   state.AgentCodex,
						AgentCmd:    []string{"codex"},
					},
				},
			},
		},
		AgentUsage: make(map[string]state.AgentUsageRecord),
	}
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

func TestNew_RestoresProjectGridMode(t *testing.T) {
	cfg := config.DefaultConfig()
	appState := testAppStateWithTwoProjects()
	appState.RestoreGridMode = state.GridRestoreProject

	m := New(cfg, appState)

	if !m.gridView.Active {
		t.Fatal("grid view should be active after restore")
	}
	if m.gridView.Mode != state.GridRestoreProject {
		t.Fatalf("grid mode = %q, want %q", m.gridView.Mode, state.GridRestoreProject)
	}
	sessions := m.gridSessions(m.gridView.Mode)
	if len(sessions) != 1 {
		t.Fatalf("restored project grid should show 1 session, got %d", len(sessions))
	}
	if sessions[0].ProjectID != "proj-1" {
		t.Fatalf("restored project grid session project = %q, want proj-1", sessions[0].ProjectID)
	}
}

func TestNew_RestoresAllProjectsGridMode(t *testing.T) {
	cfg := config.DefaultConfig()
	appState := testAppStateWithTwoProjects()
	appState.RestoreGridMode = state.GridRestoreAll

	m := New(cfg, appState)

	if !m.gridView.Active {
		t.Fatal("grid view should be active after restore")
	}
	if m.gridView.Mode != state.GridRestoreAll {
		t.Fatalf("grid mode = %q, want %q", m.gridView.Mode, state.GridRestoreAll)
	}
	if got := len(m.gridSessions(m.gridView.Mode)); got != 2 {
		t.Fatalf("restored all-projects grid should show 2 sessions, got %d", got)
	}
}

func TestInit_IncludesGridPollWhenGridRestored(t *testing.T) {
	cfg := config.DefaultConfig()
	appState := testAppStateWithTwoProjects()
	appState.RestoreGridMode = state.GridRestoreProject

	m := New(cfg, appState)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() should return a batch command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init() msg type = %T, want tea.BatchMsg", msg)
	}
	// Expected commands: SetWindowTitle, PollPreview, WatchTitles, WatchStatuses,
	// WatchState, GridPoll = 6 total.
	if len(batch) != 6 {
		t.Fatalf("Init() batch length = %d, want 6 including grid poll", len(batch))
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

func TestGridSessionSelectedMsg_PreservesProjectGridRestoreMode(t *testing.T) {
	m := testModelWithSessions()
	m.gridView.Show(m.gridSessions(state.GridRestoreProject), state.GridRestoreProject)

	result, _ := m.Update(components.GridSessionSelectedMsg{
		TmuxSession: "hive-proj1234",
		TmuxWindow:  0,
	})
	updated := result.(Model)

	if updated.pendingAttach == nil {
		t.Fatal("pendingAttach should be set when attach hint is shown")
	}
	if updated.pendingAttach.RestoreGridMode != state.GridRestoreProject {
		t.Fatalf("RestoreGridMode = %q, want %q", updated.pendingAttach.RestoreGridMode, state.GridRestoreProject)
	}
}

func TestGridSessionSelectedMsg_PreservesAllProjectsGridRestoreMode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.HideAttachHint = true
	appState := testAppStateWithTwoProjects()
	m := New(cfg, appState)
	m.gridView.Show(m.gridSessions(state.GridRestoreAll), state.GridRestoreAll)

	result, cmd := m.Update(components.GridSessionSelectedMsg{
		TmuxSession: "hive-proj1234",
		TmuxWindow:  0,
	})
	updated := result.(Model)

	if cmd == nil {
		t.Fatal("expected quit command when attach hint is disabled")
	}
	if updated.attachPending == nil {
		t.Fatal("attachPending should be set")
	}
	if updated.attachPending.RestoreGridMode != state.GridRestoreAll {
		t.Fatalf("RestoreGridMode = %q, want %q", updated.attachPending.RestoreGridMode, state.GridRestoreAll)
	}
}

// --- DirPicker flow tests ---

func TestNewProject_PressN_OpensDirPickerAfterName(t *testing.T) {
	m := testModelWithSessions()

	// Press "n" to start new-project flow.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = result.(Model)
	if m.inputMode != "project-name" {
		t.Fatalf("inputMode = %q after pressing n, want %q", m.inputMode, "project-name")
	}

	// Type a project name then press enter.
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("myproject")})
	m = result.(Model)
	result, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = result.(Model)

	// After confirming a name, inputMode should be cleared and dirPicker should be active.
	if m.inputMode != "" {
		t.Errorf("inputMode = %q after name confirm, want %q", m.inputMode, "")
	}
	if !m.dirPicker.Active {
		t.Error("dirPicker.Active = false after name confirm, want true")
	}
	if m.pendingProjectName != "myproject" {
		t.Errorf("pendingProjectName = %q, want %q", m.pendingProjectName, "myproject")
	}
}

func TestDirPickedMsg_ExistingDir_CreatesProject(t *testing.T) {
	m := testModelWithSessions()
	m.pendingProjectName = "myproject"
	m.dirPicker.Active = true

	dir := t.TempDir()
	result, cmd := m.Update(components.DirPickedMsg{Dir: dir})
	updated := result.(Model)

	// inputMode should be cleared and a create command returned.
	if updated.inputMode != "" {
		t.Errorf("inputMode = %q after DirPickedMsg with existing dir, want %q", updated.inputMode, "")
	}
	if updated.dirPicker.Active {
		t.Error("dirPicker.Active = true after pick, want false")
	}
	if cmd == nil {
		t.Error("expected a command (createProject) to be returned")
	}
}

func TestDirPickedMsg_NonExistentDir_AsksConfirmation(t *testing.T) {
	m := testModelWithSessions()
	m.pendingProjectName = "myproject"
	m.dirPicker.Active = true

	nonexistent := t.TempDir() + "/does-not-exist"
	result, _ := m.Update(components.DirPickedMsg{Dir: nonexistent})
	updated := result.(Model)

	if updated.inputMode != "project-dir-confirm" {
		t.Errorf("inputMode = %q after DirPickedMsg with new dir, want %q", updated.inputMode, "project-dir-confirm")
	}
}

func TestDirPickerCancelMsg_ReturnsToNameStep(t *testing.T) {
	m := testModelWithSessions()
	m.pendingProjectName = "myproject"
	m.dirPicker.Active = true

	result, _ := m.Update(components.DirPickerCancelMsg{})
	updated := result.(Model)

	if updated.inputMode != "project-name" {
		t.Errorf("inputMode = %q after DirPickerCancelMsg, want %q", updated.inputMode, "project-name")
	}
	if updated.dirPicker.Active {
		t.Error("dirPicker.Active = true after cancel, want false")
	}
	// The pending name must be preserved so the user doesn't have to re-type it.
	if updated.pendingProjectName != "myproject" {
		t.Errorf("pendingProjectName = %q after cancel, want %q", updated.pendingProjectName, "myproject")
	}
}

func TestDirPicker_BackgroundMessages_NotDroppedWhileActive(t *testing.T) {
	m := testModelWithSessions()
	m.dirPicker.Active = true

	// A preview update should still be processed while the picker is open.
	m.previewPollGen = 1
	m.appState.ActiveSessionID = "sess-1"
	previewMsg := components.PreviewUpdatedMsg{
		SessionID:  "sess-1",
		Content:    "background update",
		Generation: 1,
	}
	result, _ := m.Update(previewMsg)
	updated := result.(Model)

	if updated.appState.PreviewContent != "background update" {
		t.Errorf("PreviewContent = %q, want %q after background msg while dirPicker active",
			updated.appState.PreviewContent, "background update")
	}
}

// --- Key isolation tests ---

func TestHandleKey_DirPickerActive_BlocksGlobalKeys(t *testing.T) {
	m := testModelWithSessions()
	m.dirPicker.Active = true

	// Press "/" which is the Filter key — should NOT activate global filter.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated := result.(Model)

	if updated.appState.FilterActive {
		t.Error("FilterActive=true while DirPicker is open — key leaked to global bindings")
	}
}

func TestHandleKey_DirPickerActive_BlocksQuit(t *testing.T) {
	m := testModelWithSessions()
	m.dirPicker.Active = true

	// Press "q" which is the Quit key — should NOT quit.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	// tea.Quit returns a special command; if cmd is non-nil and produces
	// a QuitMsg, the key leaked.
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(tea.QuitMsg); ok {
			t.Error("quit key fired while DirPicker is open — key leaked to global bindings")
		}
	}
}

func TestHandleKey_AgentPickerActive_BlocksGlobalKeys(t *testing.T) {
	m := testModelWithSessions()
	m.agentPicker.Show(components.DefaultAgentItems)

	// Press "/" (Filter key) — should not activate global filter.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated := result.(Model)

	if updated.appState.FilterActive {
		t.Error("FilterActive=true while AgentPicker is open — key leaked to global bindings")
	}
}

func TestHandleKey_FilterActive_BlocksGlobalKeys(t *testing.T) {
	m := testModelWithSessions()
	m.appState.FilterActive = true

	// Press "n" which is the NewProject key — should add to filter, not open new project.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	updated := result.(Model)

	if updated.inputMode == "project-name" {
		t.Error("NewProject triggered while filter is active — key leaked to global bindings")
	}
	if updated.appState.FilterQuery != "n" {
		t.Errorf("FilterQuery=%q, want %q", updated.appState.FilterQuery, "n")
	}
}

func TestHandleKey_NoOverlay_GlobalKeysWork(t *testing.T) {
	m := testModelWithSessions()

	// Press "/" to activate filter — should work when no overlay is active.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated := result.(Model)

	if !updated.appState.FilterActive {
		t.Error("FilterActive=false after pressing / with no overlay — global keys broken")
	}
}

func TestHandleKey_SettingsActive_BlocksAll(t *testing.T) {
	m := testModelWithSessions()
	m.settings.Active = true

	// Press "n" (NewProject) — should be swallowed by settings.
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	updated := result.(Model)

	if updated.inputMode == "project-name" {
		t.Error("NewProject triggered while Settings is open — key leaked to global bindings")
	}
}

func TestHandleKey_ConfirmActive_BlocksAll(t *testing.T) {
	m := testModelWithSessions()
	m.appState.ShowConfirm = true
	m.appState.ConfirmAction = "test-action"
	m.confirm.Message = "Are you sure?"

	// Press "n" (NewProject key) — should be handled by confirm (treated as cancel/no-op).
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	updated := result.(Model)

	if updated.inputMode == "project-name" {
		t.Error("NewProject triggered while Confirm is open — key leaked to global bindings")
	}
}

func TestHandleKey_CtrlC_AlwaysQuits(t *testing.T) {
	m := testModelWithSessions()
	m.dirPicker.Active = true

	// ctrl+c should always quit, even with an overlay active.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c should return a quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c cmd returned %T, want tea.QuitMsg", msg)
	}
}
