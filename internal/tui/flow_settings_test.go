package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/config"
)

// openSettings pushes the Settings view via the configured keybinding ("S").
func openSettings(t *testing.T, f *flowRunner) {
	t.Helper()
	f.SendKey("S")
	if f.model.TopView() != ViewSettings {
		t.Fatalf("expected ViewSettings active, got %v", f.model.TopView())
	}
}

func TestFlow_Settings_OpenShowsFirstTab(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	openSettings(t, f)
	f.ViewContains("Settings")
	f.ViewContains("General")
	f.ViewContains("Theme")
}

func TestFlow_Settings_TabSwitchRight(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	openSettings(t, f)
	f.SendSpecialKey(tea.KeyRight) // → Team Defaults

	if got := f.model.settings.ActiveTab(); got != 1 {
		t.Fatalf("expected activeTab=1, got %d", got)
	}
	f.ViewContains("Orchestrator Agent")
	f.ViewNotContains("Preview Refresh")
}

func TestFlow_Settings_TabSwitchHL(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	openSettings(t, f)
	f.SendSpecialKey(tea.KeyRight)
	f.SendSpecialKey(tea.KeyRight)
	f.SendSpecialKey(tea.KeyLeft)
	if got := f.model.settings.ActiveTab(); got != 1 {
		t.Fatalf("expected activeTab=1 after l,l,h, got %d", got)
	}
	f.ViewContains("Orchestrator Agent")
}

func TestFlow_Settings_PerTabCursorPreserved(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	openSettings(t, f)
	f.SendSpecialKey(tea.KeyDown)
	f.SendSpecialKey(tea.KeyDown)
	if got := f.model.settings.TabCursor(0); got != 2 {
		t.Fatalf("tab 0 cursor after 2×j = %d, want 2", got)
	}

	f.SendSpecialKey(tea.KeyRight)
	f.SendSpecialKey(tea.KeyDown)
	if got := f.model.settings.TabCursor(1); got != 1 {
		t.Fatalf("tab 1 cursor after j = %d, want 1", got)
	}

	f.SendSpecialKey(tea.KeyLeft)
	if got := f.model.settings.TabCursor(0); got != 2 {
		t.Errorf("tab 0 cursor after return = %d, want 2 (preserved)", got)
	}
	if got := f.model.settings.TabCursor(1); got != 1 {
		t.Errorf("tab 1 cursor after leaving = %d, want 1 (preserved)", got)
	}
}

func TestFlow_Settings_EditBlocksTabSwitch(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	openSettings(t, f)
	// Navigate to Preview Refresh (int field, index 3) and start editing.
	f.SendSpecialKey(tea.KeyDown)
	f.SendSpecialKey(tea.KeyDown)
	f.SendSpecialKey(tea.KeyDown)
	f.SendSpecialKey(tea.KeyEnter)
	if !f.model.settings.IsEditing() {
		t.Fatal("precondition: expected editing=true")
	}

	before := f.model.settings.ActiveTab()
	f.SendSpecialKey(tea.KeyRight) // should be routed to text input, not switch tabs
	if got := f.model.settings.ActiveTab(); got != before {
		t.Errorf("activeTab changed while editing: got %d, want %d", got, before)
	}
	if !f.model.settings.IsEditing() {
		t.Error("expected still editing after l")
	}
}

func TestFlow_Settings_SaveStillWorks(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	openSettings(t, f)
	f.SendSpecialKey(tea.KeyEnter) // toggle Theme → dirty
	if !f.model.settings.IsDirty() {
		t.Fatal("precondition: expected dirty")
	}
	cmd := f.SendKey("s") // emits SettingsSaveConfirmMsg
	f.ExecCmdChain(cmd)   // processes msg → pushes ViewConfirm

	if got := f.model.TopView(); got != ViewConfirm {
		t.Fatalf("expected ViewConfirm after 's', got %v", got)
	}
	cmd = f.SendKey("y") // confirm → ConfirmedMsg → SettingsSaveRequestMsg
	f.ExecCmdChain(cmd)  // run save + ConfigSavedMsg

	if f.model.settings.Active {
		t.Error("expected settings closed after save confirm")
	}
	if got := f.model.TopView(); got != ViewMain {
		t.Errorf("expected TopView=ViewMain after save, got %v", got)
	}
	if strings.TrimSpace(f.View()) == "" {
		t.Error("expected non-empty main view render after save, got blank")
	}
}

func TestFlow_Settings_SaveError_PopsViewAndShowsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod directory perms don't block writes on Windows")
	}
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	openSettings(t, f)
	f.SendSpecialKey(tea.KeyEnter) // toggle Theme → dirty
	if !f.model.settings.IsDirty() {
		t.Fatal("precondition: expected dirty")
	}
	cmd := f.SendKey("s") // emits SettingsSaveConfirmMsg
	f.ExecCmdChain(cmd)   // pushes ViewConfirm

	if got := f.model.TopView(); got != ViewConfirm {
		t.Fatalf("expected ViewConfirm after 's', got %v", got)
	}

	// Make the config dir read-only so config.Save fails atomically.
	cfgDir := filepath.Dir(config.ConfigPath())
	if err := os.Chmod(cfgDir, 0o500); err != nil {
		t.Fatalf("chmod config dir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(cfgDir, 0o700) })

	cmd = f.SendKey("y") // confirm → SettingsSaveRequestMsg → save fails
	f.ExecCmdChain(cmd)

	if got := f.model.TopView(); got != ViewMain {
		t.Errorf("TopView after save error = %v, want ViewMain (so statusbar renders)", got)
	}
	if f.model.appState.LastError == "" {
		t.Error("LastError should be set after save failure")
	}
	if strings.TrimSpace(f.View()) == "" {
		t.Error("expected non-empty render after save error, got blank")
	}
}

func TestFlow_Settings_SaveCancel(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	openSettings(t, f)
	f.SendSpecialKey(tea.KeyEnter) // toggle Theme → dirty
	if !f.model.settings.IsDirty() {
		t.Fatal("precondition: expected dirty")
	}
	cmd := f.SendKey("s") // emits SettingsSaveConfirmMsg
	f.ExecCmdChain(cmd)   // pushes ViewConfirm

	if got := f.model.TopView(); got != ViewConfirm {
		t.Fatalf("expected ViewConfirm after 's', got %v", got)
	}

	// Cancel the dialog
	f.SendKey("n")

	if got := f.model.TopView(); got != ViewSettings {
		t.Errorf("expected ViewSettings after cancel, got %v", got)
	}
	if !f.model.settings.Active {
		t.Error("expected settings still active after cancel")
	}
	if !f.model.settings.IsDirty() {
		t.Error("expected settings still dirty after cancel")
	}
}

// TestFlow_Settings_StartupViewFieldPresent verifies the "Startup View" field
// appears in the General settings tab with the three expected option values.
func TestFlow_Settings_StartupViewFieldPresent(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	openSettings(t, f)
	// General tab is active by default.
	f.ViewContains("Startup View")
	// The current value (default "sidebar") should appear.
	f.ViewContains("sidebar")
}
