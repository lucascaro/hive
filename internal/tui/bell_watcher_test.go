package tui

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/audio"
	"github.com/lucascaro/hive/internal/mux"
)

// makeTargetMap returns a simple single-session target map for tests.
func makeTargetMap() map[string]string {
	return map[string]string{"sess-1": "hive-sessions:0"}
}

// tickingBellFn returns a getPaneTitlesFn that fires a bell on the Nth call.
// The returned function is safe to call from the watcher goroutine.
func tickingBellFn(bellOnCall int, target string) (fn func(string) (map[string]string, map[string]bool, error), calls *atomic.Int32) {
	var c atomic.Int32
	fn = func(_ string) (map[string]string, map[string]bool, error) {
		n := int(c.Add(1))
		if n == bellOnCall {
			return nil, map[string]bool{target: true}, nil
		}
		return nil, nil, nil
	}
	return fn, &c
}

func TestAttachBellWatcher_PlaysOnNewBell(t *testing.T) {
	audio.SyncForTest = true
	var playCalls atomic.Int32
	restore := audio.SetTestHooks(nil, func(string) error { playCalls.Add(1); return nil })
	t.Cleanup(func() { restore(); audio.SyncForTest = false })

	w := newAttachBellWatcher()
	fn, _ := tickingBellFn(2, "hive-sessions:0") // bell fires on 2nd poll
	w.getPaneTitlesFn = fn
	w.start(audio.BellChime, makeTargetMap())

	// Give the goroutine time to poll twice (500ms each).
	time.Sleep(1200 * time.Millisecond)
	w.stop()

	if got := playCalls.Load(); got != 1 {
		t.Errorf("playWAV calls = %d, want 1", got)
	}
}

func TestAttachBellWatcher_NoPlayOnAlreadyPending(t *testing.T) {
	audio.SyncForTest = true
	var playCalls atomic.Int32
	restore := audio.SetTestHooks(nil, func(string) error { playCalls.Add(1); return nil })
	t.Cleanup(func() { restore(); audio.SyncForTest = false })

	w := newAttachBellWatcher()
	target := "hive-sessions:0"
	// Bell present from the very first call (baseline) — should not trigger.
	w.getPaneTitlesFn = func(_ string) (map[string]string, map[string]bool, error) {
		return nil, map[string]bool{target: true}, nil
	}
	w.start(audio.BellChime, makeTargetMap())

	time.Sleep(600 * time.Millisecond)
	w.stop()

	if got := playCalls.Load(); got != 0 {
		t.Errorf("playWAV calls = %d, want 0 (bell was already pending at attach time)", got)
	}
}

func TestAttachBellWatcher_AccumulatesNewBells(t *testing.T) {
	audio.SyncForTest = true
	restore := audio.SetTestHooks(nil, func(string) error { return nil })
	t.Cleanup(func() { restore(); audio.SyncForTest = false })

	w := newAttachBellWatcher()
	target := "hive-sessions:0"
	fn, _ := tickingBellFn(2, target)
	w.getPaneTitlesFn = fn
	w.start(audio.BellChime, makeTargetMap())

	time.Sleep(1200 * time.Millisecond)
	newBells := w.stop()

	if !newBells["sess-1"] {
		t.Errorf("newBells[sess-1] = false, want true")
	}
}

func TestAttachBellWatcher_Debounce(t *testing.T) {
	audio.SyncForTest = true
	var playCalls atomic.Int32
	restore := audio.SetTestHooks(nil, func(string) error { playCalls.Add(1); return nil })
	t.Cleanup(func() { restore(); audio.SyncForTest = false })

	target := "hive-sessions:0"
	// Both poll 2 and 3 fire a new bell (toggling via absent on odd calls so
	// alreadyRinging is cleared between polls 2→3).
	var callCount atomic.Int32
	w := newAttachBellWatcher()
	w.getPaneTitlesFn = func(_ string) (map[string]string, map[string]bool, error) {
		n := int(callCount.Add(1))
		switch n {
		case 2, 4: // bell present
			return nil, map[string]bool{target: true}, nil
		default: // bell absent (clears alreadyRinging so next presence is a new edge)
			return nil, nil, nil
		}
	}
	w.start(audio.BellChime, makeTargetMap())

	// 4 polls at 500ms each = ~2000ms; both edges would fire but debounce
	// prevents the second within the 500ms window.
	time.Sleep(2200 * time.Millisecond)
	w.stop()

	// Poll 2 triggers play. Poll 4 is at ~1500ms, which is >500ms after poll 2
	// (~500ms), so it fires too. We want exactly 2 plays total.
	if got := playCalls.Load(); got != 2 {
		t.Errorf("playWAV calls = %d, want 2 (two edges separated by >500ms)", got)
	}
}

func TestAttachBellWatcher_StopIsClean(t *testing.T) {
	w := newAttachBellWatcher()
	w.getPaneTitlesFn = func(_ string) (map[string]string, map[string]bool, error) {
		return nil, nil, nil
	}
	w.start(audio.BellSilent, map[string]string{})

	done := make(chan struct{})
	go func() {
		w.stop()
		close(done)
	}()
	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("stop() did not return within 2s")
	}
}

func TestBuildSessionTargets_ExcludesDead(t *testing.T) {
	appState := testAppStateWithTwoProjects()
	targets := buildSessionTargets(&appState)
	// testAppStateWithTwoProjects gives sess-1 and sess-2 (both non-dead).
	for sid, target := range targets {
		if target == "" {
			t.Errorf("buildSessionTargets: session %s has empty target", sid)
		}
		if target == (mux.Target("", 0)) {
			t.Errorf("buildSessionTargets: session %s has zero-value target", sid)
		}
	}
	if len(targets) == 0 {
		t.Error("expected at least one target, got none")
	}
}
