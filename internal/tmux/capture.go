package tmux

import (
	"fmt"
	"strconv"
	"time"
)

// CapturePane returns the visible content of a tmux pane.
// target is "session:window". lines specifies how many lines to capture (0 = visible only).
// The -e flag preserves ANSI color codes; -J joins wrapped lines.
func CapturePane(target string, lines int) (string, error) {
	args := []string{"capture-pane", "-p", "-e", "-J", "-t", target}
	if lines > 0 {
		args = append(args, "-S", fmt.Sprintf("-%d", lines))
	}
	return Exec(args...)
}

// CapturePaneRaw returns pane content without stripping escape sequences
// (used by the title watcher to detect OSC sequences).
func CapturePaneRaw(target string, lines int) (string, error) {
	args := []string{"capture-pane", "-p", "-J", "-t", target}
	if lines > 0 {
		args = append(args, "-S", fmt.Sprintf("-%d", lines))
	}
	return Exec(args...)
}

// GetCurrentCommand returns the name of the foreground process running in the pane.
func GetCurrentCommand(target string) (string, error) {
	return Exec("display-message", "-p", "-t", target, "#{pane_current_command}")
}

// GetPaneActivity returns the time of the last output received by a pane.
// tmux's #{pane_activity} is a Unix timestamp (seconds) that is always tracked
// regardless of monitor-activity settings.
func GetPaneActivity(target string) (time.Time, error) {
	out, err := Exec("display-message", "-p", "-t", target, "#{pane_activity}")
	if err != nil {
		return time.Time{}, err
	}
	secs, err := strconv.ParseInt(out, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse pane_activity %q: %w", out, err)
	}
	return time.Unix(secs, 0), nil
}
