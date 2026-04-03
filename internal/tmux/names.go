package tmux

import "fmt"

const (
	projMaxLen  = 8
	titleMaxLen = 12

	// HiveSession is the single shared tmux session used for all hive windows.
	HiveSession = "hive-sessions"
)

// SessionName returns the shared tmux session name. The projectID argument is
// ignored; all hive windows live in one session for easy identification and recovery.
func SessionName(_ string) string { return HiveSession }

// WindowName returns a descriptive window name encoding the project, agent
// type, and session title. Format: {project[:8]}-{agentType}-{title[:12]}
func WindowName(projectName, agentType, sessionTitle string) string {
	proj := projectName
	if len(proj) > projMaxLen {
		proj = proj[:projMaxLen]
	}
	title := sessionTitle
	if len(title) > titleMaxLen {
		title = title[:titleMaxLen]
	}
	return fmt.Sprintf("%s-%s-%s", proj, agentType, title)
}

// Target returns the tmux target string "session:window".
func Target(tmuxSession string, windowIdx int) string {
	return fmt.Sprintf("%s:%d", tmuxSession, windowIdx)
}
