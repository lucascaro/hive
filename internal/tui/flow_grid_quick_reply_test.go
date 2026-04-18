package tui

import (
	"testing"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/mux/muxtest"
)

// TestGridQuickReply_SendsDigitAndEnter verifies that pressing a digit key
// on a focused session sends that digit + newline to the session.
func TestGridQuickReply_SendsDigitAndEnter(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.SendKey("g")
	f.AssertGridActive(true)

	sel := f.model.gridView.Selected()
	if sel == nil {
		t.Fatal("expected a selected session in grid view")
	}

	// Press '1' — should send "1\n" to the session regardless of status.
	cmd := f.SendKey("1")
	f.ExecCmdChain(cmd)

	if mock.CallCount("SendKeys") != 1 {
		t.Errorf("SendKeys call count = %d, want 1", mock.CallCount("SendKeys"))
	}
	if mock.LastSentKeys != "1\n" {
		t.Errorf("LastSentKeys = %q, want %q", mock.LastSentKeys, "1\n")
	}
}

// TestGridQuickReply_IgnoredInInputMode verifies that digit keys in input mode
// are forwarded as plain digits (not digit+Enter), per the existing input mode behavior.
func TestGridQuickReply_IgnoredInInputMode(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.SendKey("g")
	f.AssertGridActive(true)

	// Enter input mode.
	f.SendKey("i")
	if !f.model.gridView.InputMode() {
		t.Fatal("should be in input mode after pressing i")
	}

	// Press '2' in input mode — should be forwarded as plain "2", not "2\n".
	cmd := f.SendKey("2")
	f.ExecCmdChain(cmd)

	if mock.LastSentKeys != "2" {
		t.Errorf("LastSentKeys = %q, want %q (plain digit in input mode)", mock.LastSentKeys, "2")
	}
}

// TestGridQuickReply_DisabledByConfig verifies that setting DisableQuickReply=true
// prevents the quick-reply feature from activating.
func TestGridQuickReply_DisabledByConfig(t *testing.T) {
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
	cfg.HideGridInputHint = true
	cfg.PreviewRefreshMs = 1
	cfg.DisableQuickReply = true

	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40

	m := New(cfg, appState, "")
	m.appState.TermWidth = 120
	m.appState.TermHeight = 40
	f := newFlowRunner(t, m, mock)

	f.SendKey("g")
	f.AssertGridActive(true)

	cmd := f.SendKey("1")
	f.ExecCmdChain(cmd)

	if mock.CallCount("SendKeys") != 0 {
		t.Errorf("SendKeys should not be called when DisableQuickReply=true, got %d calls", mock.CallCount("SendKeys"))
	}
}

// TestGridQuickReply_AllDigits verifies that all digits 1-9 work for quick-reply.
func TestGridQuickReply_AllDigits(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.SendKey("g")
	f.AssertGridActive(true)

	for _, digit := range "123456789" {
		mock.ResetCounts()
		cmd := f.SendKey(string(digit))
		f.ExecCmdChain(cmd)

		expected := string(digit) + "\n"
		if mock.LastSentKeys != expected {
			t.Errorf("digit %c: LastSentKeys = %q, want %q", digit, mock.LastSentKeys, expected)
		}
	}
}

// TestGridQuickReply_ZeroNotHandled verifies that '0' does not trigger quick-reply
// (only 1-9 are valid).
func TestGridQuickReply_ZeroNotHandled(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.SendKey("g")
	f.AssertGridActive(true)

	cmd := f.SendKey("0")
	f.ExecCmdChain(cmd)

	if mock.CallCount("SendKeys") != 0 {
		t.Errorf("SendKeys should not be called for '0', got %d calls", mock.CallCount("SendKeys"))
	}
}
