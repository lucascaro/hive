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

// GetPaneTitles returns pane titles and bell flags for all windows in a tmux
// session.  It executes a single `list-windows` call and returns:
//   - titles: "session:windowIndex" → pane title
//   - bells:  "session:windowIndex" → true when a bell has fired in that window
func GetPaneTitles(tmuxSession string) (map[string]string, map[string]bool, error) {
	out, err := Exec("list-windows", "-t", tmuxSession, "-F", "#{window_index}\t#{pane_title}\t#{window_bell_flag}")
	if err != nil {
		return nil, nil, err
	}
	titles := make(map[string]string)
	bells := make(map[string]bool)
	for _, line := range splitLines(out) {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		target := fmt.Sprintf("%s:%s", tmuxSession, parts[0])
		titles[target] = parts[1]
		if len(parts) == 3 && parts[2] == "1" {
			bells[target] = true
		}
	}
	return titles, bells, nil
}

// ClearBellFlags resets the bell flag for the given targets by selecting each
// window. In tmux, selecting a window clears its alert flags (including bell).
// Since hive never attaches a client to the shared session, this has no visible
// side effects.
func ClearBellFlags(targets []string) {
	if len(targets) == 0 {
		return
	}
	// Batch all select-window commands into a single tmux exec using \;.
	args := make([]string, 0, len(targets)*4)
	for i, target := range targets {
		if i > 0 {
			args = append(args, ";")
		}
		args = append(args, "select-window", "-t", target)
	}
	_ = ExecSilent(args...)
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
