package tmux

import (
	"strings"
)

// CreateSession creates a new detached tmux session with a first window.
// If startCmd is non-empty the command is launched directly as the window
// process (no shell wrapper); when the process exits the window closes.
func CreateSession(tmuxSession, firstWindowName, workDir string, startCmd []string) error {
	args := []string{
		"new-session", "-d",
		"-s", tmuxSession,
		"-n", firstWindowName,
		"-c", workDir,
	}
	args = append(args, startCmd...)
	return ExecSilent(args...)
}

func SessionExists(tmuxSession string) bool {
	_, err := Exec("has-session", "-t", tmuxSession)
	return err == nil
}

// KillSession removes an entire tmux session and all its windows.
func KillSession(tmuxSession string) error {
	return ExecSilent("kill-session", "-t", tmuxSession)
}

// SetOption sets a tmux session-level option (e.g. "mouse", "on").
func SetOption(tmuxSession, key, value string) error {
	return ExecSilent("set-option", "-t", tmuxSession, key, value)
}

func ListSessionNames() ([]string, error) {
	out, err := Exec("list-sessions", "-F", "#{session_name}")
	if err != nil {
		// No server running is not an error we propagate.
		if strings.Contains(err.Error(), "no server running") ||
			strings.Contains(err.Error(), "No such file") {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}
