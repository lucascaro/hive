package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lucascaro/hive/internal/state"
)

// TestFlow_AttachHint_ShowAndConfirm tests that the attach hint overlay
// is shown when HideAttachHint=false, and can be confirmed with Enter.
func TestFlow_AttachHint_ShowAndConfirm(t *testing.T) {
	m, mock := testFlowModelWithHint(t)
	f := newFlowRunner(t, m, mock)

	// Press "a" to attach the active session.
	f.SendKey("a")

	// Attach hint should be shown.
	if !f.Model().showAttachHint {
		t.Fatal("showAttachHint should be true with HideAttachHint=false")
	}
	if f.Model().pendingAttach == nil {
		t.Fatal("pendingAttach should be set")
	}
	f.ViewContains("enter")
	f.Snapshot("01-attach-hint-shown")

	// Confirm with Enter.
	cmd := f.SendSpecialKey(tea.KeyEnter)

	// Hint should be dismissed.
	if f.Model().showAttachHint {
		t.Fatal("showAttachHint should be false after confirming")
	}

	// attachPending should be set (quit+restart path).
	if f.Model().attachPending == nil {
		t.Fatal("attachPending should be set")
	}
	if f.Model().attachPending.RestoreGridMode != state.GridRestoreNone {
		t.Fatalf("RestoreGridMode = %q, want %q (sidebar attach)",
			f.Model().attachPending.RestoreGridMode, state.GridRestoreNone)
	}

	// Should return tea.Quit.
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
	quitMsg := cmd()
	if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("cmd returned %T, want tea.QuitMsg", quitMsg)
	}
}

// TestFlow_AttachHint_Dismiss tests that the attach hint can be dismissed with Esc.
func TestFlow_AttachHint_Dismiss(t *testing.T) {
	m, mock := testFlowModelWithHint(t)
	f := newFlowRunner(t, m, mock)

	// Press "a" to show attach hint.
	f.SendKey("a")
	if !f.Model().showAttachHint {
		t.Fatal("showAttachHint should be true")
	}
	f.Snapshot("01-hint-shown")

	// Dismiss with Esc.
	f.SendSpecialKey(tea.KeyEscape)

	// Hint and pending attach should be cleared.
	if f.Model().showAttachHint {
		t.Fatal("showAttachHint should be false after esc")
	}
	if f.Model().pendingAttach != nil {
		t.Fatal("pendingAttach should be nil after dismissing hint")
	}

	// Should be back to sidebar view.
	f.ViewContains("test-project-1")
	f.Snapshot("02-hint-dismissed")
}

// TestFlow_AttachHint_DontShowAgain tests the "d" key to dismiss and disable hint.
func TestFlow_AttachHint_DontShowAgain(t *testing.T) {
	m, mock := testFlowModelWithHint(t)
	f := newFlowRunner(t, m, mock)

	// Show attach hint.
	f.SendKey("a")
	if !f.Model().showAttachHint {
		t.Fatal("showAttachHint should be true")
	}

	// Press "d" to dismiss and disable.
	cmd := f.SendKey("d")

	// Hint should be dismissed.
	if f.Model().showAttachHint {
		t.Fatal("showAttachHint should be false after 'd'")
	}

	// Config should be updated.
	if !f.Model().cfg.HideAttachHint {
		t.Fatal("cfg.HideAttachHint should be true after 'd'")
	}

	// Attach should proceed (attachPending set, tea.Quit returned).
	if f.Model().attachPending == nil {
		t.Fatal("attachPending should be set")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

// TestFlow_SidebarAttach_DirectAttach tests that with HideAttachHint=true,
// pressing "a" goes directly to attach without showing the hint.
func TestFlow_SidebarAttach_DirectAttach(t *testing.T) {
	m, mock := testFlowModel(t) // HideAttachHint=true by default
	f := newFlowRunner(t, m, mock)

	// Press "a" to attach — returns a cmd that produces SessionAttachMsg.
	cmd := f.SendKey("a")

	// No hint should be shown.
	if f.Model().showAttachHint {
		t.Fatal("showAttachHint should be false with HideAttachHint=true")
	}

	// Execute the cmd chain: attachActiveSession → SessionAttachMsg → doAttach → tea.Quit.
	if cmd == nil {
		t.Fatal("expected a command from attach key")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("attachActiveSession command returned nil")
	}
	cmd = f.Send(msg)

	// attachPending should be set (quit+restart path).
	if f.Model().attachPending == nil {
		t.Fatal("attachPending should be set for direct attach")
	}

	// Should return tea.Quit.
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
	quitMsg := cmd()
	if _, ok := quitMsg.(tea.QuitMsg); !ok {
		t.Fatalf("cmd returned %T, want tea.QuitMsg", quitMsg)
	}
}
