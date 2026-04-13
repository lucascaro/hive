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
	restore := audio.SetTestHooks(nil, func(string, int) error { playCalls.Add(1); return nil })
	t.Cleanup(func() { audio.SyncForTest = false; restore() })

	w := newAttachBellWatcher()
	fn, _ := tickingBellFn(2, "hive-sessions:0") // bell fires on 2nd poll
	w.getPaneTitlesFn = fn
	w.start(audio.BellChime, 100, makeTargetMap())

	// Give the goroutine time to poll twice (500ms each) with generous margin.
	time.Sleep(1600 * time.Millisecond)
	w.stop()

	if got := playCalls.Load(); got != 1 {
		t.Errorf("playWAV calls = %d, want 1", got)
	}
}

func TestAttachBellWatcher_NoPlayOnAlreadyPending(t *testing.T) {
	audio.SyncForTest = true
	var playCalls atomic.Int32
	restore := audio.SetTestHooks(nil, func(string, int) error { playCalls.Add(1); return nil })
	t.Cleanup(func() { audio.SyncForTest = false; restore() })

	w := newAttachBellWatcher()
	target := "hive-sessions:0"
	// Bell present from the very first call (baseline) — should not trigger.
	w.getPaneTitlesFn = func(_ string) (map[string]string, map[string]bool, error) {
		return nil, map[string]bool{target: true}, nil
	}
	w.start(audio.BellChime, 100, makeTargetMap())

	time.Sleep(600 * time.Millisecond)
	w.stop()

	if got := playCalls.Load(); got != 0 {
		t.Errorf("playWAV calls = %d, want 0 (bell was already pending at attach time)", got)
	}
}

func TestAttachBellWatcher_AccumulatesNewBells(t *testing.T) {
	audio.SyncForTest = true
	restore := audio.SetTestHooks(nil, func(string, int) error { return nil })
	t.Cleanup(func() { audio.SyncForTest = false; restore() })

	w := newAttachBellWatcher()
	target := "hive-sessions:0"
	fn, _ := tickingBellFn(2, target)
	w.getPaneTitlesFn = fn
	w.start(audio.BellChime, 100, makeTargetMap())

	time.Sleep(1600 * time.Millisecond)
	newBells := w.stop()

	if !newBells["sess-1"] {
		t.Errorf("newBells[sess-1] = false, want true")
	}
}

func TestAttachBellWatcher_Debounce(t *testing.T) {
	audio.SyncForTest = true
	var playCalls atomic.Int32
	restore := audio.SetTestHooks(nil, func(string, int) error { playCalls.Add(1); return nil })
	t.Cleanup(func() { audio.SyncForTest = false; restore() })

	target := "hive-sessions:0"
	// Polls 2 and 4 fire a bell; polls 1, 3, 5 are silent.
	// Silent polls clear alreadyRinging so the next bell is a fresh edge.
	// Poll 2 (≈500ms) triggers play. Poll 4 (≈1500ms) is >500ms later so
	// debounce allows a second play. Total expected: 2.
	var callCount atomic.Int32
	w := newAttachBellWatcher()
	w.getPaneTitlesFn = func(_ string) (map[string]string, map[string]bool, error) {
		n := int(callCount.Add(1))
		switch n {
		case 2, 4: // bell present
			return nil, map[string]bool{target: true}, nil
		default: // bell absent — clears alreadyRinging so next presence is a new edge
			return nil, nil, nil
		}
	}
	w.start(audio.BellChime, 100, makeTargetMap())

	// 4 polls at 500ms each = ~2000ms; add 800ms margin for CI jitter.
	time.Sleep(2800 * time.Millisecond)
	w.stop()

	if got := playCalls.Load(); got != 2 {
		t.Errorf("playWAV calls = %d, want 2 (two edges separated by >500ms)", got)
	}
}

func TestAttachBellWatcher_StopIsClean(t *testing.T) {
	w := newAttachBellWatcher()
	w.getPaneTitlesFn = func(_ string) (map[string]string, map[string]bool, error) {
		return nil, nil, nil
	}
	w.start(audio.BellSilent, 100, map[string]string{})

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
