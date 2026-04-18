package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/escape"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

// PollingManager centralises all session polling behind a single generation
// counter. Views declare intent; the manager decides which tea.Cmds to fire.
//
// Replaces the previous architecture of 5 independent tea.Tick chains with
// separate generation counters and ~20 scattered reschedule call sites.
type PollingManager struct {
	// generation is incremented on every intent change. Stale messages whose
	// generation doesn't match are discarded without rescheduling, letting
	// old goroutines die off naturally.
	generation uint64

	// Config.
	previewRefreshMs int

	// Status detection state (moved from Model).
	contentSnapshots map[string]string
	stableCounts     map[string]int
	paneTitles       map[string]string
	detectionCtxs    map[string]escape.SessionDetectionCtx
}

// NewPollingManager creates a PollingManager with the given config interval.
func NewPollingManager(previewRefreshMs int) PollingManager {
	return PollingManager{
		previewRefreshMs: previewRefreshMs,
		contentSnapshots: make(map[string]string),
		stableCounts:     make(map[string]int),
		paneTitles:       make(map[string]string),
	}
}

// Generation returns the current generation counter.
func (pm PollingManager) Generation() uint64 {
	return pm.generation
}

// Invalidate bumps the generation counter, causing all in-flight stale
// tick messages to be discarded when they arrive.
func (pm *PollingManager) Invalidate() {
	pm.generation++
}

// IsStale returns true if the given generation doesn't match the current one.
func (pm PollingManager) IsStale(gen uint64) bool {
	return gen != pm.generation
}

// SchedulePreview returns a tea.Cmd for the sidebar preview poll using the
// current generation.
func (pm *PollingManager) SchedulePreview(sess *state.Session) tea.Cmd {
	if sess == nil || sess.TmuxSession == "" {
		return nil
	}
	interval := time.Duration(pm.previewRefreshMs) * time.Millisecond
	return components.PollPreview(sess.ID, sess.TmuxSession, sess.TmuxWindow, interval, pm.generation)
}

// ScheduleGridPoll returns a tea.Cmd for the grid background poll using the
// current generation. In input mode, the focused session is excluded and the
// interval is doubled.
func (pm *PollingManager) ScheduleGridPoll(sessions []*state.Session, inputMode bool, focusedID string) tea.Cmd {
	if len(sessions) == 0 {
		return nil
	}
	interval := time.Duration(pm.previewRefreshMs) * time.Millisecond
	partial := false
	if inputMode {
		slow := time.Duration(inputModeBackgroundMs) * time.Millisecond
		if slow > interval {
			interval = slow
		}
		if focusedID != "" {
			filtered := make([]*state.Session, 0, len(sessions))
			for _, s := range sessions {
				if s.ID != focusedID {
					filtered = append(filtered, s)
				}
			}
			sessions = filtered
			partial = true
		}
		if len(sessions) == 0 {
			return nil
		}
	}
	return components.PollGridPreviews(sessions, interval, pm.generation, partial)
}

// ScheduleFocusedPoll returns a tea.Cmd for the fast 50ms input-mode poll.
func (pm *PollingManager) ScheduleFocusedPoll(sess *state.Session) tea.Cmd {
	if sess == nil || sess.TmuxSession == "" {
		return nil
	}
	interval := time.Duration(inputModeFocusedMs) * time.Millisecond
	if cfg := time.Duration(pm.previewRefreshMs) * time.Millisecond; cfg < interval {
		interval = cfg
	}
	return components.PollFocusedGridPreview(sess, interval)
}

// ScheduleStatuses returns a tea.Cmd for the status detection poll.
func (pm *PollingManager) ScheduleStatuses(allSessions []*state.Session) tea.Cmd {
	targets := make(map[string]string)
	detection := make(map[string]escape.SessionDetectionCtx)
	for _, sess := range allSessions {
		if sess.Status != state.StatusDead {
			targets[sess.ID] = mux.Target(sess.TmuxSession, sess.TmuxWindow)
			if ctx, ok := pm.detectionCtxs[string(sess.AgentType)]; ok {
				detection[sess.ID] = ctx
			}
		}
	}
	if len(targets) == 0 {
		return nil
	}
	titles, bells, err := mux.GetPaneTitles(mux.HiveSession)
	if err != nil {
		titles = make(map[string]string)
		bells = make(map[string]bool)
	}
	if titles == nil {
		titles = make(map[string]string)
	}
	if bells == nil {
		bells = make(map[string]bool)
	}
	// Snapshot maps to avoid races.
	prevContents := make(map[string]string, len(pm.contentSnapshots))
	for k, v := range pm.contentSnapshots {
		prevContents[k] = v
	}
	stableCounts := make(map[string]int, len(pm.stableCounts))
	for k, v := range pm.stableCounts {
		stableCounts[k] = v
	}
	interval := time.Duration(pm.previewRefreshMs*2) * time.Millisecond
	return escape.WatchStatuses(targets, prevContents, stableCounts, detection, titles, bells, interval)
}

// ScheduleTitles returns a tea.Cmd for the title detection poll.
func (pm *PollingManager) ScheduleTitles(allSessions []*state.Session) tea.Cmd {
	targets := make(map[string]string)
	for _, sess := range allSessions {
		if sess.Status != state.StatusDead {
			targets[sess.ID] = mux.Target(sess.TmuxSession, sess.TmuxWindow)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	interval := time.Duration(pm.previewRefreshMs*2) * time.Millisecond
	return escape.WatchTitles(targets, interval)
}

// ContentSnapshot returns the last captured content for the given session.
func (pm *PollingManager) ContentSnapshot(sessionID string) string {
	return pm.contentSnapshots[sessionID]
}

// SetContentSnapshot stores a content snapshot for the given session.
func (pm *PollingManager) SetContentSnapshot(sessionID, content string) {
	pm.contentSnapshots[sessionID] = content
}

// StableCount returns the debounce counter for the given session.
func (pm *PollingManager) StableCount(sessionID string) int {
	return pm.stableCounts[sessionID]
}

// SetStableCount stores the debounce counter for the given session.
func (pm *PollingManager) SetStableCount(sessionID string, count int) {
	pm.stableCounts[sessionID] = count
}

// SetPaneTitles replaces the pane title map wholesale.
func (pm *PollingManager) SetPaneTitles(titles map[string]string) {
	pm.paneTitles = titles
}

// PaneTitle returns the pane title map for grid rendering.
func (pm *PollingManager) PaneTitle() map[string]string {
	return pm.paneTitles
}

// SetDetectionCtxs sets the compiled status detection regexes.
func (pm *PollingManager) SetDetectionCtxs(ctxs map[string]escape.SessionDetectionCtx) {
	pm.detectionCtxs = ctxs
}

// CleanupSession removes stale detection state for a killed session.
func (pm *PollingManager) CleanupSession(sessionID string) {
	delete(pm.stableCounts, sessionID)
	delete(pm.contentSnapshots, sessionID)
}
