package escape

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/tmux"
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
			raw, err := tmux.CapturePaneRaw(target, 200)
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
