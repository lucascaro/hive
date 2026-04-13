package tmux

import (
	"fmt"
	"strings"
)

// IsAlternateScreen reports whether the given pane is currently in alternate
// screen mode (i.e. running a TUI application like Claude Code that switches
// to the alternate screen buffer via \e[?1049h).
func IsAlternateScreen(target string) bool {
	out, err := Exec("display-message", "-p", "-t", target, "#{alternate_on}")
	return err == nil && strings.TrimSpace(out) == "1"
}

// CapturePane returns the visible content of a tmux pane.
// target is "session:window". lines specifies how many lines to capture (0 = visible only).
// The -e flag preserves ANSI color codes; -J joins wrapped lines.
//
// When the pane is in alternate screen mode (e.g. running a TUI like Claude Code),
// the alternate screen is captured instead of the normal screen buffer. The normal
// screen buffer only holds content from before the TUI started (usually a shell
// prompt), so it would appear nearly empty in the preview even though the session
// has real content visible to the user.
func CapturePane(target string, lines int) (string, error) {
	if IsAlternateScreen(target) {
		// Capture the alternate screen — the current visible TUI state.
		// No -S flag: the alternate screen has no scrollback buffer; the entire
		// visible screen is captured as-is.
		return Exec("capture-pane", "-a", "-p", "-e", "-J", "-t", target)
	}
	// Normal screen: capture with scrollback for context.
	args := []string{"capture-pane", "-p", "-e", "-J", "-t", target}
	if lines > 0 {
		args = append(args, "-S", fmt.Sprintf("-%d", lines))
	}
	return Exec(args...)
}

// CapturePaneRaw returns pane content without stripping escape sequences
// (used by the title watcher to detect OSC sequences).
// Like CapturePane, it captures the alternate screen when the pane is in
// alternate screen mode so the title watcher sees the current output.
func CapturePaneRaw(target string, lines int) (string, error) {
	if IsAlternateScreen(target) {
		return Exec("capture-pane", "-a", "-p", "-J", "-t", target)
	}
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
	out, err := Exec("list-windows", "-t", tmuxSession, "-F",
		"#{window_index}"+paneSep+"#{pane_title}"+paneSep+"#{window_bell_flag}")
	if err != nil {
		return nil, nil, err
	}
	return parsePaneTitles(tmuxSession, out)
}

// paneSep is the delimiter used between fields in list-windows output.
// ASCII unit separator (0x1F) — pane titles can contain tabs, so \t is not safe.
const paneSep = "\x1f"

// parsePaneTitles parses the raw output of a list-windows call into title and
// bell maps keyed by "session:windowIndex".
func parsePaneTitles(tmuxSession, out string) (map[string]string, map[string]bool, error) {
	titles := make(map[string]string)
	bells := make(map[string]bool)
	for _, line := range splitLines(out) {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, paneSep, 3)
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

// splitLines splits s by newline, handling empty trailing lines.
func splitLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

