package escape

import (
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/state"
)

// TitlesDetectedMsg is sent when WatchTitles finds agent-set titles via escape sequences.
// It carries all sessions with detected titles so callers can update them in one pass.
type TitlesDetectedMsg struct {
	Titles map[string]string // sessionID → title
}

// WatchTitles returns a tea.Cmd that polls all active sessions for title escape sequences.
// All sessions with a detected title are returned in a single TitlesDetectedMsg to avoid
// wasting subprocess calls on an early-exit pattern.
// sessionTargets maps sessionID → "tmuxSession:windowIdx".
func WatchTitles(sessionTargets map[string]string, interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(_ time.Time) tea.Msg {
		found := make(map[string]string)
		for sessionID, target := range sessionTargets {
			raw, err := mux.CapturePaneRaw(target, 200)
			if err != nil {
				continue
			}
			if title := ExtractTitle(raw); title != "" {
				found[sessionID] = title
			}
		}
		if len(found) == 0 {
			return nil
		}
		return TitlesDetectedMsg{Titles: found}
	})
}

// SessionDetectionCtx holds compiled regexes and config for a single session's
// status detection. Built once at startup from config.StatusDetection.
type SessionDetectionCtx struct {
	WaitTitleRe  *regexp.Regexp
	RunTitleRe   *regexp.Regexp
	WaitPromptRe *regexp.Regexp
	IdlePromptRe *regexp.Regexp // if set and NOT matched on stable content → waiting
	StableTicks  int
}

// StatusesDetectedMsg carries fresh status readings and updated content snapshots
// for all polled sessions.
type StatusesDetectedMsg struct {
	Statuses map[string]state.SessionStatus // sessionID → detected status
	Contents map[string]string              // sessionID → captured pane content (for next diff)
	// Titles is a pass-through of the per-target pane title map captured at
	// schedule time and used for tier-1 status detection.  Keyed by target
	// ("tmuxSession:windowIdx") to match GetPaneTitles' return shape — not by
	// sessionID like the other fields.  Forwarded so the TUI can surface live
	// agent titles in the grid view.
	Titles map[string]string
	// Bells carries per-target bell flags from tmux's #{window_bell_flag}.
	// Keyed by target ("tmuxSession:windowIdx"), true when a bell has fired.
	Bells map[string]bool
}

// WatchStatuses returns a tea.Cmd that captures pane content for all active sessions
// and derives running/idle/waiting status using combined heuristics:
//
//  1. Content changed → always StatusRunning (real-time, overrides stale titles).
//  2. Content stable for fewer than StableTicks → StatusRunning (debounce).
//  3. Pane title regex match → StatusWaiting or StatusRunning.
//  4. WaitPrompt matches last content line → StatusWaiting.
//  5. IdlePrompt configured: match → StatusIdle, no match → StatusWaiting
//     (agent stopped but isn't at its rest prompt, likely asking a question).
//  6. Fallback → StatusIdle.
//
// Parameters:
//   - sessionTargets: sessionID → "tmuxSession:windowIdx"
//   - prevContents: sessionID → last captured content (for diff)
//   - stableCounts: sessionID → consecutive stable polls (for debounce)
//   - detection: sessionID → compiled detection context
//   - titles: "tmuxSession:windowIdx" → pane title (from GetPaneTitles)
//   - bells: "tmuxSession:windowIdx" → true when bell flag set (from GetPaneTitles)
func WatchStatuses(
	sessionTargets map[string]string,
	prevContents map[string]string,
	stableCounts map[string]int,
	detection map[string]SessionDetectionCtx,
	titles map[string]string,
	bells map[string]bool,
	interval time.Duration,
) tea.Cmd {
	return tea.Tick(interval, func(_ time.Time) tea.Msg {
		statuses := make(map[string]state.SessionStatus, len(sessionTargets))
		contents := make(map[string]string, len(sessionTargets))
		for sessionID, target := range sessionTargets {
			content, err := mux.CapturePane(target, 50)
			if err != nil {
				continue
			}
			contents[sessionID] = content

			det, hasDet := detection[sessionID]
			title := titles[target]

			// Always check content diff first — content changes are real-time
			// and override potentially stale pane titles.
			contentChanged := content != prevContents[sessionID]
			if contentChanged {
				statuses[sessionID] = state.StatusRunning
				continue
			}

			// Content is stable. Check debounce window before any idle/waiting decision.
			stableCount := stableCounts[sessionID] + 1
			stableTicks := 2 // default
			if hasDet && det.StableTicks > 0 {
				stableTicks = det.StableTicks
			}
			if stableCount < stableTicks {
				statuses[sessionID] = state.StatusRunning
				continue
			}

			// Content stable past debounce. Now check pane title (tier 1).
			if hasDet && title != "" {
				if det.WaitTitleRe != nil && det.WaitTitleRe.MatchString(title) {
					statuses[sessionID] = state.StatusWaiting
					continue
				}
				if det.RunTitleRe != nil && det.RunTitleRe.MatchString(title) {
					statuses[sessionID] = state.StatusRunning
					continue
				}
			}

			// Tier 2: prompt pattern on last line.
			if hasDet && det.WaitPromptRe != nil && matchesLastLine(content, det.WaitPromptRe) {
				statuses[sessionID] = state.StatusWaiting
				continue
			}

			// IdlePrompt: if configured, the agent's rest prompt (e.g. "> ").
			// Match → idle (at prompt). No match → waiting (agent stopped but
			// isn't at its prompt, likely asking a question).
			if hasDet && det.IdlePromptRe != nil {
				if matchesLastLine(content, det.IdlePromptRe) {
					statuses[sessionID] = state.StatusIdle
				} else {
					statuses[sessionID] = state.StatusWaiting
				}
				continue
			}

			statuses[sessionID] = state.StatusIdle
		}
		return StatusesDetectedMsg{Statuses: statuses, Contents: contents, Titles: titles, Bells: bells}
	})
}

// matchesLastLine tests re against the last non-empty line of content.
func matchesLastLine(content string, re *regexp.Regexp) bool {
	line := lastNonEmptyLine(content)
	if line == "" {
		return false
	}
	// Strip ANSI escape sequences before matching.
	line = stripANSI(line)
	return re.MatchString(line)
}

// lastNonEmptyLine returns the last line from content that has visible text
// (after stripping ANSI codes and whitespace). The original line is returned
// intact so the caller can strip ANSI separately for regex matching.
// This correctly skips lines that contain only ANSI reset sequences (e.g.
// \x1b[0m) which tmux emits on blank lines below visible content.
func lastNonEmptyLine(content string) string {
	lines := strings.Split(content, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(stripANSI(lines[i])) != "" {
			return lines[i]
		}
	}
	return ""
}

// ansiRe matches ANSI escape sequences (CSI and OSC).
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07`)

// stripANSI removes ANSI escape sequences from s.
func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}
