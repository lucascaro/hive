package tui

import "testing"

func TestPollingManager_InvalidateBumpsGeneration(t *testing.T) {
	pm := NewPollingManager(500)
	gen0 := pm.Generation()

	pm.Invalidate()
	gen1 := pm.Generation()

	if gen1 <= gen0 {
		t.Errorf("Invalidate should bump generation: got %d, was %d", gen1, gen0)
	}
}

func TestPollingManager_IsStale(t *testing.T) {
	pm := NewPollingManager(500)
	gen := pm.Generation()

	if pm.IsStale(gen) {
		t.Error("current generation should not be stale")
	}

	pm.Invalidate()

	if !pm.IsStale(gen) {
		t.Error("previous generation should be stale after Invalidate")
	}
	if pm.IsStale(pm.Generation()) {
		t.Error("new generation should not be stale")
	}
}

func TestPollingManager_CleanupSession(t *testing.T) {
	pm := NewPollingManager(500)
	pm.SetContentSnapshot("sess-1", "content")
	pm.SetStableCount("sess-1", 3)

	pm.CleanupSession("sess-1")

	if pm.ContentSnapshot("sess-1") != "" {
		t.Error("CleanupSession should remove content snapshot")
	}
	if pm.StableCount("sess-1") != 0 {
		t.Error("CleanupSession should remove stable count")
	}
}

func TestPollingManager_ContentSnapshotAccessors(t *testing.T) {
	pm := NewPollingManager(500)

	if pm.ContentSnapshot("x") != "" {
		t.Error("missing key should return empty string")
	}

	pm.SetContentSnapshot("x", "hello")
	if pm.ContentSnapshot("x") != "hello" {
		t.Errorf("got %q, want %q", pm.ContentSnapshot("x"), "hello")
	}
}

func TestPollingManager_StableCountAccessors(t *testing.T) {
	pm := NewPollingManager(500)

	if pm.StableCount("x") != 0 {
		t.Error("missing key should return 0")
	}

	pm.SetStableCount("x", 5)
	if pm.StableCount("x") != 5 {
		t.Errorf("got %d, want 5", pm.StableCount("x"))
	}
}

func TestPollingManager_PaneTitles(t *testing.T) {
	pm := NewPollingManager(500)

	titles := map[string]string{"target:0": "My Title"}
	pm.SetPaneTitles(titles)

	got := pm.PaneTitle()
	if got["target:0"] != "My Title" {
		t.Errorf("PaneTitle()[target:0] = %q, want %q", got["target:0"], "My Title")
	}
}

func TestPollingManager_SchedulePreviewNilSession(t *testing.T) {
	pm := NewPollingManager(500)
	if cmd := pm.SchedulePreview(nil); cmd != nil {
		t.Error("SchedulePreview(nil) should return nil")
	}
}

func TestPollingManager_ScheduleGridPollEmpty(t *testing.T) {
	pm := NewPollingManager(500)
	if cmd := pm.ScheduleGridPoll(nil, false, ""); cmd != nil {
		t.Error("ScheduleGridPoll with empty sessions should return nil")
	}
}

func TestPollingManager_ScheduleFocusedPollNil(t *testing.T) {
	pm := NewPollingManager(500)
	if cmd := pm.ScheduleFocusedPoll(nil); cmd != nil {
		t.Error("ScheduleFocusedPoll(nil) should return nil")
	}
}
