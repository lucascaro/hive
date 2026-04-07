package tmux

import (
	"fmt"
	"strconv"
	"strings"
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

// IsPaneDead reports whether the pane's process has exited.
func IsPaneDead(target string) bool {
	out, err := Exec("display-message", "-p", "-t", target, "#{pane_dead}")
	return err == nil && out == "1"
}

// GetCurrentCommand returns the name of the foreground process running in the pane.
func GetCurrentCommand(target string) (string, error) {
	return Exec("display-message", "-p", "-t", target, "#{pane_current_command}")
}

// GetPaneTitles returns pane titles for all windows in a tmux session.
// It executes a single `list-windows` call and returns a map of
// "session:windowIndex" → pane title.
func GetPaneTitles(tmuxSession string) (map[string]string, error) {
	out, err := Exec("list-windows", "-t", tmuxSession, "-F", "#{window_index}\t#{pane_title}")
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, line := range splitLines(out) {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		target := fmt.Sprintf("%s:%s", tmuxSession, parts[0])
		result[target] = parts[1]
	}
	return result, nil
}

// splitLines splits s by newline, handling empty trailing lines.
func splitLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
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
