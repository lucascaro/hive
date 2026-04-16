package tui

import (
	"bytes"
	"os/exec"
	"strings"
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
	f.SendSpecialKey(tea.KeyEnter)

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
	f.SendSpecialKey(tea.KeyEnter)
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
	f.SendSpecialKey(tea.KeyEnter)
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

// TestDoAttach_ExecPath_WritesClearOnly verifies that on the tea.ExecProcess
// attach path (tmux backend), doAttach writes exactly the screen-clear sequence
// \033[2J\033[H to its output and does NOT write \033[?1049l (exit alt-screen).
//
// This is the regression test for the flash introduced by PR #110: removing
// \033[?1049l from doAttach was correct (it raced with the renderer), but the
// fix must never re-introduce an alt-screen exit in the pre-attach output because
// that would expose the primary terminal buffer.
func TestDoAttach_ExecPath_WritesClearOnly(t *testing.T) {
	m, mock := testFlowModel(t)

	// Enable the ExecProcess attach path.
	mock.SetUseExecAttach(true)

	// Inject a buffer so we can capture what doAttach writes.
	var buf bytes.Buffer
	m.attachOut = &buf

	// Trigger doAttach via a SessionAttachMsg (same path as pressing "a").
	msg := SessionAttachMsg{
		TmuxSession: "hive-sessions",
		TmuxWindow:  0,
	}
	m.doAttach(msg) // we only care about the side-effect on attachOut

	got := buf.String()

	// Must contain the clear sequence.
	if !strings.Contains(got, "\033[2J\033[H") {
		t.Errorf("doAttach output missing clear sequence \\033[2J\\033[H; got %q", got)
	}

	// Must NOT exit alt-screen — that would expose the primary buffer.
	if strings.Contains(got, "\033[?1049l") {
		t.Errorf("doAttach output contains \\033[?1049l (alt-screen exit) which causes a terminal flash; got %q", got)
	}
}

// TestAltScreenExecCmd_Run_EntersAltScreen verifies that altScreenExecCmd.Run()
// immediately writes the enter-alt-screen + clear sequence before starting the
// subprocess. This minimizes the primary-buffer flash that occurs between
// BubbleTea's ReleaseTerminal (exitAltScreen + 10ms sleep) and subprocess start.
//
// Critical properties:
//   - Must write \033[?1049h (enter alt-screen) before subprocess starts
//   - Must write \033[2J\033[H (clear screen)
//   - Must NOT write \033[?1049l (exit alt-screen would be wrong — we're re-entering)
func TestAltScreenExecCmd_Run_EntersAltScreen(t *testing.T) {
	var termBuf bytes.Buffer

	// Use a no-op command so the test runs fast without side effects.
	cmd := exec.Command("true")
	var cmdOut bytes.Buffer
	cmd.Stdout = &cmdOut

	execCmd := &altScreenExecCmd{cmd: cmd, termOut: &termBuf}
	if err := execCmd.Run(); err != nil {
		t.Fatalf("altScreenExecCmd.Run() unexpected error: %v", err)
	}

	got := termBuf.String()

	if !strings.Contains(got, "\033[?1049h") {
		t.Errorf("altScreenExecCmd.Run() missing alt-screen enter \\033[?1049h; got %q", got)
	}
	if !strings.Contains(got, "\033[2J") {
		t.Errorf("altScreenExecCmd.Run() missing clear \\033[2J; got %q", got)
	}
	if strings.Contains(got, "\033[?1049l") {
		t.Errorf("altScreenExecCmd.Run() must not exit alt-screen (\\033[?1049l causes a flash); got %q", got)
	}
}

// TestAltScreenExecCmd_SetStdin_OnlyIfNil verifies that SetStdin/SetStdout/SetStderr
// do not override values already set on the underlying exec.Cmd.
func TestAltScreenExecCmd_SetStdin_OnlyIfNil(t *testing.T) {
	var termBuf, cmdOut bytes.Buffer
	cmd := exec.Command("true")
	cmd.Stdout = &cmdOut // pre-set

	execCmd := &altScreenExecCmd{cmd: cmd, termOut: &termBuf}

	var other bytes.Buffer
	execCmd.SetStdout(&other) // should be ignored — already set
	if cmd.Stdout != &cmdOut {
		t.Error("SetStdout should not override a pre-set cmd.Stdout")
	}
}

// TestFlow_SidebarAttach_DirectAttach tests that with HideAttachHint=true,
// pressing "a" goes directly to attach without showing the hint.
func TestFlow_SidebarAttach_DirectAttach(t *testing.T) {
	m, mock := testFlowModel(t) // HideAttachHint=true by default
	f := newFlowRunner(t, m, mock)

	// Press "a" to attach — returns a cmd that produces SessionAttachMsg.
	cmd := f.SendSpecialKey(tea.KeyEnter)

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
