package components

import (
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/lipgloss"
	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// allAnsiSeq matches any ANSI/VT escape sequence using the full ECMA-48 CSI
// grammar so that sequences with non-digit parameters (e.g. \x1b[>4m,
// \x1b[38:2:R:G:Bm, \x1b[=…, \x1b[<…) are also captured.
//
// ECMA-48 CSI structure:
//
//	\x1b[          — introducer
//	[\x30-\x3f]*   — parameter bytes (0-9 : ; < = > ?)
//	[\x20-\x2f]*   — intermediate bytes (space ! " # $ % & ' ( ) * + , - . /)
//	[@-~]          — final byte (one byte, 0x40–0x7E)
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
// Sequences like cursor movement, cursor positioning, scroll, and mode changes
// would move the cursor mid-render in Bubble Tea, visually corrupting the layout.
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
				return ""
			}
		}
		return match
	})
	s = strings.NewReplacer("\r", "", "\v", "", "\f", "").Replace(s)
	s = expandTabs(s)
	return s
}

// expandTabs replaces each horizontal tab with the number of spaces needed to
// reach the next 8-column tab stop, tracking column position across the string.
// ANSI escape sequences are treated as zero-width and do not advance the column.
// This is necessary because ansi.StringWidth counts \t as zero-width, so
// ansi.Cut (used by viewport) would not truncate tab-indented lines correctly.
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
			spaces := 8 - col%8
			for k := 0; k < spaces; k++ {
				buf.WriteByte(' ')
			}
			col += spaces
			i++
		case b == '\x1b':
			j := i + 1
			if j < len(s) && s[j] == '[' {
				j++
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
				if j < len(s) {
					j++
				}
			} else if j < len(s) {
				j++
			}
			buf.WriteString(s[i:j])
			i = j
		default:
			r, size := rune(b), 1
			if b >= 0x80 {
				var rr rune
				rr, size = decodeRune(s[i:])
				r = rr
			}
			buf.WriteString(s[i : i+size])
			col += runeDisplayWidth(r)
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
	size := 1
	for size < len(s) && size < 4 && (s[size]&0xc0) == 0x80 {
		size++
	}
	for _, r2 := range s[:size] {
		return r2, size
	}
	return r, size
}

// runeDisplayWidth returns the terminal display width of r.
func runeDisplayWidth(r rune) int {
	if r < 0x20 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	// Delegate to ansi for correct wide-char (East Asian) handling.
	switch {
	case r < 0x0300:
		return 1
	default:
		return lipgloss.Width(string(r))
	}
}

var previewLog *log.Logger

func init() {
	f, err := os.OpenFile(config.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
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
	Generation uint64
}

// SessionWindowGoneMsg is sent when capture-pane fails because the tmux
// window no longer exists.
type SessionWindowGoneMsg struct {
	SessionID string
}

// PollPreview returns a tea.Cmd that captures the current pane content.
// gen is a monotonically increasing generation counter incremented whenever
// the active session changes, so stale in-flight results can be discarded.
func PollPreview(sessionID, tmuxSession string, tmuxWindow int, interval time.Duration, gen uint64) tea.Cmd {
	return tea.Tick(interval, func(_ time.Time) tea.Msg {
		if tmuxSession == "" {
			return PreviewUpdatedMsg{SessionID: sessionID, Content: "", Generation: gen}
		}
		target := mux.Target(tmuxSession, tmuxWindow)
		content, err := mux.CapturePane(target, 500)
		if err != nil {
			if !mux.WindowExists(target) {
				previewLog.Printf("PollPreview: window gone session=%s target=%s gen=%d", sessionID, target, gen)
				return SessionWindowGoneMsg{SessionID: sessionID}
			}
			previewLog.Printf("PollPreview: CapturePane(%s) error: %v gen=%d", target, err, gen)
			return PreviewUpdatedMsg{SessionID: sessionID, Content: "", Generation: gen}
		}
		previewLog.Printf("PollPreview: session=%s contentLen=%d gen=%d", sessionID, len(content), gen)
		return PreviewUpdatedMsg{SessionID: sessionID, Content: content, Generation: gen}
	})
}

// Preview renders the terminal preview pane.
//
// Usage:
//
//	Call Resize(w, h) when terminal dimensions change (WindowSizeMsg).
//	Call SetContent(raw) when new capture-pane data arrives (PreviewUpdatedMsg).
//	Call View(activeSession) in the BubbleTea View method — it is pure.
type Preview struct {
	Width   int
	Height  int
	Focused bool
	vp         viewport.Model
	hasContent bool
}

// ScrollUp scrolls the preview viewport up by n lines.
func (p *Preview) ScrollUp(n int) { p.vp.LineUp(n) }

// ScrollDown scrolls the preview viewport down by n lines.
func (p *Preview) ScrollDown(n int) { p.vp.LineDown(n) }

// Resize updates the preview dimensions and passes them to the internal viewport.
// Must be called before the first View() call and whenever the terminal resizes.
// Scrolls back to the bottom when content is present, because a height change
// invalidates the previous YOffset.
func (p *Preview) Resize(w, h int) {
	p.Width = w
	p.Height = h
	p.vp.Width = w
	p.vp.Height = h
	if p.hasContent {
		// Set style before GotoBottom so maxYOffset uses the correct frame size.
		// Without this the zero-value style (no border) produces a YOffset that is
		// off by borderVerticalFrameSize, hiding the last few lines of content.
		// Guard against tiny viewports where the frame exceeds the height.
		// Always update vp.Style — if a previously-set style is left in place
		// on a now-tiny viewport, GotoBottom will compute maxYOffset using the
		// old frame size and scroll past the end of the content.
		if p.vp.Height > styles.PreviewStyle.GetVerticalFrameSize() {
			p.vp.Style = styles.PreviewStyle
		} else {
			p.vp.Style = lipgloss.Style{}
		}
		p.vp.GotoBottom()
	}
}

// SetContent sanitizes raw tmux capture-pane output and stores it in the
// viewport, scrolled to the bottom so the most recent output is visible.
// Pass an empty string to clear the pane (e.g. when switching sessions).
func (p *Preview) SetContent(content string) {
	p.hasContent = content != ""
	if p.hasContent {
		sanitized := sanitizePreviewContent(content)
		// Truncate lines to the inner viewport width.
		//
		// Content captured from wide tmux panes (e.g. 245-column) routinely
		// contains full-width separator bars and status lines that are exactly
		// the pane width.  When the preview inner width is narrower (typically
		// ~193 columns), those lines overflow the bordered box, making the
		// rendered preview wider than the terminal.  In a real terminal each
		// overlong row wraps into extra physical lines, which pushes the total
		// frame height over TermHeight and causes the terminal to scroll —
		// the "screen corruption" seen when switching sessions.
		if innerW := p.Width - styles.PreviewStyle.GetHorizontalFrameSize(); innerW > 0 {
			lines := strings.Split(sanitized, "\n")
			for i, l := range lines {
				lines[i] = xansi.Truncate(l, innerW, "")
			}
			sanitized = strings.Join(lines, "\n")
		}
		// Always update vp.Style before SetContent/GotoBottom so maxYOffset
		// uses the correct frame size.  Leaving a previously-set style in place
		// on a now-tiny viewport would compute a wrong YOffset.
		if p.vp.Height > styles.PreviewStyle.GetVerticalFrameSize() {
			p.vp.Style = styles.PreviewStyle
		} else {
			p.vp.Style = lipgloss.Style{}
		}
		p.vp.SetContent(sanitized)
		p.vp.GotoBottom()
	} else {
		p.vp.SetContent("")
	}
}

// View renders the preview pane.  It is pure: it reads state but does not
// mutate anything significant (only updates the viewport style for focus).
func (p *Preview) View(activeSession string) string {
	borderStyle := styles.PreviewStyle
	if p.Focused {
		borderStyle = styles.PreviewFocusedStyle
	}

	if !p.hasContent {
		// Placeholder: render a centred message inside the border.
		// Uses the same Width/Height/MaxWidth/MaxHeight pattern as viewport
		// to guarantee exact output dimensions.
		innerW := p.Width - borderStyle.GetHorizontalFrameSize()
		innerH := p.Height - borderStyle.GetVerticalFrameSize()
		if innerW < 1 {
			innerW = 1
		}
		if innerH < 1 {
			innerH = 1
		}
		msg := "Waiting for output…"
		if activeSession == "" {
			msg = "No active session\n\nSelect a session and press 'a' to attach"
		}
		content := lipgloss.NewStyle().
			Width(innerW).Height(innerH).
			MaxWidth(innerW).MaxHeight(innerH).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(styles.ColorMuted).
			Render(msg)
		return borderStyle.UnsetWidth().UnsetHeight().Render(content)
	}

	// Set the border style on the viewport so its View() wraps content in the
	// correct border (focused or unfocused).  The viewport computes inner
	// dimensions from Width/Height minus the style's frame size automatically.
	// Guard against degenerate sizes where the frame exceeds available space.
	p.vp.Style = borderStyle
	frameH := borderStyle.GetVerticalFrameSize()
	frameW := borderStyle.GetHorizontalFrameSize()
	if p.vp.Height > frameH && p.vp.Width > frameW {
		return p.vp.View()
	}
	// Dimensions too small to render a bordered viewport — return minimal output.
	return borderStyle.Render("")
}
