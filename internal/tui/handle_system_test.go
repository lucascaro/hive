package tui

import (
	"testing"
)

func TestHandleConfigSaved_PopsSettingsView(t *testing.T) {
	m, _ := testFlowModel(t)
	m.appState.LastError = "prior error"
	m.PushView(ViewSettings)

	result, cmd := m.handleConfigSaved()
	m = result.(Model)
	if cmd != nil {
		t.Errorf("expected nil cmd, got %v", cmd)
	}
	if got := m.TopView(); got != ViewMain {
		t.Errorf("TopView = %v, want ViewMain", got)
	}
	if m.settings.Active {
		t.Error("settings.Active = true, want false after save")
	}
	if m.appState.LastError != "" {
		t.Errorf("LastError = %q, want empty", m.appState.LastError)
	}
}

func TestHandleConfigSaved_NoopWhenSettingsNotOnTop(t *testing.T) {
	m, _ := testFlowModel(t)
	m.appState.LastError = "prior error"
	// Stack stays at ViewMain; e.g. config reloaded externally.

	result, _ := m.handleConfigSaved()
	m = result.(Model)
	if got := m.TopView(); got != ViewMain {
		t.Errorf("TopView = %v, want ViewMain", got)
	}
	if m.appState.LastError != "" {
		t.Errorf("LastError = %q, want empty", m.appState.LastError)
	}
}
