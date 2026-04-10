package components

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lucascaro/hive/internal/config"
)

func testConfig() config.Config {
	return config.Config{
		Theme:            "dark",
		Multiplexer:      "tmux",
		PreviewRefreshMs: 500,
		Hooks: config.HooksConfig{
			Enabled: false,
			Dir:     "~/.config/hive/hooks",
		},
		Keybindings: config.KeybindingsConfig{
			NewSession: "t",
			Help:       "?",
		},
		TeamDefaults: config.TeamDefaultsConfig{
			Orchestrator: "claude",
			WorkerCount:  2,
			WorkerAgent:  "claude",
		},
	}
}

func TestSettingsView_OpenAndClose(t *testing.T) {
	sv := NewSettingsView()
	cfg := testConfig()
	sv.Open(cfg)
	if !sv.Active {
		t.Error("expected Active=true after Open")
	}
	if sv.IsDirty() {
		t.Error("expected not dirty after Open")
	}
	sv.Close()
	if sv.Active {
		t.Error("expected Active=false after Close")
	}
}

func TestSettingsView_InactiveConsumesFalse(t *testing.T) {
	sv := NewSettingsView()
	_, consumed := sv.Update(keyPress("j"))
	if consumed {
		t.Error("expected consumed=false when inactive")
	}
}

func TestSettingsView_CursorNavigation(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	if sv.cursor != 0 {
		t.Errorf("initial cursor=%d, want 0", sv.cursor)
	}

	sv.Update(keyPress("j"))
	if sv.cursor != 1 {
		t.Errorf("after j: cursor=%d, want 1", sv.cursor)
	}

	sv.Update(keyPress("k"))
	if sv.cursor != 0 {
		t.Errorf("after k: cursor=%d, want 0", sv.cursor)
	}

	// Clamp at top
	sv.Update(keyPress("k"))
	if sv.cursor != 0 {
		t.Errorf("clamp top: cursor=%d, want 0", sv.cursor)
	}
}

func TestSettingsView_BoolToggle(t *testing.T) {
	sv := NewSettingsView()
	cfg := testConfig()
	cfg.HideAttachHint = false
	sv.Open(cfg)

	// Navigate to HideAttachHint (index 4 among fields: Theme=0, Multiplexer=1, PreviewRefreshMs=2, AgentTitleOverrides=3, HideAttachHint=4)
	for i := 0; i < 4; i++ {
		sv.Update(keyPress("j"))
	}

	// Toggle on
	sv.Update(keyType(tea.KeyEnter))
	if !sv.IsDirty() {
		t.Error("expected dirty after toggle")
	}
	got := sv.GetConfig().HideAttachHint
	if !got {
		t.Error("expected HideAttachHint=true after toggle")
	}

	// Toggle off
	sv.Update(keyPress(" "))
	if sv.GetConfig().HideAttachHint {
		t.Error("expected HideAttachHint=false after second toggle")
	}
}

func TestSettingsView_SelectCycle(t *testing.T) {
	sv := NewSettingsView()
	cfg := testConfig()
	cfg.Theme = "dark"
	sv.Open(cfg)

	// Theme is the first field (cursor=0), options: dark, light
	sv.Update(keyType(tea.KeyEnter))
	if sv.GetConfig().Theme != "light" {
		t.Errorf("expected theme=light after cycle, got %s", sv.GetConfig().Theme)
	}
	sv.Update(keyType(tea.KeyEnter))
	if sv.GetConfig().Theme != "dark" {
		t.Errorf("expected theme=dark after wraparound, got %s", sv.GetConfig().Theme)
	}
}

func TestSettingsView_IntValidation(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	// Navigate to PreviewRefreshMs (index 2)
	sv.Update(keyPress("j"))
	sv.Update(keyPress("j"))

	// Start editing
	sv.Update(keyType(tea.KeyEnter))
	if !sv.editing {
		t.Fatal("expected editing=true")
	}

	// Try invalid value: 49 (below min 50)
	sv.editInput.SetValue("49")
	sv.Update(keyType(tea.KeyEnter))
	if sv.editErr == "" {
		t.Error("expected validation error for 49")
	}

	// editErr should be set but editing continues
	if !sv.editing {
		t.Error("expected still editing after validation error")
	}

	// Try valid value
	sv.editInput.SetValue("100")
	sv.Update(keyType(tea.KeyEnter))
	if sv.editing {
		t.Error("expected editing=false after valid input")
	}
	if sv.GetConfig().PreviewRefreshMs != 100 {
		t.Errorf("expected PreviewRefreshMs=100, got %d", sv.GetConfig().PreviewRefreshMs)
	}

	// Re-enter editing, try 30001 (above max 30000)
	sv.Update(keyType(tea.KeyEnter))
	sv.editInput.SetValue("30001")
	sv.Update(keyType(tea.KeyEnter))
	if sv.editErr == "" {
		t.Error("expected validation error for 30001")
	}
}

func TestSettingsView_StringValidation_EmptyKeybinding(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	// Navigate to a keybinding field. Keybindings start after the headers and
	// earlier fields. Let's find "Toggle Collapse" which is the first keybinding.
	// Fields: Theme(0), Mux(1), Refresh(2), AgentTitle(3), HideAttach(4),
	// Orch(5), WorkerCount(6), WorkerAgent(7), HooksEnabled(8), HooksDir(9),
	// ToggleCollapse(10)
	for i := 0; i < 10; i++ {
		sv.Update(keyPress("j"))
	}

	// Start editing
	sv.Update(keyType(tea.KeyEnter))
	sv.editInput.SetValue("")
	sv.Update(keyType(tea.KeyEnter))
	if sv.editErr == "" {
		t.Error("expected error for empty keybinding")
	}
}

func TestSettingsView_StringValidation_EmptyHooksDir(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	// Navigate to HooksDir (index 9)
	for i := 0; i < 9; i++ {
		sv.Update(keyPress("j"))
	}

	sv.Update(keyType(tea.KeyEnter))
	sv.editInput.SetValue("")
	sv.Update(keyType(tea.KeyEnter))
	if sv.editErr == "" {
		t.Error("expected error for empty hooks dir")
	}
}

func TestSettingsView_DirtyTracking(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	if sv.IsDirty() {
		t.Error("should not be dirty initially")
	}

	// Toggle theme (select field)
	sv.Update(keyType(tea.KeyEnter))
	if !sv.IsDirty() {
		t.Error("should be dirty after change")
	}
}

func TestSettingsView_EscCleanCloses(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	cmd, consumed := sv.Update(keyType(tea.KeyEscape))
	if !consumed {
		t.Error("expected consumed=true")
	}
	if sv.Active {
		t.Error("expected Active=false for clean esc")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(SettingsClosedMsg); !ok {
		t.Errorf("expected SettingsClosedMsg, got %T", msg)
	}
}

func TestSettingsView_EscDirtyRequiresTwoEscapes(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	// Make dirty
	sv.Update(keyType(tea.KeyEnter)) // toggle theme

	// First esc → pending discard
	sv.Update(keyType(tea.KeyEscape))
	if !sv.Active {
		t.Error("expected still active after first esc with dirty state")
	}
	if !sv.pendingDiscard {
		t.Error("expected pendingDiscard=true")
	}

	// Second esc → close
	cmd, _ := sv.Update(keyType(tea.KeyEscape))
	if sv.Active {
		t.Error("expected Active=false after second esc")
	}
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	msg := cmd()
	if _, ok := msg.(SettingsClosedMsg); !ok {
		t.Errorf("expected SettingsClosedMsg, got %T", msg)
	}
}

func TestSettingsView_PendingDiscardClearedByOtherKey(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	// Make dirty and trigger pending discard
	sv.Update(keyType(tea.KeyEnter))
	sv.Update(keyType(tea.KeyEscape))
	if !sv.pendingDiscard {
		t.Fatal("precondition: pendingDiscard should be true")
	}

	// Another key should clear pendingDiscard
	sv.Update(keyPress("j"))
	if sv.pendingDiscard {
		t.Error("expected pendingDiscard=false after other key")
	}
}

func TestSettingsView_SaveFlow(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	// Make dirty
	sv.Update(keyType(tea.KeyEnter)) // toggle theme

	// Press s → pending save
	sv.Update(keyPress("s"))
	if !sv.pendingSave {
		t.Error("expected pendingSave=true")
	}

	// Confirm with y
	cmd, consumed := sv.Update(keyPress("y"))
	if !consumed {
		t.Error("expected consumed=true")
	}
	if sv.Active {
		t.Error("expected Active=false after save confirm")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	save, ok := msg.(SettingsSaveRequestMsg)
	if !ok {
		t.Fatalf("expected SettingsSaveRequestMsg, got %T", msg)
	}
	if save.Config.Theme != "light" {
		t.Errorf("expected saved theme=light, got %s", save.Config.Theme)
	}
}

func TestSettingsView_SaveCancelledByOtherKey(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	sv.Update(keyType(tea.KeyEnter)) // make dirty
	sv.Update(keyPress("s"))                 // pending save

	sv.Update(keyPress("n")) // cancel save
	if sv.pendingSave {
		t.Error("expected pendingSave=false after cancel")
	}
	if !sv.Active {
		t.Error("expected still active after save cancel")
	}
}

func TestSettingsView_SCleanCloses(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	// s with no dirty state should close
	cmd, _ := sv.Update(keyPress("s"))
	if sv.Active {
		t.Error("expected Active=false when s with clean state")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(SettingsClosedMsg); !ok {
		t.Errorf("expected SettingsClosedMsg, got %T", msg)
	}
}

func TestSettingsView_EditEscCancels(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	// Navigate to PreviewRefreshMs (int field, index 2)
	sv.Update(keyPress("j"))
	sv.Update(keyPress("j"))

	// Start editing
	sv.Update(keyType(tea.KeyEnter))
	if !sv.editing {
		t.Fatal("expected editing=true")
	}

	// Esc should cancel editing without changing value
	sv.Update(keyType(tea.KeyEscape))
	if sv.editing {
		t.Error("expected editing=false after esc")
	}
	if sv.GetConfig().PreviewRefreshMs != 500 {
		t.Errorf("expected unchanged PreviewRefreshMs=500, got %d", sv.GetConfig().PreviewRefreshMs)
	}
}
