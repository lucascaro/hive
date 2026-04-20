package tui

import (
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/mux"
)

const stateWatchInterval = 500 * time.Millisecond

// stateWatchMsg is the internal tick returned by the background watcher
// goroutine.  mtime is the observed modification time of state.json; the
// Update handler compares it against Model.stateLastKnownMtime to decide
// whether an external instance wrote the file. canonicalGone is true when
// the backend's canonical session has disappeared (tmux server restart or
// external kill); the TUI treats this as fatal and exits cleanly.
type stateWatchMsg struct {
	mtime         time.Time
	canonicalGone bool
}

// scheduleWatchState returns a command that sleeps for stateWatchInterval
// then stats state.json and returns its current modification time.
func scheduleWatchState(lastMod time.Time) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(stateWatchInterval)
		msg := stateWatchMsg{mtime: lastMod}
		if info, err := os.Stat(config.StatePath()); err == nil {
			msg.mtime = info.ModTime()
		}
		// Detect tmux-level teardown. Non-grouped backends always report
		// canonicalExists=true (see mux.CanonicalExists), so this is a
		// no-op on the native backend.
		if !mux.CanonicalExists() {
			msg.canonicalGone = true
		}
		return msg
	}
}
