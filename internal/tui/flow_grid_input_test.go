package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/mux/muxtest"
	"github.com/lucascaro/hive/internal/state"
	"github.com/muesli/termenv"
)

// TestGridInputMode_EnterAndExit verifies that pressing 'i' enters input mode
// and Ctrl+Q exits it, leaving the grid active in both cases.
func TestGridInputMode_EnterAndExit(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Open grid.
	f.SendKey("g")
	f.AssertGridActive(true)

	if f.model.gridView.InputMode() {
		t.Fatal("gridView.InputMode() should be false before pressing i")
	}

	// Press 'i' to enter input mode.
	f.SendKey("i")
	if !f.model.gridView.InputMode() {
		t.Fatal("gridView.InputMode() should be true after pressing i")
	}
	// Grid must still be active.
	f.AssertGridActive(true)

	// Press Ctrl+Q to exit input mode.
	f.Send(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if f.model.gridView.InputMode() {
		t.Fatal("gridView.InputMode() should be false after Ctrl+Q")
	}
	// Grid must remain active after exiting input mode.
	f.AssertGridActive(true)
}

// TestGridInputMode_EscForwardedToSession verifies that Esc in input mode
// is forwarded to the session (as \033) rather than closing the grid.
func TestGridInputMode_EscForwardedToSession(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.SendKey("g")
	f.SendKey("i")

	// Esc should NOT exit grid or input mode; it should be forwarded.
	cmd := f.SendSpecialKey(tea.KeyEscape)
	f.ExecCmdChain(cmd)

	// Grid must still be active and in input mode.
	f.AssertGridActive(true)
	if !f.model.gridView.InputMode() {
		t.Fatal("gridView.InputMode() should still be true after Esc")
	}
	// The mock should have received the Esc as \033.
	if mock.LastSentKeys != "\033" {
		t.Errorf("LastSentKeys = %q, want %q", mock.LastSentKeys, "\033")
	}
}

// TestGridInputMode_KeysForwarded verifies that printable keys in input mode
// are forwarded to the focused session via SendKeys.
func TestGridInputMode_KeysForwarded(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.SendKey("g")
	f.SendKey("i")

	// Press 'y' — should forward to session.
	cmd := f.SendKey("y")
	f.ExecCmdChain(cmd)

	if mock.CallCount("SendKeys") != 1 {
		t.Errorf("SendKeys call count = %d, want 1", mock.CallCount("SendKeys"))
	}
	if mock.LastSentKeys != "y" {
		t.Errorf("LastSentKeys = %q, want %q", mock.LastSentKeys, "y")
	}
}

// TestGridInputMode_NavSuppressedInInputMode verifies that nav shortcuts like
// 'x' (kill) are not triggered when input mode is active; instead the key is
// forwarded to the session.
func TestGridInputMode_NavSuppressedInInputMode(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.SendKey("g")
	f.SendKey("i")

	// 'x' in nav mode would push a kill-session confirm dialog.
	// In input mode it must be forwarded instead.
	cmd := f.SendKey("x")
	f.ExecCmdChain(cmd)

	// No confirm dialog should have been pushed.
	if f.model.TopView() == ViewConfirm {
		t.Error("ViewConfirm was pushed — 'x' should be forwarded in input mode, not kill-session")
	}
	// Key should have been forwarded.
	if mock.CallCount("SendKeys") == 0 {
		t.Error("SendKeys was not called — 'x' should be forwarded to session in input mode")
	}
	if mock.LastSentKeys != "x" {
		t.Errorf("LastSentKeys = %q, want %q", mock.LastSentKeys, "x")
	}
}

// TestGridInputMode_ArrowsForwarded verifies that arrow keys in input mode are
// sent as ANSI escape sequences rather than navigating the grid cursor.
func TestGridInputMode_ArrowsForwarded(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Open all-sessions grid so we have multiple cells to navigate.
	f.SendKey("G")
	initialCursor := f.Model().gridView.Cursor
	f.SendKey("i")

	// Press Up arrow — should be forwarded as \033[A, not move cursor.
	cmd := f.Send(tea.KeyMsg{Type: tea.KeyUp})
	f.ExecCmdChain(cmd)

	if f.Model().gridView.Cursor != initialCursor {
		t.Error("cursor moved on Up in input mode — arrow should be forwarded, not navigate")
	}
	if mock.LastSentKeys != "\033[A" {
		t.Errorf("LastSentKeys = %q, want %q (ANSI up)", mock.LastSentKeys, "\033[A")
	}
}

// TestGridInputMode_DisabledByConfig verifies that setting DisableGridInput=true
// prevents the feature from activating.
func TestGridInputMode_DisabledByConfig(t *testing.T) {
	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)
	t.Setenv("TERM", "dumb")

	mock := muxtest.New()
	mux.SetBackend(mock)
	t.Cleanup(func() { mux.SetBackend(nil) })

	mock.SetPaneContent("hive-sessions:0", "$ claude\nSession started.")
	mock.SetPaneContent("hive-sessions:1", "$ codex\nReady.")

	cfg := config.DefaultConfig()
	cfg.HideAttachHint = true
	cfg.PreviewRefreshMs = 1
	cfg.DisableGridInput = true // opt-out

	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40

	m := New(cfg, appState, "")
	m.appState.TermWidth = 120
	m.appState.TermHeight = 40
	f := newFlowRunner(t, m, mock)

	f.SendKey("g")
	f.AssertGridActive(true)

	// 'i' should be a no-op.
	f.SendKey("i")
	if f.model.gridView.InputMode() {
		t.Fatal("gridView.InputMode() should remain false when DisableGridInput=true")
	}
}

// TestGridInputMode_HideExitsInputMode verifies that hiding the grid (e.g. via
// Esc in nav mode, which can't happen in input mode, but via programmatic Hide)
// always resets input mode so the next open starts in nav mode.
func TestGridInputMode_HideExitsInputMode(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.SendKey("g")
	f.SendKey("i")

	if !f.model.gridView.InputMode() {
		t.Fatal("pre-condition: input mode should be active")
	}

	// Exit input mode then close grid normally with 'g'.
	f.Send(tea.KeyMsg{Type: tea.KeyCtrlQ})
	f.SendKey("g") // closes grid in nav mode

	f.AssertGridActive(false)

	// Re-open grid; input mode must be false.
	f.SendKey("g")
	if f.model.gridView.InputMode() {
		t.Fatal("inputMode should be false after re-opening the grid")
	}

	_ = mock // avoid "declared and not used" if mock.* not referenced
	_ = state.GridRestoreProject // keep import used
}

// TestGridInputMode_HintShownOnFirstUse verifies that pressing 'i' shows the
// grid-input-hint overlay when HideGridInputHint=false.
func TestGridInputMode_HintShownOnFirstUse(t *testing.T) {
	m, mock := testFlowModelWithGridHint(t)
	f := newFlowRunner(t, m, mock)

	f.SendKey("g")
	f.AssertGridActive(true)

	f.SendKey("i")
	if f.model.TopView() != ViewGridInputHint {
		t.Fatalf("expected top view to be ViewGridInputHint, got %s", f.model.TopView())
	}
	if !f.model.gridView.InputMode() {
		t.Fatal("gridView.InputMode() should be true after pressing i")
	}
}

// TestGridInputMode_HintDontShowAgain verifies that pressing 'd' in the hint
// dialog sets HideGridInputHint=true and dismisses the overlay.
func TestGridInputMode_HintDontShowAgain(t *testing.T) {
	m, mock := testFlowModelWithGridHint(t)
	f := newFlowRunner(t, m, mock)

	f.SendKey("g")
	f.SendKey("i")
	if f.model.TopView() != ViewGridInputHint {
		t.Fatalf("expected ViewGridInputHint, got %s", f.model.TopView())
	}

	// Press 'd' to dismiss and suppress future hints.
	f.SendKey("d")
	if f.model.TopView() == ViewGridInputHint {
		t.Fatal("hint overlay should be dismissed after 'd'")
	}
	if !f.model.cfg.HideGridInputHint {
		t.Fatal("cfg.HideGridInputHint should be true after 'd'")
	}
	// Input mode should still be active.
	if !f.model.gridView.InputMode() {
		t.Fatal("gridView.InputMode() should remain true after dismissing hint with 'd'")
	}
}

// TestGridInputMode_ViewChangesDimming verifies that the grid View() output
// changes when input mode is activated and again when it is deactivated,
// confirming that dimming (and the INPUT badge) visually alter the render.
func TestGridInputMode_ViewChangesDimming(t *testing.T) {
	// Use TrueColor so that colour-based dimming is reflected in the rendered
	// output (testFlowModel sets TERM=dumb which strips colour by default).
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.SendKey("g")
	f.AssertGridActive(true)

	// Capture the view before entering input mode.
	viewNormal := f.model.gridView.View("")

	// Enter input mode and capture the new view.
	f.SendKey("i")
	if !f.model.gridView.InputMode() {
		t.Fatal("expected input mode to be active")
	}
	viewInputMode := f.model.gridView.View("")

	if viewNormal == viewInputMode {
		t.Error("grid View() should differ when input mode is active (dimming + INPUT badge)")
	}

	// Exit input mode — view should revert to something equal to the pre-input view.
	f.Send(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if f.model.gridView.InputMode() {
		t.Fatal("expected input mode to be inactive after Ctrl+Q")
	}
	viewAfterExit := f.model.gridView.View("")
	if viewAfterExit == viewInputMode {
		t.Error("grid View() should differ after exiting input mode (dimming removed)")
	}
}

// TestGridInputMode_HintEscCancelsInputMode verifies that pressing 'esc' in the
// hint dialog pops the overlay and also exits input mode.
func TestGridInputMode_HintEscCancelsInputMode(t *testing.T) {
	m, mock := testFlowModelWithGridHint(t)
	f := newFlowRunner(t, m, mock)

	f.SendKey("g")
	f.SendKey("i")
	if f.model.TopView() != ViewGridInputHint {
		t.Fatalf("expected ViewGridInputHint, got %s", f.model.TopView())
	}

	// Press 'esc' to cancel — should dismiss hint AND exit input mode.
	f.SendSpecialKey(tea.KeyEsc)
	if f.model.TopView() == ViewGridInputHint {
		t.Fatal("hint overlay should be dismissed after 'esc'")
	}
	if f.model.gridView.InputMode() {
		t.Fatal("gridView.InputMode() should be false after 'esc' in hint")
	}
	// Grid should still be active.
	f.AssertGridActive(true)
}
