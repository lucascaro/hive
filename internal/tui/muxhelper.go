package tui

import "github.com/lucascaro/hive/internal/mux"

// ensureMuxWindow creates a tmux window in the given session, creating the
// session first if it doesn't exist. Returns the window index.
func ensureMuxWindow(muxSess, windowName, workDir string, cmd []string) (int, error) {
	if !mux.SessionExists(muxSess) {
		if err := mux.CreateSession(muxSess, windowName, workDir, cmd); err != nil {
			return 0, err
		}
		return 0, nil
	}
	return mux.CreateWindow(muxSess, windowName, workDir, cmd)
}
