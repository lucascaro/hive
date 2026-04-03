package tui

import (
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lucascaro/hive/internal/config"
)

const stateWatchInterval = 500 * time.Millisecond

// stateWatchMsg is the internal tick returned by the background watcher
// goroutine.  mtime is the observed modification time of state.json; the
// Update handler compares it against Model.stateLastKnownMtime to decide
// whether an external instance wrote the file.
type stateWatchMsg struct {
	mtime time.Time
}

// scheduleWatchState returns a command that sleeps for stateWatchInterval
// then stats state.json and returns its current modification time.
func scheduleWatchState(lastMod time.Time) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(stateWatchInterval)
		info, err := os.Stat(config.StatePath())
		if err != nil {
			// File missing or unreadable — keep the last known mtime so the
			// next cycle has a stable baseline without triggering a reload.
			return stateWatchMsg{mtime: lastMod}
		}
		return stateWatchMsg{mtime: info.ModTime()}
	}
}
