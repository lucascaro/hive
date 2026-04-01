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
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// allAnsiSeq matches any ANSI/VT escape sequence using the full ECMA-48 CSI
// grammar so that sequences with non-digit parameters (e.g. \x1b[>4m,
// \x1b[38:2:R:G:Bm, \x1b[=…, \x1b[<…) are also captured.
//
// ECMA-48 CSI structure:
//   \x1b[  — introducer
//   [\x30-\x3f]*  — parameter bytes (0-9 : ; < = > ?)
//   [\x20-\x2f]*  — intermediate bytes (space ! " # $ % & ' ( ) * + , - . /)
//   [@-~]         — final byte (one byte, 0x40–0x7E)
//
// We use ReplaceAllStringFunc to keep only SGR (color/style) codes and
// strip everything else — cursor movement, screen-clear, mode changes, OSC,
// DCS, etc. — so they cannot corrupt Bubble Tea's cursor-based renderer.
var allAnsiSeq = regexp.MustCompile(
	`\x1b(?:` +
		`\[[\x30-\x3f]*[\x20-\x2f]*[@-~]` + // CSI (full ECMA-48 param range)
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
		// Keep only CSI sequences that are pure SGR: must start with \x1b[,
		// end with 'm', and have only digit/semicolon/colon parameter bytes
		// (no intermediate bytes, no < > = ? or other non-SGR chars).
		if len(match) < 3 || match[1] != '[' || match[len(match)-1] != 'm' {
			return ""
		}
		for i := 2; i < len(match)-1; i++ {
			c := match[i]
			if c != ';' && c != ':' && (c < '0' || c > '9') {
				return "" // not a valid SGR parameter byte
			}
		}
		return match
	})
	// Strip bare carriage returns and vertical-movement control chars that
	// could shift the cursor unexpectedly outside of ANSI sequences.
	s = strings.NewReplacer("\r", "", "\v", "", "\f", "").Replace(s)
	// Expand tab characters to spaces.  ansi.StringWidth counts \t as
	// zero-width, so ansi.Truncate does not account for the 1-8 display
	// columns a tab consumes when the terminal renders it to the next
	// 8-column tab stop.  Left unexpanded, a tab-indented line like
	// "\t\tcode..." passes Truncate but then wraps inside the preview box,
	// corrupting the layout.  We expand each tab to its correct number of
	// spaces relative to the current column position so the display width
	// matches what the terminal would show.
	s = expandTabs(s)
	return s
}

// expandTabs replaces each horizontal tab with the number of spaces needed to
// reach the next 8-column tab stop, tracking the current column position
// across the full string (resetting to 0 at each newline).  ANSI escape
// sequences are treated as zero-width so they do not advance the column.
func expandTabs(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	var buf strings.Builder
	buf.Grow(len(s))
	col := 0
	i := 0
	for i < len(s) {
		b := s[i]
		switch {
		case b == '\n':
			buf.WriteByte('\n')
			col = 0
			i++
		case b == '\t':
			// Advance to next multiple of 8.
			spaces := 8 - col%8
			for k := 0; k < spaces; k++ {
				buf.WriteByte(' ')
			}
			col += spaces
			i++
		case b == '\x1b':
			// Pass ANSI escape sequences through unchanged, contributing
			// zero to column width.
			j := i + 1
			if j < len(s) && s[j] == '[' {
				// CSI: read until a byte in the range 0x40–0x7E.
				j++
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
				if j < len(s) {
					j++ // include final byte
				}
			} else if j < len(s) {
				j++ // other 2-byte escape
			}
			buf.WriteString(s[i:j])
			i = j
		default:
			// Regular UTF-8 character: write the full rune and advance col.
			r, size := rune(b), 1
			if b >= 0x80 {
				// Decode the full rune.
				var rr rune
				rr, size = decodeRune(s[i:])
				r = rr
			}
			buf.WriteString(s[i : i+size])
			w := runeDisplayWidth(r)
			col += w
			i += size
		}
	}
	return buf.String()
}

// decodeRune decodes the first UTF-8 rune from s.
func decodeRune(s string) (rune, int) {
	r := rune(s[0])
	if r < 0x80 {
		return r, 1
	}
	// Count leading 1-bits to determine sequence length.
	size := 1
	for size < len(s) && size < 4 && (s[size]&0xc0) == 0x80 {
		size++
	}
	// Use the standard library via a quick conversion.
	for _, r2 := range s[:size] {
		return r2, size
	}
	return r, size
}

// runeDisplayWidth returns the terminal display width of r (0 for control
// chars, 1 for normal chars, 2 for wide East Asian chars).
func runeDisplayWidth(r rune) int {
	if r < 0x20 || (r >= 0x7f && r < 0xa0) {
		return 0 // control character
	}
	return ansi.StringWidth(string(r))
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
		target := mux.Target(tmuxSession, tmuxWindow)
		content, err := mux.CapturePane(target, 500)
		if err != nil {
			// Window may have been closed by the agent exiting — check before
			// reporting a generic error so the TUI can remove the dead session.
			if !mux.WindowExists(target) {
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
			// ansi.Truncate properly closes any open color sequences at the
			// truncation point, preventing color bleed into subsequent lines.
			lines[i] = ansi.Truncate(l, innerW, "")
		}
		// Pad to exactly innerH lines.  Lipgloss will also pad via Height(),
		// but explicit blank lines ensure every row in the content area is
		// written to the terminal even when switching to a session with
		// shorter output.  Without this, Bubble Tea's renderer may skip
		// writing blank terminal rows, leaving old session content visible.
		for len(lines) < innerH {
			lines = append(lines, "")
		}
		// Append a hard ANSI reset after the last real content line so that
		// any open color sequences from the captured pane do not bleed into
		// the blank padding rows that follow.
		if rawLineCount > 0 && rawLineCount <= innerH {
			lines[rawLineCount-1] += "\x1b[m"
		}
		content = strings.Join(lines, "\n")
		contentLineCount := strings.Count(content, "\n") + 1
		if contentLineCount != innerH {
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
