package components

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/tmux"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// allAnsiSeq matches any ANSI/VT escape sequence.
// We use ReplaceAllStringFunc to keep only SGR (color/style) codes and
// strip everything else — cursor movement, screen-clear, mode changes, OSC,
// DCS, etc. — so they cannot corrupt Bubble Tea's cursor-based renderer.
var allAnsiSeq = regexp.MustCompile(
	`\x1b(?:` +
		`\[[0-9;?!]*[@-~]` + // CSI sequences  (\033[ params final)
		`|\][^\x07\x1b]*(?:\x07|\x1b\\)` + // OSC sequences (\033]...\007 or ST)
		`|P[^\x1b]*\x1b\\` + // DCS sequences  (\033P...\033\)
		`|[^[\]P]` + // Other single-char escapes
		`)`,
)

// sanitizePreviewContent strips all ANSI escape sequences that are NOT SGR
// (Select Graphic Rendition, i.e. color/style codes ending with 'm').
// Sequences like cursor movement (\033[nA/B/C/D), cursor positioning
// (\033[row;colH), scroll (\033[S/T), and mode changes (\033[?…h/l) would
// move the cursor mid-render in Bubble Tea, visually corrupting the layout.
func sanitizePreviewContent(s string) string {
	s = allAnsiSeq.ReplaceAllStringFunc(s, func(match string) string {
		// Keep only CSI sequences that end with 'm' (SGR).
		if len(match) >= 3 && match[1] == '[' && match[len(match)-1] == 'm' {
			return match
		}
		return ""
	})
	// Strip bare carriage returns and vertical-movement control chars that
	// could shift the cursor unexpectedly outside of ANSI sequences.
	s = strings.NewReplacer("\r", "", "\v", "", "\f", "").Replace(s)
	return s
}

var previewLog *log.Logger

func init() {
	f, err := os.OpenFile(config.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		previewLog = log.New(os.Stderr, "[preview] ", log.Ltime)
		return
	}
	previewLog = log.New(f, "[preview] ", log.Ltime|log.Lmicroseconds)
}

// PreviewUpdatedMsg is sent when capture-pane returns new content.
type PreviewUpdatedMsg struct {
	SessionID  string
	Content    string
	Generation uint64 // matches the poll generation that produced this msg
}

// SessionWindowGoneMsg is sent when capture-pane fails because the tmux
// window no longer exists (the agent process exited and closed the window).
type SessionWindowGoneMsg struct {
	SessionID string
}

// PollPreview returns a tea.Cmd that captures the current pane content.
// gen is a monotonically increasing generation counter; the app increments it
// whenever it switches sessions so that in-flight polls from prior sessions are
// recognised as stale and discarded without being rescheduled.
func PollPreview(sessionID, tmuxSession string, tmuxWindow int, interval time.Duration, gen uint64) tea.Cmd {
	return tea.Tick(interval, func(_ time.Time) tea.Msg {
		if tmuxSession == "" {
			previewLog.Printf("PollPreview: empty tmuxSession for %s gen=%d", sessionID, gen)
			return PreviewUpdatedMsg{SessionID: sessionID, Content: "", Generation: gen}
		}
		target := tmux.Target(tmuxSession, tmuxWindow)
		content, err := tmux.CapturePane(target, 500)
		if err != nil {
			// Window may have been closed by the agent exiting — check before
			// reporting a generic error so the TUI can remove the dead session.
			if !tmux.WindowExists(target) {
				previewLog.Printf("PollPreview: window gone for session=%s target=%s gen=%d", sessionID, target, gen)
				return SessionWindowGoneMsg{SessionID: sessionID}
			}
			previewLog.Printf("PollPreview: CapturePane(%s) error: %v gen=%d", target, err, gen)
			return PreviewUpdatedMsg{SessionID: sessionID, Content: fmt.Sprintf("[capture error: %v]", err), Generation: gen}
		}
		content = sanitizePreviewContent(content)
		previewLog.Printf("PollPreview: session=%s target=%s contentLen=%d gen=%d", sessionID, target, len(content), gen)
		return PreviewUpdatedMsg{SessionID: sessionID, Content: content, Generation: gen}
	})
}

// Preview renders the terminal preview pane.
type Preview struct {
	Width   int
	Height  int
	Content string
	Focused bool
}

// View renders the preview pane.
func (p *Preview) View(activeSession string) string {
	borderStyle := styles.PreviewStyle
	if p.Focused {
		borderStyle = styles.PreviewFocusedStyle
	}

	innerW := p.Width - 4 // border + padding
	innerH := p.Height - 2
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}

	var content string
	if p.Content == "" {
		if activeSession == "" {
			content = lipgloss.NewStyle().
				Width(innerW).
				Height(innerH).
				Align(lipgloss.Center, lipgloss.Center).
				Foreground(styles.ColorMuted).
				Render("No active session\n\nSelect a session and press 'a' to attach")
		} else {
			content = lipgloss.NewStyle().
				Width(innerW).
				Height(innerH).
				Align(lipgloss.Center, lipgloss.Center).
				Foreground(styles.ColorMuted).
				Render("Waiting for output…")
		}
	} else {
		// Truncate to fit. Use ansi.Truncate so we never cut through an
		// ANSI escape sequence — a raw byte slice would leave the terminal
		// in a broken color/state that corrupts all subsequent rendering.
		lines := strings.Split(strings.TrimRight(p.Content, "\n"), "\n")
		rawLineCount := len(lines)
		if len(lines) > innerH {
			lines = lines[len(lines)-innerH:]
		}
		for i, l := range lines {
			lines[i] = ansi.Truncate(l, innerW, "")
		}
		content = strings.Join(lines, "\n")
		contentLineCount := strings.Count(content, "\n") + 1
		if rawLineCount != contentLineCount || contentLineCount != innerH {
			previewLog.Printf("View LINES: rawLines=%d afterTruncate=%d innerH=%d contentLines=%d",
				rawLineCount, len(lines), innerH, contentLineCount)
		}
	}

	rendered := borderStyle.Width(p.Width - 2).Height(p.Height - 2).Render(content)
	renderedLines := strings.Count(rendered, "\n") + 1
	previewLog.Printf("View: w=%d h=%d innerW=%d innerH=%d contentLen=%d rendered=%d%s",
		p.Width, p.Height, innerW, innerH, len(p.Content), renderedLines,
		func() string {
			if renderedLines != p.Height {
				return fmt.Sprintf(" HEIGHT_MISMATCH(want=%d got=%d)", p.Height, renderedLines)
			}
			return ""
		}(),
	)
	return rendered
}
