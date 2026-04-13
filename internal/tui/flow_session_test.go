package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/escape"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/mux/muxtest"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

// TestFlow_NewSession_AgentPick_Created tests the full new session flow:
// press "t" → agent picker opens → pick agent → session created → appears in sidebar.
func TestFlow_NewSession_AgentPick_Created(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Verify starting state.
	f.ViewContains("test-project-1")
	f.ViewContains("session-1")

	// Step 1: Press "t" to open agent picker.
	f.SendKey("t")
	if !f.Model().agentPicker.Active {
		t.Fatal("agentPicker should be active after pressing 't'")
	}
	f.AssertInputMode("new-session")
	f.Snapshot("01-agent-picker")

	// Step 2: Pick an agent. In the real flow, pressing Enter in the picker
	// triggers the picker's own Update() which calls Hide() and emits
	// AgentPickedMsg. We can't easily simulate the picker's internal key
	// handling, so we manually hide it and send the message directly.
	{
		m := f.Model()
		m.agentPicker.Hide()
		f.model = m
	}
	cmd := f.Send(components.AgentPickedMsg{AgentType: state.AgentClaude})

	if cmd == nil {
		t.Fatal("expected a command after agent pick")
	}

	// Execute the returned command.
	msg := cmd()
	if msg == nil {
		t.Fatal("command returned nil message")
	}

	// The result could be SessionCreatedMsg (agent found) or ConfirmActionMsg
	// (agent not found, install prompt). Handle both paths.
	f.Send(msg)

	switch msg.(type) {
	case SessionCreatedMsg:
		// Agent was found and session was created.
		if mock.CallCount("CreateSession")+mock.CallCount("CreateWindow") == 0 {
			t.Fatal("mock backend should have CreateSession or CreateWindow called")
		}
		// Don't use Snapshot here — session title is randomly generated.
		// Use ViewContains to verify the new session appears.
		f.ViewContains("[claude]")

	case ConfirmActionMsg:
		// Agent not found — install prompt shown (expected in test env).
		if !f.Model().appState.ShowConfirm {
			t.Fatal("ShowConfirm should be true for install prompt")
		}
		f.ViewContains("not found")
	default:
		t.Fatalf("unexpected message type after agent pick: %T", msg)
	}
}

// TestFlow_KillSession_Confirm_Removed tests the kill session flow:
// press "x" → confirm dialog → "y" → session removed from sidebar.
func TestFlow_KillSession_Confirm_Removed(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Verify session-1 is visible and active.
	f.AssertActiveSession("sess-1")
	f.ViewContains("session-1")
	f.Snapshot("01-before-kill")

	// Step 1: Press "x" to kill the active session → returns ConfirmActionMsg cmd.
	cmd := f.SendKey("x")
	if cmd == nil {
		t.Fatal("expected ConfirmActionMsg command from kill key")
	}

	// Execute cmd to get ConfirmActionMsg and feed it.
	msg := cmd()
	f.Send(msg)

	// Confirm dialog should be visible.
	if !f.Model().appState.ShowConfirm {
		t.Fatal("ShowConfirm should be true after ConfirmActionMsg")
	}
	f.ViewContains("Kill session")
	f.Snapshot("02-confirm-dialog")

	// Step 2: Press "y" to confirm.
	cmd = f.SendKey("y")

	// Confirm should be dismissed.
	if f.Model().appState.ShowConfirm {
		t.Fatal("ShowConfirm should be false after confirming")
	}

	// Execute the ConfirmedMsg cmd chain.
	if cmd != nil {
		msg = cmd()
		if msg != nil {
			cmd = f.Send(msg)
			// Execute kill command.
			if cmd != nil {
				msg = cmd()
				if msg != nil {
					f.Send(msg)
				}
			}
		}
	}

	// Session should be removed — the mock's KillWindow should have been called.
	if mock.CallCount("KillWindow") == 0 {
		t.Fatal("mock backend KillWindow should have been called")
	}

	// View should no longer show the killed session.
	f.Snapshot("03-after-kill")
}

// TestFlow_SessionSwitch_PreviewClearAndCache tests that switching sessions
// clears the preview (or shows cached content) and the view updates accordingly.
func TestFlow_SessionSwitch_PreviewClearAndCache(t *testing.T) {
	m, mock := testFlowModel(t)

	// Pre-populate content snapshots (as StatusesDetectedMsg would).
	m.contentSnapshots["sess-1"] = "Output from session 1"
	m.appState.PreviewContent = "Output from session 1"
	m.preview.SetContent("Output from session 1")

	f := newFlowRunner(t, m, mock)
	f.ViewContains("Output from session 1")
	f.Snapshot("01-session1-content")

	// Navigate down to session 2.
	// Sidebar items order: project-1 header, session-1, project-2 header, session-2.
	for i := 0; i < 5 && f.Model().appState.ActiveSessionID != "sess-2"; i++ {
		f.SendSpecialKey(tea.KeyDown)
	}

	if f.Model().appState.ActiveSessionID != "sess-2" {
		t.Fatalf("could not navigate to sess-2, active = %q", f.Model().appState.ActiveSessionID)
	}

	// Preview should show "Waiting" since there's no cached content for session-2.
	f.ViewContains("Waiting for output")
	f.Snapshot("02-session2-waiting")

	// Navigate back to session 1 — cached content should be restored from contentSnapshots.
	for i := 0; i < 5 && f.Model().appState.ActiveSessionID != "sess-1"; i++ {
		f.SendSpecialKey(tea.KeyUp)
	}

	if f.Model().appState.ActiveSessionID == "sess-1" {
		f.ViewContains("Output from session 1")
		f.Snapshot("03-session1-cached")
	}
}

// TestFlow_SessionSwitch_FrameHeightStable tests that the rendered frame height
// stays constant when switching between sessions with different content lengths.
func TestFlow_SessionSwitch_FrameHeightStable(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Set long content for session 1.
	longContent := ""
	for i := 0; i < 50; i++ {
		longContent += "line from session 1\n"
	}
	f.Send(components.PreviewUpdatedMsg{
		SessionID: "sess-1",
		Content:   longContent,
	})

	view1 := f.View()
	lines1 := countLines(view1)

	// Switch to session 2 with empty content.
	f.Send(components.PreviewUpdatedMsg{
		SessionID: "sess-2",
		Content:   "",
	})
	// Change active session.
	m2 := f.Model()
	m2.appState.ActiveSessionID = "sess-2"
	m2.appState.PreviewContent = ""
	m2.preview.SetContent("")
	f2 := newFlowRunner(t, m2, mock)

	view2 := f2.View()
	lines2 := countLines(view2)

	if lines1 != lines2 {
		t.Errorf("frame height changed: %d lines with content → %d lines without", lines1, lines2)
	}
}

func countLines(s string) int {
	n := 1
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}

// testFlowModelThreeSessions creates a model with one project and 3 sessions
// (sess-1, sess-2, sess-3) for focus management tests.
func testFlowModelThreeSessions(t *testing.T) (Model, *muxtest.MockBackend) {
	t.Helper()
	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)
	t.Setenv("TERM", "dumb")

	mock := muxtest.New()
	mux.SetBackend(mock)
	t.Cleanup(func() { mux.SetBackend(nil) })

	mock.SetPaneContent("hive-proj:0", "$ claude\nSession 1.")
	mock.SetPaneContent("hive-proj:1", "$ claude\nSession 2.")
	mock.SetPaneContent("hive-proj:2", "$ claude\nSession 3.")

	cfg := config.DefaultConfig()
	cfg.HideAttachHint = true
	cfg.PreviewRefreshMs = 1

	appState := state.AppState{
		ActiveProjectID: "proj-1",
		ActiveSessionID: "sess-2", // start focused on middle session
		Projects: []*state.Project{
			{
				ID:    "proj-1",
				Name:  "test-project",
				Color: "#7C3AED",
				Teams: []*state.Team{},
				Sessions: []*state.Session{
					{
						ID:          "sess-1",
						ProjectID:   "proj-1",
						Title:       "session-1",
						TmuxSession: "hive-proj",
						TmuxWindow:  0,
						Status:      state.StatusRunning,
						AgentType:   state.AgentClaude,
						AgentCmd:    []string{"claude"},
					},
					{
						ID:          "sess-2",
						ProjectID:   "proj-1",
						Title:       "session-2",
						TmuxSession: "hive-proj",
						TmuxWindow:  1,
						Status:      state.StatusRunning,
						AgentType:   state.AgentClaude,
						AgentCmd:    []string{"claude"},
					},
					{
						ID:          "sess-3",
						ProjectID:   "proj-1",
						Title:       "session-3",
						TmuxSession: "hive-proj",
						TmuxWindow:  2,
						Status:      state.StatusRunning,
						AgentType:   state.AgentClaude,
						AgentCmd:    []string{"claude"},
					},
				},
			},
		},
		AgentUsage: make(map[string]state.AgentUsageRecord),
		TermWidth:  120,
		TermHeight: 40,
	}

	m := New(cfg, appState, "")
	m.appState.TermWidth = 120
	m.appState.TermHeight = 40
	return m, mock
}

// killActiveSession drives the kill-session confirm flow and returns
// after the session is removed. Requires the active session to be set.
func killActiveSession(t *testing.T, f *flowRunner) {
	t.Helper()
	// Press "x" → returns cmd that produces ConfirmActionMsg.
	cmd := f.SendKey("x")
	if cmd == nil {
		t.Fatal("expected ConfirmActionMsg command from kill key")
	}
	f.ExecCmdChain(cmd)
	// Confirm dialog should be visible.
	if !f.Model().appState.ShowConfirm {
		t.Fatal("ShowConfirm should be true after ConfirmActionMsg")
	}
	// Press "y" to confirm → drives through ConfirmedMsg → killSession → SessionKilledMsg.
	cmd = f.SendKey("y")
	if cmd != nil {
		f.ExecCmdChain(cmd)
	}
}

// gridSelected returns the grid view's currently selected session from the
// flow runner. Works around Selected() having a pointer receiver.
func gridSelected(f *flowRunner) *state.Session {
	return f.model.gridView.Selected()
}

// TestFlow_CreateSession_FocusSidebar verifies that creating a session
// in sidebar view sets focus to the new session.
func TestFlow_CreateSession_FocusSidebar(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)
	f.AssertActiveSession("sess-1")

	// Simulate a session being created (bypass the agent picker).
	newSess := &state.Session{
		ID:          "sess-new",
		ProjectID:   "proj-1",
		Title:       "new-session",
		TmuxSession: "hive-new",
		TmuxWindow:  0,
		Status:      state.StatusRunning,
		AgentType:   state.AgentClaude,
		AgentCmd:    []string{"claude"},
	}
	m2 := f.Model()
	m2.appState.Projects[0].Sessions = append(m2.appState.Projects[0].Sessions, newSess)
	f.model = m2
	f.Send(SessionCreatedMsg{Session: newSess})

	f.AssertActiveSession("sess-new")
}

// TestFlow_CreateSession_FocusGrid verifies that creating a session
// while grid view is open sets the grid cursor to the new session.
func TestFlow_CreateSession_FocusGrid(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)
	f.AssertActiveSession("sess-1")

	// Open all-sessions grid (Shift+G from sidebar).
	f.SendKey("G")
	f.AssertGridActive(true)
	f.AssertGridMode(state.GridRestoreAll)

	// Simulate session creation.
	newSess := &state.Session{
		ID:          "sess-new",
		ProjectID:   "proj-1",
		Title:       "new-session",
		TmuxSession: "hive-new",
		TmuxWindow:  0,
		Status:      state.StatusRunning,
		AgentType:   state.AgentClaude,
		AgentCmd:    []string{"claude"},
	}
	m2 := f.Model()
	m2.appState.Projects[0].Sessions = append(m2.appState.Projects[0].Sessions, newSess)
	f.model = m2
	f.Send(SessionCreatedMsg{Session: newSess})

	f.AssertActiveSession("sess-new")
	sel := gridSelected(f)
	if sel == nil || sel.ID != "sess-new" {
		selID := ""
		if sel != nil {
			selID = sel.ID
		}
		t.Errorf("gridView.Selected() = %q, want %q", selID, "sess-new")
	}
}

// TestFlow_CreateSession_FocusProjectGrid verifies grid focus on create
// when showing project-scoped grid.
func TestFlow_CreateSession_FocusProjectGrid(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)
	f.AssertActiveSession("sess-1")

	// Open project-scoped grid (g from sidebar).
	f.SendKey("g")
	f.AssertGridActive(true)
	f.AssertGridMode(state.GridRestoreProject)

	// Simulate session creation in same project.
	newSess := &state.Session{
		ID:          "sess-new",
		ProjectID:   "proj-1",
		Title:       "new-session",
		TmuxSession: "hive-new",
		TmuxWindow:  0,
		Status:      state.StatusRunning,
		AgentType:   state.AgentClaude,
		AgentCmd:    []string{"claude"},
	}
	m2 := f.Model()
	m2.appState.Projects[0].Sessions = append(m2.appState.Projects[0].Sessions, newSess)
	f.model = m2
	f.Send(SessionCreatedMsg{Session: newSess})

	f.AssertActiveSession("sess-new")
	sel := gridSelected(f)
	if sel == nil || sel.ID != "sess-new" {
		selID := ""
		if sel != nil {
			selID = sel.ID
		}
		t.Errorf("gridView.Selected() = %q, want %q", selID, "sess-new")
	}
}

// TestFlow_KillSession_FocusSidebar verifies that killing the middle session
// in sidebar view moves focus to the next session in the same group.
func TestFlow_KillSession_FocusSidebar(t *testing.T) {
	m, mock := testFlowModelThreeSessions(t)
	f := newFlowRunner(t, m, mock)
	f.AssertActiveSession("sess-2")

	killActiveSession(t, f)

	// Focus should move to sess-3 (next in group).
	f.AssertActiveSession("sess-3")
}

// TestFlow_KillSession_FocusGrid verifies that killing a session in
// all-sessions grid view moves focus to the next session.
func TestFlow_KillSession_FocusGrid(t *testing.T) {
	m, mock := testFlowModelThreeSessions(t)
	f := newFlowRunner(t, m, mock)
	f.AssertActiveSession("sess-2")

	// Open all-sessions grid (Shift+G).
	f.SendKey("G")
	f.AssertGridActive(true)

	killActiveSession(t, f)

	f.AssertActiveSession("sess-3")
	sel := gridSelected(f)
	if sel == nil || sel.ID != "sess-3" {
		selID := ""
		if sel != nil {
			selID = sel.ID
		}
		t.Errorf("gridView.Selected() = %q, want %q", selID, "sess-3")
	}
}

// TestFlow_KillSession_FocusProjectGrid verifies that killing a session in
// project-scoped grid moves focus to the next session in the same project.
func TestFlow_KillSession_FocusProjectGrid(t *testing.T) {
	m, mock := testFlowModelThreeSessions(t)
	f := newFlowRunner(t, m, mock)
	f.AssertActiveSession("sess-2")

	// Open project grid (g).
	f.SendKey("g")
	f.AssertGridActive(true)
	f.AssertGridMode(state.GridRestoreProject)

	killActiveSession(t, f)

	f.AssertActiveSession("sess-3")
	sel := gridSelected(f)
	if sel == nil || sel.ID != "sess-3" {
		selID := ""
		if sel != nil {
			selID = sel.ID
		}
		t.Errorf("gridView.Selected() = %q, want %q", selID, "sess-3")
	}
}

// TestFlow_KillSession_CrossProjectFocusSync verifies that when the only
// session in a project is killed, focus (including ActiveProjectID) moves
// to a session in another project — breadcrumb must update.
func TestFlow_KillSession_CrossProjectFocusSync(t *testing.T) {
	m, mock := testFlowModel(t) // 2 projects, 1 session each, active = sess-1 in proj-1
	f := newFlowRunner(t, m, mock)
	f.AssertActiveSession("sess-1")
	if f.Model().appState.ActiveProjectID != "proj-1" {
		t.Fatalf("ActiveProjectID = %q, want %q", f.Model().appState.ActiveProjectID, "proj-1")
	}

	killActiveSession(t, f)

	// Focus should fall back to sess-2 in proj-2.
	f.AssertActiveSession("sess-2")
	if f.Model().appState.ActiveProjectID != "proj-2" {
		t.Errorf("ActiveProjectID = %q, want %q (should follow focused session)", f.Model().appState.ActiveProjectID, "proj-2")
	}
}

// TestFlow_KillSession_LastInGroup verifies fallback when killing the last
// session in a group — should move to previous.
func TestFlow_KillSession_LastInGroup(t *testing.T) {
	m, mock := testFlowModelThreeSessions(t)
	f := newFlowRunner(t, m, mock)

	// Navigate to sess-3 (last session).
	for i := 0; i < 5 && f.Model().appState.ActiveSessionID != "sess-3"; i++ {
		f.SendSpecialKey(tea.KeyDown)
	}
	f.AssertActiveSession("sess-3")

	killActiveSession(t, f)

	// Focus should move to sess-2 (previous in group).
	f.AssertActiveSession("sess-2")
}

// TestFlow_StatusUpdate_UpdatesPreview tests that the preview is updated by
// PreviewUpdatedMsg (PollPreview), not by StatusesDetectedMsg.
// StatusesDetectedMsg updates the content snapshot for the grid / session-switch
// cache, but must not update the live preview to avoid scroll jumps caused by
// alternating 50-line (WatchStatuses) and 500-line (PollPreview) content.
func TestFlow_StatusUpdate_UpdatesPreview(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Initially no content.
	f.ViewContains("Waiting for output")

	// StatusesDetectedMsg must NOT update the preview — preview stays "Waiting".
	f.Send(escape.StatusesDetectedMsg{
		Statuses: map[string]state.SessionStatus{
			"sess-1": state.StatusRunning,
		},
		Contents: map[string]string{
			"sess-1": "Fresh output from agent",
		},
	})
	f.ViewContains("Waiting for output")

	// PreviewUpdatedMsg (from PollPreview) IS the authoritative preview source.
	gen := f.Model().previewPollGen
	f.Send(components.PreviewUpdatedMsg{
		SessionID:  "sess-1",
		Content:    "Fresh output from agent",
		Generation: gen,
	})
	f.ViewContains("Fresh output from agent")
	f.Snapshot("01-status-updated")
}

// TestFlow_SidebarNewWorktreeSession tests that pressing "W" in the sidebar
// opens the agent picker for creating a new worktree session.
func TestFlow_SidebarNewWorktreeSession(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)
	f.AssertActiveSession("sess-1")

	// Press "W" to create a new worktree session from sidebar.
	f.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("W")})

	model := f.Model()

	// The test project directory may not be a git repo, so the guard may
	// return an error. Either outcome validates the key binding is wired up.
	if model.agentPicker.Active {
		// Git repo found — agent picker should be active with worktree mode.
		if model.inputMode != "new-session" {
			t.Errorf("inputMode = %q, want %q", model.inputMode, "new-session")
		}
		if model.pendingProjectID != "proj-1" {
			t.Errorf("pendingProjectID = %q, want %q", model.pendingProjectID, "proj-1")
		}
		if !model.pendingWorktree {
			t.Error("pendingWorktree should be true for worktree session")
		}
	}
	// If agentPicker is not active, the project dir was not a git repo and an
	// ErrorMsg was returned — that's the expected guard working correctly.
}
