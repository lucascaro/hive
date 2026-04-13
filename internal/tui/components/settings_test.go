package components

import (
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucascaro/hive/internal/audio"
	"github.com/lucascaro/hive/internal/config"
)

func testConfig() config.Config {
	return config.Config{
		Theme:            "dark",
		StartupView:      "sidebar",
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

	if sv.cursor() != 0 {
		t.Errorf("initial cursor=%d, want 0", sv.cursor())
	}

	sv.Update(keyPress("j"))
	if sv.cursor() != 1 {
		t.Errorf("after j: cursor=%d, want 1", sv.cursor())
	}

	sv.Update(keyPress("k"))
	if sv.cursor() != 0 {
		t.Errorf("after k: cursor=%d, want 0", sv.cursor())
	}

	// Clamp at top
	sv.Update(keyPress("k"))
	if sv.cursor() != 0 {
		t.Errorf("clamp top: cursor=%d, want 0", sv.cursor())
	}
}

func TestSettingsView_BoolToggle(t *testing.T) {
	sv := NewSettingsView()
	cfg := testConfig()
	cfg.HideAttachHint = false
	sv.Open(cfg)

	// General tab fields: Theme(0), StartupView(1), Multiplexer(2), PreviewRefreshMs(3),
	// AgentTitleOverrides(4), HideAttachHint(5), HideWhatsNew(6)
	for i := 0; i < 5; i++ {
		sv.Update(keyPress("j"))
	}
	if f := sv.selectedField(); f == nil || f.label != "Hide Attach Hint" {
		t.Fatalf("expected field 'Hide Attach Hint', got %v", f)
	}

	// Toggle on
	sv.Update(keyType(tea.KeyEnter))
	if !sv.IsDirty() {
		t.Error("expected dirty after toggle")
	}
	if !sv.GetConfig().HideAttachHint {
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

	// Theme is the first field on the General tab (cursor=0), options: dark, light
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

	// Navigate to PreviewRefreshMs (General tab, index 3)
	sv.Update(keyPress("j"))
	sv.Update(keyPress("j"))
	sv.Update(keyPress("j"))
	if f := sv.selectedField(); f == nil || f.label != "Preview Refresh (ms)" {
		t.Fatalf("expected field 'Preview Refresh (ms)', got %v", f)
	}

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

// navigateToField presses "j" n times to reach field at index n.
func navigateToField(sv *SettingsView, n int) {
	for i := 0; i < n; i++ {
		sv.Update(keyPress("j"))
	}
}

func TestSettingsView_BellSoundOnChange_CallsAudioPlay(t *testing.T) {
	// Suppress real audio; count how many times Play is invoked.
	prev := audio.SyncForTest
	audio.SyncForTest = true
	var playCalls atomic.Int32
	restore := audio.SetTestHooks(nil, func(string, int) error { playCalls.Add(1); return nil })
	t.Cleanup(func() { audio.SyncForTest = prev; restore() })

	sv := NewSettingsView()
	cfg := testConfig()
	cfg.BellSound = "normal"
	cfg.BellVolume = 50
	sv.Open(cfg)

	// Bell Sound is field index 7 on the General tab.
	navigateToField(&sv, 7)
	if f := sv.selectedField(); f == nil || f.label != "Bell Sound" {
		t.Fatalf("expected field 'Bell Sound', got %v", f)
	}

	before := playCalls.Load()
	sv.Update(keyType(tea.KeyEnter)) // cycle: normal → bee
	after := playCalls.Load()

	if after-before != 1 {
		t.Errorf("onChange: playCalls delta = %d, want 1", after-before)
	}
	if sv.GetConfig().BellSound != "bee" {
		t.Errorf("BellSound = %q after cycle, want %q", sv.GetConfig().BellSound, "bee")
	}
}

func TestSettingsView_BellVolumeOnChange_CallsAudioPlay(t *testing.T) {
	prev := audio.SyncForTest
	audio.SyncForTest = true
	var lastVol atomic.Int32
	restore := audio.SetTestHooks(nil, func(_ string, vol int) error { lastVol.Store(int32(vol)); return nil })
	t.Cleanup(func() { audio.SyncForTest = prev; restore() })

	sv := NewSettingsView()
	cfg := testConfig()
	cfg.BellSound = "bee"
	cfg.BellVolume = 50
	sv.Open(cfg)

	// Bell Volume is field index 8 on the General tab.
	navigateToField(&sv, 8)
	if f := sv.selectedField(); f == nil || f.label != "Bell Volume" {
		t.Fatalf("expected field 'Bell Volume', got %v", f)
	}

	sv.Update(keyType(tea.KeyEnter)) // cycle: 50 → 75
	if got := sv.GetConfig().BellVolume; got != 75 {
		t.Errorf("BellVolume = %d after cycle, want 75", got)
	}
	if got := lastVol.Load(); got != 75 {
		t.Errorf("audio.Play received volume=%d, want 75", got)
	}
}

func TestSettingsView_StringValidation_EmptyKeybinding(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	// Keybindings is the 4th tab (index 3). Toggle Collapse is its first field.
	sv.Update(keyPress("l")) // to Team Defaults
	sv.Update(keyPress("l")) // to Hooks
	sv.Update(keyPress("l")) // to Keybindings
	if f := sv.selectedField(); f == nil || f.label != "Toggle Collapse" {
		t.Fatalf("expected field 'Toggle Collapse', got %v", f)
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

	// Hooks tab (index 2), HooksDir is field index 1.
	sv.Update(keyPress("l")) // Team Defaults
	sv.Update(keyPress("l")) // Hooks
	sv.Update(keyPress("j")) // to Hooks Directory
	if f := sv.selectedField(); f == nil || f.label != "Hooks Directory" {
		t.Fatalf("expected field 'Hooks Directory', got %v", f)
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
	sv.Update(keyPress("s"))         // pending save

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

func TestSettingsView_SaveConfirmedWithEnter(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	sv.Update(keyType(tea.KeyEnter)) // toggle theme → dirty
	sv.Update(keyPress("s"))         // pending save

	// Confirm with enter (source also accepts "y")
	cmd, consumed := sv.Update(keyType(tea.KeyEnter))
	if !consumed {
		t.Error("expected consumed=true")
	}
	if sv.Active {
		t.Error("expected Active=false after enter-confirm")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(SettingsSaveRequestMsg); !ok {
		t.Fatalf("expected SettingsSaveRequestMsg, got %T", msg)
	}
}

func TestSettings_HideWhatsNewToggle(t *testing.T) {
	sv := NewSettingsView()
	cfg := testConfig()
	cfg.HideWhatsNew = false
	sv.Open(cfg)

	// General tab, Hide What's New is field index 6.
	for i := 0; i < 6; i++ {
		sv.Update(keyPress("j"))
	}

	f := sv.selectedField()
	if f == nil || f.label != "Hide What's New" {
		label := ""
		if f != nil {
			label = f.label
		}
		t.Fatalf("expected 'Hide What's New' field, got %q", label)
	}

	// Toggle on.
	sv.Update(keyType(tea.KeyEnter))
	if !sv.GetConfig().HideWhatsNew {
		t.Error("expected HideWhatsNew=true after toggle")
	}

	// Toggle off.
	sv.Update(keyType(tea.KeyEnter))
	if sv.GetConfig().HideWhatsNew {
		t.Error("expected HideWhatsNew=false after second toggle")
	}
}

func TestSettingsView_EditEscCancels(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	// Navigate to PreviewRefreshMs (General tab, index 3)
	sv.Update(keyPress("j"))
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

// ─── Tabbed-settings (issue #76) ─────────────────────────────────────────────

func TestSettingsView_BuildTabs_HasFourCategories(t *testing.T) {
	tabs := buildSettingTabs()
	want := []string{"General", "Team Defaults", "Hooks", "Keybindings"}
	if len(tabs) != len(want) {
		t.Fatalf("expected %d tabs, got %d", len(want), len(tabs))
	}
	for i, title := range want {
		if tabs[i].title != title {
			t.Errorf("tab %d: expected title %q, got %q", i, title, tabs[i].title)
		}
		if len(tabs[i].fields) == 0 {
			t.Errorf("tab %d (%s): expected non-empty fields", i, title)
		}
	}
}

func TestSettingsView_Open_InitializesPerTabState(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	if got := len(sv.tabCursors); got != 4 {
		t.Errorf("expected 4 tabCursors, got %d", got)
	}
	if got := len(sv.tabScrollOffsets); got != 4 {
		t.Errorf("expected 4 tabScrollOffsets, got %d", got)
	}
	if sv.activeTab != 0 {
		t.Errorf("expected activeTab=0, got %d", sv.activeTab)
	}
	for i, c := range sv.tabCursors {
		if c != 0 {
			t.Errorf("tabCursors[%d] = %d, want 0", i, c)
		}
	}
}

func TestSettingsView_SwitchTab_Right_ClampsAtLast(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	sv.Update(keyPress("l"))
	if sv.activeTab != 1 {
		t.Fatalf("after l: activeTab=%d, want 1", sv.activeTab)
	}
	// Press right enough times to overshoot; should clamp at last tab (3).
	for i := 0; i < 10; i++ {
		sv.Update(keyType(tea.KeyRight))
	}
	if sv.activeTab != 3 {
		t.Errorf("clamp last: activeTab=%d, want 3", sv.activeTab)
	}
}

func TestSettingsView_SwitchTab_Left_ClampsAtZero(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	sv.Update(keyType(tea.KeyLeft))
	if sv.activeTab != 0 {
		t.Errorf("clamp zero: activeTab=%d, want 0", sv.activeTab)
	}
	sv.Update(keyPress("h"))
	if sv.activeTab != 0 {
		t.Errorf("clamp zero (h): activeTab=%d, want 0", sv.activeTab)
	}
}

func TestSettingsView_SwitchTab_HLMatchesArrows(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	sv.Update(keyPress("l"))
	sv.Update(keyPress("l"))
	if sv.activeTab != 2 {
		t.Errorf("l l: activeTab=%d, want 2", sv.activeTab)
	}
	sv.Update(keyPress("h"))
	if sv.activeTab != 1 {
		t.Errorf("h: activeTab=%d, want 1", sv.activeTab)
	}
}

func TestSettingsView_SwitchTab_PreservesPerTabCursor(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	// Move cursor on tab 0 (General) to index 2.
	sv.Update(keyPress("j"))
	sv.Update(keyPress("j"))
	if sv.cursor() != 2 {
		t.Fatalf("tab 0 cursor=%d, want 2", sv.cursor())
	}

	// Switch to tab 1 (Team Defaults); cursor should start at 0.
	sv.Update(keyPress("l"))
	if sv.cursor() != 0 {
		t.Fatalf("tab 1 initial cursor=%d, want 0", sv.cursor())
	}
	sv.Update(keyPress("j"))
	if sv.cursor() != 1 {
		t.Fatalf("tab 1 cursor=%d, want 1", sv.cursor())
	}

	// Back to tab 0; cursor should still be 2.
	sv.Update(keyPress("h"))
	if sv.activeTab != 0 {
		t.Fatalf("expected activeTab=0, got %d", sv.activeTab)
	}
	if sv.cursor() != 2 {
		t.Errorf("tab 0 cursor after return=%d, want 2", sv.cursor())
	}

	// Forward to tab 1 again; cursor should still be 1.
	sv.Update(keyPress("l"))
	if sv.cursor() != 1 {
		t.Errorf("tab 1 cursor after return=%d, want 1", sv.cursor())
	}
}

func TestSettingsView_SwitchTab_BlockedWhileEditing(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	// Navigate to PreviewRefreshMs (General tab, index 3) and start editing.
	sv.Update(keyPress("j"))
	sv.Update(keyPress("j"))
	sv.Update(keyPress("j"))
	sv.Update(keyType(tea.KeyEnter))
	if !sv.editing {
		t.Fatal("precondition: expected editing=true")
	}

	startTab := sv.activeTab
	// "l" should route to the edit input, not switch tabs.
	sv.Update(keyPress("l"))
	if sv.activeTab != startTab {
		t.Errorf("activeTab changed while editing: got %d, want %d", sv.activeTab, startTab)
	}
	if !sv.editing {
		t.Error("expected still editing")
	}
}

func TestSettingsView_SwitchTab_BlockedWhilePendingSave(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	sv.Update(keyType(tea.KeyEnter)) // toggle theme → dirty
	sv.Update(keyPress("s"))         // pendingSave
	if !sv.pendingSave {
		t.Fatal("precondition: pendingSave=true")
	}

	startTab := sv.activeTab
	// "l" isn't y/enter — it cancels pending save but must NOT switch tabs.
	sv.Update(keyPress("l"))
	if sv.activeTab != startTab {
		t.Errorf("activeTab changed while pendingSave: got %d, want %d", sv.activeTab, startTab)
	}
}

func TestSettingsView_RenderTabStrip_FitsNarrowWidth(t *testing.T) {
	sv := NewSettingsView()
	sv.Open(testConfig())

	for _, width := range []int{120, 80, 60, 40, 20, 10} {
		top, mid, base := sv.renderTabStrip(width)
		if got := ansi.StringWidth(top); got > width {
			t.Errorf("width=%d: top row width %d exceeds limit\n%q", width, got, top)
		}
		if got := ansi.StringWidth(mid); got > width {
			t.Errorf("width=%d: labels row width %d exceeds limit\n%q", width, got, mid)
		}
		if got := ansi.StringWidth(base); got > width {
			t.Errorf("width=%d: baseline row width %d exceeds limit\n%q", width, got, base)
		}
	}
}

func TestSettingsView_View_ShowsActiveTabContent(t *testing.T) {
	sv := NewSettingsView()
	sv.Width = 80
	sv.Height = 24
	sv.Open(testConfig())

	// Default: General tab.
	v := sv.View()
	if !contains(v, "Theme") {
		t.Errorf("expected General tab content (Theme) in view")
	}

	// Switch to Hooks.
	sv.Update(keyPress("l")) // Team Defaults
	sv.Update(keyPress("l")) // Hooks
	v = sv.View()
	if !contains(v, "Hooks Enabled") {
		t.Errorf("expected Hooks tab content (Hooks Enabled) in view")
	}
	if contains(v, "Preview Refresh") {
		t.Errorf("did not expect General-only field (Preview Refresh) on Hooks tab")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
