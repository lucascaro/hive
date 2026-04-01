package tmux

import "fmt"

const shortIDLen = 8

// SessionName returns the tmux session name for a project.
func SessionName(projectID string) string {
	short := projectID
	if len(short) > shortIDLen {
		short = short[:shortIDLen]
	}
	return fmt.Sprintf("hive-%s", short)
}

// WindowName returns the tmux window name for a session.
func WindowName(sessionID string) string {
	if len(sessionID) > shortIDLen {
		return sessionID[:shortIDLen]
	}
	return sessionID
}

// Target returns the tmux target string "session:window".
func Target(tmuxSession string, windowIdx int) string {
	return fmt.Sprintf("%s:%d", tmuxSession, windowIdx)
}
