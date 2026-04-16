package tmux

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var altScreenCache struct {
	mu      sync.Mutex
	entries map[string]altCacheEntry
}

type altCacheEntry struct {
	isAlt     bool
	checkedAt time.Time
}

const altScreenTTL = 500 * time.Millisecond

func isAlternateScreenCached(target string) bool {
	now := time.Now()
	altScreenCache.mu.Lock()
	if altScreenCache.entries == nil {
		altScreenCache.entries = make(map[string]altCacheEntry)
	}
	if e, ok := altScreenCache.entries[target]; ok && now.Sub(e.checkedAt) < altScreenTTL {
		altScreenCache.mu.Unlock()
		return e.isAlt
	}
	altScreenCache.mu.Unlock()

	isAlt := IsAlternateScreen(target)

	altScreenCache.mu.Lock()
	altScreenCache.entries[target] = altCacheEntry{isAlt: isAlt, checkedAt: now}
	altScreenCache.mu.Unlock()
	return isAlt
}

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
	if isAlternateScreenCached(target) {
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
	if isAlternateScreenCached(target) {
		return Exec("capture-pane", "-a", "-p", "-J", "-t", target)
	}
	args := []string{"capture-pane", "-p", "-J", "-t", target}
	if lines > 0 {
		args = append(args, "-S", fmt.Sprintf("-%d", lines))
	}
	return Exec(args...)
}

// batchSeparator is used to delimit output from multiple capture-pane calls
// in a single shell invocation. ASCII Record Separator (0x1E) is not emitted
// by terminal applications.
const batchSeparator = "\x1e"

// BatchCapturePane captures multiple panes in a single sh -c subprocess.
// targets maps "session:window" to the number of scrollback lines to capture.
// When escapes is true, ANSI color codes are preserved (-e flag).
func BatchCapturePane(targets map[string]int, escapes bool) (map[string]string, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	if len(targets) == 1 {
		for target, lines := range targets {
			content, err := CapturePane(target, lines)
			if err != nil {
				return nil, err
			}
			return map[string]string{target: content}, nil
		}
	}

	altStates := BatchIsAlternateScreen(targets)

	var script strings.Builder
	orderedTargets := make([]string, 0, len(targets))
	for target, lines := range targets {
		orderedTargets = append(orderedTargets, target)
		script.WriteString("printf '%s\\n' '")
		script.WriteString(target)
		script.WriteString("'\n")

		args := []string{"capture-pane"}
		if altStates[target] {
			args = append(args, "-a")
		}
		args = append(args, "-p", "-J")
		if escapes {
			args = append(args, "-e")
		}
		args = append(args, "-t", target)
		if !altStates[target] && lines > 0 {
			args = append(args, "-S", fmt.Sprintf("-%d", lines))
		}
		script.WriteString("tmux")
		for _, a := range args {
			script.WriteByte(' ')
			script.WriteString(a)
		}
		script.WriteString(" 2>/dev/null\n")
		script.WriteString("printf '\\x1e\\n'\n")
	}

	cmd := exec.Command("sh", "-c", script.String())
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("batch capture: %w — %s", err, errBuf.String())
	}

	results := make(map[string]string, len(targets))
	raw := out.String()
	sections := strings.Split(raw, batchSeparator+"\n")
	for _, section := range sections {
		section = strings.TrimRight(section, "\n")
		if section == "" {
			continue
		}
		nlIdx := strings.IndexByte(section, '\n')
		if nlIdx < 0 {
			continue
		}
		target := section[:nlIdx]
		content := section[nlIdx+1:]
		results[target] = strings.TrimRight(content, "\n")
	}
	return results, nil
}

// BatchIsAlternateScreen checks alternate screen state for all targets in a
// single tmux list-windows call, falling back to per-target checks on error.
func BatchIsAlternateScreen(targets map[string]int) map[string]bool {
	result := make(map[string]bool, len(targets))

	sessions := make(map[string]bool)
	for target := range targets {
		if i := strings.IndexByte(target, ':'); i >= 0 {
			sessions[target[:i]] = true
		}
	}

	for sess := range sessions {
		out, err := Exec("list-windows", "-t", sess, "-F",
			"#{window_index}"+"\x1f"+"#{alternate_on}")
		if err != nil {
			for target := range targets {
				if i := strings.IndexByte(target, ':'); i >= 0 && target[:i] == sess {
					result[target] = IsAlternateScreen(target)
				}
			}
			continue
		}
		for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			parts := strings.SplitN(line, "\x1f", 2)
			if len(parts) != 2 {
				continue
			}
			target := sess + ":" + parts[0]
			if _, ok := targets[target]; ok {
				result[target] = strings.TrimSpace(parts[1]) == "1"
			}
		}
	}
	return result
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

