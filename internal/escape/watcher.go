package escape

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/state"
)

// TitleDetectedMsg is sent when an agent sets a session title via escape sequence.
type TitleDetectedMsg struct {
	SessionID string
	Title     string
}

// WatchTitles returns a tea.Cmd that polls all active sessions for title escape sequences.
// sessionTargets maps sessionID → "tmuxSession:windowIdx".
func WatchTitles(sessionTargets map[string]string, interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(_ time.Time) tea.Msg {
		for sessionID, target := range sessionTargets {
			raw, err := mux.CapturePaneRaw(target, 200)
			if err != nil {
				continue
			}
			if title := ExtractTitle(raw); title != "" {
				return TitleDetectedMsg{SessionID: sessionID, Title: title}
			}
		}
		return nil
	})
}

// StatusesDetectedMsg carries fresh status readings and updated content snapshots
// for all polled sessions.
type StatusesDetectedMsg struct {
	Statuses    map[string]state.SessionStatus // sessionID → detected status
	Contents    map[string]string              // sessionID → captured pane content (for next diff)
	RawContents map[string]string              // sessionID → raw pane output (for bell detection bookkeeping)
	Bells       map[string]bool               // sessionID → true if a new bell was detected
}

// WatchStatuses returns a tea.Cmd that captures pane content for all active sessions
// and derives running/idle status by comparing against prevContents.
// A session whose content changed since prevContents is StatusRunning; one whose
// content is unchanged is StatusIdle. prevContents maps sessionID → last content.
// prevRawContents maps sessionID → last raw output (used for bell detection).
// sessionTargets maps sessionID → "tmuxSession:windowIdx".
func WatchStatuses(sessionTargets map[string]string, prevContents map[string]string, prevRawContents map[string]string, interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(_ time.Time) tea.Msg {
		statuses := make(map[string]state.SessionStatus, len(sessionTargets))
		contents := make(map[string]string, len(sessionTargets))
		rawContents := make(map[string]string, len(sessionTargets))
		bells := make(map[string]bool)
		for sessionID, target := range sessionTargets {
			content, err := mux.CapturePane(target, 50)
			if err != nil {
				continue
			}
			contents[sessionID] = content
			if content != prevContents[sessionID] {
				statuses[sessionID] = state.StatusRunning
			} else {
				statuses[sessionID] = state.StatusIdle
			}
			raw, err := mux.CapturePaneRaw(target, 50)
			if err == nil {
				rawContents[sessionID] = raw
				if raw != prevRawContents[sessionID] && DetectBell(raw) {
					bells[sessionID] = true
				}
			}
		}
		return StatusesDetectedMsg{Statuses: statuses, Contents: contents, RawContents: rawContents, Bells: bells}
	})
}
