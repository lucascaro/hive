package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/mux/muxtest"
)

const testWhatsNewContent = `## [0.6.0] — 2026-04-11

### Added
- Reorder items via keyboard

### Fixed
- Terminal bell forwarding`

// testFlowModelWithWhatsNew creates a Model with whatsNewContent set.
func testFlowModelWithWhatsNew(t *testing.T, content string) (Model, *muxtest.MockBackend) {
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
	cfg.HideAttachHint = true
	cfg.PreviewRefreshMs = 1

	appState := testAppStateWithTwoProjects()
	appState.TermWidth = 120
	appState.TermHeight = 40

	m := New(cfg, appState, content)
	m.appState.TermWidth = 120
	m.appState.TermHeight = 40

	return m, mock
}

func TestFlow_WhatsNew_ShownOnVersionChange(t *testing.T) {
	m, mock := testFlowModelWithWhatsNew(t, testWhatsNewContent)
	f := newFlowRunner(t, m, mock)

	model := f.Model()
	if !model.HasView(ViewWhatsNew) {
		t.Fatal("ViewWhatsNew should be in view stack")
	}
	f.ViewContains("What's New")
	f.ViewContains("Reorder items")
}

func TestFlow_WhatsNew_DismissWithEnter(t *testing.T) {
	m, mock := testFlowModelWithWhatsNew(t, testWhatsNewContent)
	f := newFlowRunner(t, m, mock)

	f.SendSpecialKey(tea.KeyEnter)

	model := f.Model()
	if model.HasView(ViewWhatsNew) {
		t.Fatal("ViewWhatsNew should be dismissed after Enter")
	}
}

func TestFlow_WhatsNew_DismissWithEsc(t *testing.T) {
	m, mock := testFlowModelWithWhatsNew(t, testWhatsNewContent)
	f := newFlowRunner(t, m, mock)

	f.SendSpecialKey(tea.KeyEscape)

	model := f.Model()
	if model.HasView(ViewWhatsNew) {
		t.Fatal("ViewWhatsNew should be dismissed after Esc")
	}
}

func TestFlow_WhatsNew_DontShowAgain(t *testing.T) {
	m, mock := testFlowModelWithWhatsNew(t, testWhatsNewContent)
	f := newFlowRunner(t, m, mock)

	f.SendKey("d")

	model := f.Model()
	if model.HasView(ViewWhatsNew) {
		t.Fatal("ViewWhatsNew should be dismissed after 'd'")
	}
	if !model.cfg.HideWhatsNew {
		t.Fatal("cfg.HideWhatsNew should be true after pressing 'd'")
	}
}

func TestFlow_WhatsNew_NotShownWhenEmpty(t *testing.T) {
	m, mock := testFlowModelWithWhatsNew(t, "")
	f := newFlowRunner(t, m, mock)

	model := f.Model()
	if model.HasView(ViewWhatsNew) {
		t.Fatal("ViewWhatsNew should not be shown with empty content")
	}
}

func TestFlow_WhatsNew_ScrollUpDown(t *testing.T) {
	// Use a long content string to ensure scrolling is possible.
	longContent := testWhatsNewContent + "\n\n" +
		"### Changed\n- Line 1\n- Line 2\n- Line 3\n- Line 4\n- Line 5\n" +
		"- Line 6\n- Line 7\n- Line 8\n- Line 9\n- Line 10\n" +
		"- Line 11\n- Line 12\n- Line 13\n- Line 14\n- Line 15\n" +
		"- Line 16\n- Line 17\n- Line 18\n- Line 19\n- Line 20\n" +
		"- Line 21\n- Line 22\n- Line 23\n- Line 24\n- Line 25\n"

	m, mock := testFlowModelWithWhatsNew(t, longContent)
	f := newFlowRunner(t, m, mock)

	model := f.Model()
	if !model.HasView(ViewWhatsNew) {
		t.Fatal("ViewWhatsNew should be in view stack")
	}

	initialOffset := f.Model().whatsNewViewport.YOffset

	// Scroll down.
	f.SendKey("j")
	afterDown := f.Model().whatsNewViewport.YOffset
	if afterDown <= initialOffset {
		t.Errorf("YOffset after j: %d, expected > %d", afterDown, initialOffset)
	}

	// Scroll back up.
	f.SendKey("k")
	afterUp := f.Model().whatsNewViewport.YOffset
	if afterUp >= afterDown {
		t.Errorf("YOffset after k: %d, expected < %d", afterUp, afterDown)
	}
}
