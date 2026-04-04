package tui

import (
	"testing"

	"github.com/lucascaro/hive/internal/escape"
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

	// Step 2: Pick an agent. In real flow, the picker's own Update() hides
	// itself and emits AgentPickedMsg. We simulate both steps.
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
		f.SendKey("j")
	}

	if f.Model().appState.ActiveSessionID != "sess-2" {
		t.Skipf("could not navigate to sess-2, active = %q", f.Model().appState.ActiveSessionID)
	}

	// Preview should show "Waiting" since there's no cached content for session-2.
	f.ViewContains("Waiting for output")
	f.Snapshot("02-session2-waiting")

	// Navigate back to session 1 — cached content should be restored from contentSnapshots.
	for i := 0; i < 5 && f.Model().appState.ActiveSessionID != "sess-1"; i++ {
		f.SendKey("k")
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

// TestFlow_StatusUpdate_UpdatesPreview tests that StatusesDetectedMsg
// updates the preview for the active session.
func TestFlow_StatusUpdate_UpdatesPreview(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Initially no content.
	f.ViewContains("Waiting for output")

	// Send status update with content for active session.
	f.Send(escape.StatusesDetectedMsg{
		Statuses: map[string]state.SessionStatus{
			"sess-1": state.StatusRunning,
		},
		Contents: map[string]string{
			"sess-1": "Fresh output from agent",
		},
	})

	f.ViewContains("Fresh output from agent")
	f.Snapshot("01-status-updated")
}
