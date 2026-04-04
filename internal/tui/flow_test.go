package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/mux/muxtest"
	"github.com/lucascaro/hive/internal/state"
)

// flowRunner drives a Model through a sequence of tea.Msg dispatches,
// providing helpers for state and visual assertions at each step.
type flowRunner struct {
	t     *testing.T
	model Model
	mock  *muxtest.MockBackend
}

// newFlowRunner wraps a Model for multi-step testing.
func newFlowRunner(t *testing.T, m Model, mock *muxtest.MockBackend) *flowRunner {
	t.Helper()
	return &flowRunner{t: t, model: m, mock: mock}
}

// Send dispatches msg through Update and stores the resulting Model.
// Returns the tea.Cmd from Update (may be nil).
func (f *flowRunner) Send(msg tea.Msg) tea.Cmd {
	f.t.Helper()
	result, cmd := f.model.Update(msg)
	f.model = result.(Model)
	return cmd
}

// SendKey sends a single rune key press. For special keys (Enter, Esc, etc.)
// use SendSpecialKey instead.
func (f *flowRunner) SendKey(key string) tea.Cmd {
	f.t.Helper()
	runes := []rune(key)
	if len(runes) != 1 {
		f.t.Fatalf("SendKey expects a single rune, got %q; use SendSpecialKey for Enter/Esc/etc", key)
	}
	return f.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: runes})
}

// SendSpecialKey sends a special key (Enter, Esc, etc.).
func (f *flowRunner) SendSpecialKey(key tea.KeyType) tea.Cmd {
	f.t.Helper()
	return f.Send(tea.KeyMsg{Type: key})
}

// SendAndExec dispatches msg, executes the returned cmd (if non-nil),
// and feeds the result back through Update. Returns the final cmd.
// Skips feeding back tea.BatchMsg (would recurse). Note: does NOT guard
// against tick/timer cmds — avoid calling this when the returned cmd
// could be a tea.Tick (use Send + manual cmd execution instead).
func (f *flowRunner) SendAndExec(msg tea.Msg) tea.Cmd {
	f.t.Helper()
	cmd := f.Send(msg)
	if cmd == nil {
		return nil
	}
	resultMsg := cmd()
	if resultMsg == nil {
		return nil
	}
	// Don't feed back batch messages or ticks — they'd recurse or block.
	switch resultMsg.(type) {
	case tea.BatchMsg:
		return cmd
	}
	return f.Send(resultMsg)
}

// ExecCmdChain executes a cmd and feeds results back through Update,
// handling tea.BatchMsg by dispatching each sub-cmd. Skips cmds that
// block (e.g. tea.Tick). Stops at tea.QuitMsg or when all cmds are
// exhausted. Max 50 iterations to prevent infinite loops.
func (f *flowRunner) ExecCmdChain(cmd tea.Cmd) {
	f.t.Helper()
	queue := []tea.Cmd{cmd}
	for i := 0; i < 50 && len(queue) > 0; i++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		// Execute with a short timeout to skip blocking cmds (tea.Tick).
		ch := make(chan tea.Msg, 1)
		go func() { ch <- c() }()
		select {
		case msg := <-ch:
			if msg == nil {
				continue
			}
			if _, ok := msg.(tea.QuitMsg); ok {
				return
			}
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, sub := range batch {
					queue = append(queue, sub)
				}
				continue
			}
			next := f.Send(msg)
			if next != nil {
				queue = append(queue, next)
			}
		case <-time.After(10 * time.Millisecond):
			// Cmd blocked (likely a tick/timer) — skip it.
		}
	}
}

// Model returns the current Model state for custom assertions.
func (f *flowRunner) Model() Model {
	return f.model
}

// View returns the current rendered output.
func (f *flowRunner) View() string {
	return f.model.View()
}

// Snapshot captures the current View() output and compares it against
// a golden file at testdata/TestName/name.golden. Use -update flag to regenerate.
func (f *flowRunner) Snapshot(name string) {
	f.t.Helper()
	f.t.Run(name, func(t *testing.T) {
		t.Helper()
		golden.RequireEqual(t, f.model.View())
	})
}

// ViewContains asserts that View() output contains substr.
func (f *flowRunner) ViewContains(substr string) {
	f.t.Helper()
	view := f.model.View()
	if !strings.Contains(view, substr) {
		f.t.Errorf("View() does not contain %q\n--- View output (first 500 chars) ---\n%.500s", substr, view)
	}
}

// ViewNotContains asserts that View() output does NOT contain substr.
func (f *flowRunner) ViewNotContains(substr string) {
	f.t.Helper()
	view := f.model.View()
	if strings.Contains(view, substr) {
		f.t.Errorf("View() unexpectedly contains %q", substr)
	}
}

// AssertGridActive asserts whether the grid view is active.
func (f *flowRunner) AssertGridActive(active bool) {
	f.t.Helper()
	if f.model.gridView.Active != active {
		f.t.Errorf("gridView.Active = %v, want %v", f.model.gridView.Active, active)
	}
}

// AssertGridMode asserts the grid restore mode.
func (f *flowRunner) AssertGridMode(mode state.GridRestoreMode) {
	f.t.Helper()
	if f.model.gridView.Mode != mode {
		f.t.Errorf("gridView.Mode = %q, want %q", f.model.gridView.Mode, mode)
	}
}

// AssertActiveSession asserts the active session ID.
func (f *flowRunner) AssertActiveSession(id string) {
	f.t.Helper()
	if f.model.appState.ActiveSessionID != id {
		f.t.Errorf("ActiveSessionID = %q, want %q", f.model.appState.ActiveSessionID, id)
	}
}

// AssertInputMode asserts the current input mode.
func (f *flowRunner) AssertInputMode(mode string) {
	f.t.Helper()
	if f.model.inputMode != mode {
		f.t.Errorf("inputMode = %q, want %q", f.model.inputMode, mode)
	}
}

// testFlowModel creates a Model suitable for flow tests with:
// - config/state redirected to a temp dir
// - mock mux backend installed
// - TERM=dumb for deterministic rendering (no ANSI colors)
// - 2 projects with 1 session each, 120x40 terminal
// - HideAttachHint=true by default
func testFlowModel(t *testing.T) (Model, *muxtest.MockBackend) {
	t.Helper()

	// Redirect config/state to temp dir.
	tmp := t.TempDir()
	setHomePersist(t, tmp)
	ensureConfigDir(t)

	// Strip ANSI colors for deterministic golden files.
	t.Setenv("TERM", "dumb")

	// Install mock backend.
	mock := muxtest.New()
	mux.SetBackend(mock)
	t.Cleanup(func() { mux.SetBackend(nil) })

	// Pre-populate mock with the sessions that the state references.
	mock.SetPaneContent("hive-sessions:0", "$ claude\nSession started.")
	mock.SetPaneContent("hive-sessions:1", "$ codex\nReady.")

	cfg := config.DefaultConfig()
	cfg.HideAttachHint = true
	cfg.PreviewRefreshMs = 1 // near-zero tick interval to keep tests fast

	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40

	m := New(cfg, appState)
	m.appState.TermWidth = 120
	m.appState.TermHeight = 40

	return m, mock
}

// testFlowModelWithHint is like testFlowModel but with HideAttachHint=false.
func testFlowModelWithHint(t *testing.T) (Model, *muxtest.MockBackend) {
	t.Helper()

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
	cfg.HideAttachHint = false
	cfg.PreviewRefreshMs = 1 // near-zero tick interval to keep tests fast

	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40

	m := New(cfg, appState)
	m.appState.TermWidth = 120
	m.appState.TermHeight = 40

	return m, mock
}
