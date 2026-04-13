package tui

import (
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/audio"
	"github.com/lucascaro/hive/internal/escape"
	"github.com/lucascaro/hive/internal/tui/components"
)

// bellRecorder captures calls to audio hooks for assertions. Install with
// installBellRecorder; tests must not call audio hooks concurrently.
type bellRecorder struct {
	writeCalls atomic.Int32
	playCalls  atomic.Int32
}

func installBellRecorder(t *testing.T) *bellRecorder {
	t.Helper()
	r := &bellRecorder{}
	// Run Play synchronously so assertions don't race the goroutine.
	prev := audio.SyncForTest
	audio.SyncForTest = true
	restore := audio.SetTestHooks(
		func() { r.writeCalls.Add(1) },
		func(path string) error { r.playCalls.Add(1); return nil },
	)
	t.Cleanup(func() {
		restore()
		audio.SyncForTest = prev
	})
	return r
}

// TestFlow_BellSoundInSettings opens Settings → General, navigates to the
// "Bell Sound" field, cycles past "normal" to the next option via Enter,
// and asserts the confirmed save carries the new value.
func TestFlow_BellSoundInSettings(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	openSettings(t, f)

	// Navigate to Bell Sound by label so the test doesn't break if new
	// General-tab fields are inserted above it. Bounded by the field count
	// so a missing field fails loudly instead of looping forever.
	const wantLabel = "Bell Sound"
	for i := 0; i < 32; i++ {
		if f.model.settings.SelectedFieldLabel() == wantLabel {
			break
		}
		prev := f.model.settings.TabCursor(0)
		f.SendKey("j")
		if f.model.settings.TabCursor(0) == prev {
			t.Fatalf("reached end of General tab without finding %q field", wantLabel)
		}
	}
	if got := f.model.settings.SelectedFieldLabel(); got != wantLabel {
		t.Fatalf("selected field = %q, want %q", got, wantLabel)
	}

	// Enter cycles to the next option. Default is "normal" (index 0), so
	// one press should move to "bee" (Bells[1]).
	f.SendSpecialKey(tea.KeyEnter)
	if !f.model.settings.IsDirty() {
		t.Fatal("expected dirty=true after cycling Bell Sound")
	}
	if got := f.model.settings.GetConfig().BellSound; got != audio.BellBee {
		t.Errorf("BellSound after one Enter = %q, want %q", got, audio.BellBee)
	}

	// Save flow: 's' then 'y'.
	f.SendKey("s")
	cmd := f.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("save confirm should emit a SettingsSaveRequestMsg cmd")
	}
	msg := cmd()
	save, ok := msg.(components.SettingsSaveRequestMsg)
	if !ok {
		t.Fatalf("save cmd produced %T, want SettingsSaveRequestMsg", msg)
	}
	if save.Config.BellSound != audio.BellBee {
		t.Errorf("saved Config.BellSound = %q, want %q", save.Config.BellSound, audio.BellBee)
	}
}

// TestFlow_SilentBellDoesNotEmit verifies that with BellSound="silent",
// a bell event still marks the sidebar badge but produces no audio.
func TestFlow_SilentBellDoesNotEmit(t *testing.T) {
	m, mock := testFlowModel(t)
	m.cfg.BellSound = audio.BellSilent
	rec := installBellRecorder(t)
	f := newFlowRunner(t, m, mock)

	target := "hive-proj1234:0" // sess-1 from testAppStateWithTwoProjects
	f.Send(escape.StatusesDetectedMsg{
		Bells: map[string]bool{target: true},
	})

	if got := rec.playCalls.Load(); got != 0 {
		t.Errorf("playWAV called %d times for silent bell, want 0", got)
	}
	if got := rec.writeCalls.Load(); got != 0 {
		t.Errorf("writeBell called %d times for silent bell, want 0", got)
	}
	if !f.model.bellPending["sess-1"] {
		t.Error("silent mode must still set the sidebar bell indicator")
	}
}

// TestFlow_BellDebounceStillApplies verifies the 500ms debounce in
// handleStatusesDetected is preserved under the new audio.Play plumbing:
// two back-to-back bell events produce only one playback.
func TestFlow_BellDebounceStillApplies(t *testing.T) {
	m, mock := testFlowModel(t)
	m.cfg.BellSound = audio.BellPing
	rec := installBellRecorder(t)
	f := newFlowRunner(t, m, mock)

	target := "hive-proj1234:0"
	// First event: fires exactly once.
	f.Send(escape.StatusesDetectedMsg{Bells: map[string]bool{target: true}})
	if got := rec.playCalls.Load(); got != 1 {
		t.Fatalf("first bell: playWAV = %d, want 1", got)
	}

	// Clear bellPending so the same target can "re-ring" as a new edge.
	// Immediate second event: debounce keeps the audible count at 1.
	delete(f.model.bellPending, "sess-1")
	f.Send(escape.StatusesDetectedMsg{Bells: map[string]bool{target: true}})
	if got := rec.playCalls.Load(); got != 1 {
		t.Errorf("second bell within 500ms: playWAV = %d, want still 1 (debounced)", got)
	}
}

// TestFlow_BellDuringAttachRestoresBadge verifies that NewBells carried by
// AttachDoneMsg is merged into bellPending so the sidebar badge appears when
// the user returns from an attached session.
func TestFlow_BellDuringAttachRestoresBadge(t *testing.T) {
	m, mock := testFlowModel(t)
	rec := installBellRecorder(t)
	f := newFlowRunner(t, m, mock)

	// Simulate returning from an attached session where sess-2 rang.
	f.Send(AttachDoneMsg{
		NewBells: map[string]bool{"sess-2": true},
	})

	// The watcher already played the sound; handleAttachDone must NOT re-play.
	if got := rec.playCalls.Load(); got != 0 {
		t.Errorf("playWAV calls after AttachDoneMsg = %d, want 0 (watcher plays, not TUI resume)", got)
	}
	// The badge must appear for sess-2.
	if !f.model.bellPending["sess-2"] {
		t.Error("bellPending[sess-2] = false, want true after AttachDoneMsg.NewBells")
	}
	// The active session (sess-1) must be cleared.
	if f.model.bellPending["sess-1"] {
		t.Error("bellPending[sess-1] = true, want false (active session cleared on attach)")
	}
}

// TestFlow_BellDuringAttachUpdatesGridBadge verifies that the grid view's
// bell pending state is updated when the TUI resumes from an attached session.
func TestFlow_BellDuringAttachUpdatesGridBadge(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.Send(AttachDoneMsg{
		NewBells: map[string]bool{"sess-2": true},
	})

	if !f.model.gridView.BellPendingForTest("sess-2") {
		t.Error("gridView bell pending for sess-2 = false, want true after AttachDoneMsg")
	}
}

// TestFlow_CustomBellPlays sanity-checks the golden path: with a non-silent
// non-normal sound, a bell event produces exactly one playWAV call.
func TestFlow_CustomBellPlays(t *testing.T) {
	m, mock := testFlowModel(t)
	m.cfg.BellSound = audio.BellChime
	rec := installBellRecorder(t)
	f := newFlowRunner(t, m, mock)

	f.Send(escape.StatusesDetectedMsg{
		Bells: map[string]bool{"hive-proj1234:0": true},
	})

	if got := rec.playCalls.Load(); got != 1 {
		t.Errorf("playWAV = %d, want 1", got)
	}
	if got := rec.writeCalls.Load(); got != 0 {
		t.Errorf("writeBell = %d, want 0 (no fallback expected)", got)
	}
}
