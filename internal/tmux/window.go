package tmux

import (
	"fmt"
	"strconv"
	"strings"
)

// WindowInfo holds parsed information about a tmux window.
type WindowInfo struct {
	Index  int
	Name   string
	Active bool
}

// CreateWindow adds a new window to an existing tmux session.
// If startCmd is non-empty the command is launched directly as the window
// process (no shell wrapper); when the process exits the window closes.
// Returns the new window's index.
func CreateWindow(tmuxSession, windowName, workDir string, startCmd []string) (int, error) {
	args := []string{
		"new-window",
		"-t", tmuxSession,
		"-n", windowName,
		"-c", workDir,
		"-P", "-F", "#{window_index}",
	}
	args = append(args, startCmd...)
	out, err := Exec(args...)
	if err != nil {
		return 0, err
	}
	idx, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parse window index %q: %w", out, err)
	}
	return idx, nil
}

// WindowExists reports whether a tmux window target (session:index) exists.
func WindowExists(target string) bool {
	_, err := Exec("display-message", "-t", target, "-p", "")
	return err == nil
}

func KillWindow(target string) error {
	return ExecSilent("kill-window", "-t", target)
}

func RenameWindow(target, newName string) error {
	return ExecSilent("rename-window", "-t", target, newName)
}

// SendKeys sends a literal string of bytes to the pane at target.
// The -l flag bypasses tmux key-name interpretation so raw bytes (including
// ANSI escape sequences) are written directly to the pane's stdin.
func SendKeys(target, keys string) error {
	return ExecSilent("send-keys", "-t", target, "-l", "--", keys)
}

func ListWindows(tmuxSession string) ([]WindowInfo, error) {
	out, err := Exec(
		"list-windows",
		"-t", tmuxSession,
		"-F", "#{window_index}:#{window_name}:#{window_active}",
	)
	if err != nil {
		return nil, err
	}
	var windows []WindowInfo
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		idx, _ := strconv.Atoi(parts[0])
		windows = append(windows, WindowInfo{
			Index:  idx,
			Name:   parts[1],
			Active: parts[2] == "1",
		})
	}
	return windows, nil
}
