package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/styles"
)

func newPreview(w, h int, content string) *Preview {
	p := &Preview{}
	p.Resize(w, h)
	p.SetContent(content)
	return p
}

func TestPreviewView_EmptyContent_NoSession(t *testing.T) {
	p := newPreview(80, 24, "")
	out := p.View("")
	if !strings.Contains(out, "No active session") {
		t.Error("empty content + no session should show 'No active session'")
	}
}

func TestPreviewView_EmptyContent_WithSession(t *testing.T) {
	p := newPreview(80, 24, "")
	out := p.View("sess-1")
	if !strings.Contains(out, "Waiting for output") {
		t.Error("empty content + session should show 'Waiting for output…'")
	}
}

func TestPreviewView_WithContent(t *testing.T) {
	p := newPreview(80, 24, "Hello, world!")
	out := p.View("sess-1")
	if !strings.Contains(out, "Hello, world!") {
		t.Error("content should be rendered")
	}
	if strings.Contains(out, "Waiting for output") {
		t.Error("should NOT show 'Waiting for output…' when content is present")
	}
}

func TestPreviewView_ContentTruncation(t *testing.T) {
	// Generate content with many lines
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line content here")
	}
	content := strings.Join(lines, "\n")

	p := newPreview(80, 10, content)
	out := p.View("sess-1")

	// Output should be bounded by height
	outLines := strings.Split(out, "\n")
	if len(outLines) > 12 { // height + some border/padding
		t.Errorf("output has %d lines, expected <= 12 for height=10", len(outLines))
	}
}

func TestPreviewView_MinDimensions(t *testing.T) {
	// Very small dimensions should not panic
	p := newPreview(1, 1, "test")
	out := p.View("sess-1")
	if out == "" {
		t.Error("should produce output even with tiny dimensions")
	}
}

func TestPreviewView_ZeroDimensions(t *testing.T) {
	// Zero dimensions should not panic
	p := newPreview(0, 0, "test")
	out := p.View("sess-1")
	if out == "" {
		t.Error("should produce output even with zero dimensions")
	}
}

func TestPreviewView_CaptureErrorContent(t *testing.T) {
	// When CapturePane fails, an empty content is now returned (error is logged).
	p := newPreview(80, 24, "")
	out := p.View("sess-1")
	if strings.Contains(out, "capture error") {
		t.Error("empty content should NOT show capture error text")
	}
}

func TestSanitizePreviewContent_KeepsSGR(t *testing.T) {
	// SGR (color) sequences should be preserved.
	input := "hello \x1b[32mgreen\x1b[0m world"
	out := sanitizePreviewContent(input)
	if !strings.Contains(out, "\x1b[32m") {
		t.Error("SGR sequence \x1b[32m should be preserved")
	}
	if !strings.Contains(out, "green") {
		t.Error("text should be preserved")
	}
}

func TestSanitizePreviewContent_StripsCursorMovement(t *testing.T) {
	// Cursor movement CSI sequences should be stripped.
	input := "before\x1b[3Aafter" // CUU - cursor up 3
	out := sanitizePreviewContent(input)
	if strings.Contains(out, "\x1b[3A") {
		t.Error("cursor movement should be stripped")
	}
	if !strings.Contains(out, "beforeafter") {
		t.Errorf("text should be preserved, got %q", out)
	}
}

func TestSanitizePreviewContent_StripsOSC(t *testing.T) {
	input := "before\x1b]2;My Title\x07after"
	out := sanitizePreviewContent(input)
	if strings.Contains(out, "My Title") {
		t.Error("OSC title should be stripped")
	}
	if !strings.Contains(out, "beforeafter") {
		t.Errorf("text should be preserved, got %q", out)
	}
}

func TestSanitizePreviewContent_StripsBEL(t *testing.T) {
	input := "before\aafter"
	out := sanitizePreviewContent(input)
	if strings.Contains(out, "\a") {
		t.Error("BEL character should be stripped")
	}
	if !strings.Contains(out, "beforeafter") {
		t.Errorf("text should be preserved, got %q", out)
	}
}

func TestSanitizePreviewContent_StripsPrivateMode(t *testing.T) {
	// Private mode sequences like \033[?1049h (alt screen), \033[?25l (hide cursor)
	input := "before\x1b[?1049hafter\x1b[?25l"
	out := sanitizePreviewContent(input)
	if strings.Contains(out, "\x1b[?") {
		t.Error("private mode sequences should be stripped")
	}
}

func TestSanitizePreviewContent_ExpandsTabs(t *testing.T) {
	// Tabs must be expanded to spaces. ansi.StringWidth counts \t as zero
	// width, so an unexpanded tab bypasses ansi.Truncate and then renders
	// wider than the box, causing lines to wrap and corrupt the layout.
	cases := []struct {
		name     string
		input    string
		wantNone string // must NOT appear in output
		wantSome string // must appear in output
	}{
		{
			name:     "single tab at start",
			input:    "\thello",
			wantNone: "\t",
			wantSome: "        hello", // 8 spaces to first tab stop
		},
		{
			name:     "tab after 4 chars reaches next 8-stop",
			input:    "abcd\tef",
			wantNone: "\t",
			wantSome: "abcd    ef", // 4 spaces to column 8
		},
		{
			name:     "two tabs from column 0",
			input:    "\t\tcode",
			wantNone: "\t",
			wantSome: "                code", // 16 spaces
		},
		{
			name:     "tab after newline resets column",
			input:    "line1\n\tline2",
			wantNone: "\t",
			wantSome: "line1\n        line2",
		},
		{
			name:     "no tabs unchanged",
			input:    "hello world",
			wantSome: "hello world",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := sanitizePreviewContent(tc.input)
			if tc.wantNone != "" && strings.Contains(out, tc.wantNone) {
				t.Errorf("output contains %q; got %q", tc.wantNone, out)
			}
			if tc.wantSome != "" && !strings.Contains(out, tc.wantSome) {
				t.Errorf("output missing %q; got %q", tc.wantSome, out)
			}
		})
	}
}

func TestExpandTabs(t *testing.T) {
	// expandTabs correctness: verify column-based expansion.
	cases := []struct {
		input string
		want  string
	}{
		{"\t", "        "},           // col 0 → 8 spaces
		{"a\t", "a       "},          // col 1 → 7 spaces to col 8
		{"abcdefg\t", "abcdefg "},    // col 7 → 1 space to col 8
		{"abcdefgh\t", "abcdefgh        "}, // col 8 → 8 spaces to col 16
		{"\t\t", "                "}, // 8+8 = 16 spaces
		{"hi\n\tbye", "hi\n        bye"},
		{"no tabs here", "no tabs here"},
	}
	for _, tc := range cases {
		got := expandTabs(tc.input)
		if got != tc.want {
			t.Errorf("expandTabs(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

func TestSanitizePreviewContent_StripsZeroWidthChars(t *testing.T) {
	// Zero-width characters have 0 display width but occupy real byte space.
	// lipgloss.Width() padding uses display width, so lines with zero-width
	// chars end up physically longer, causing misaligned borders.
	cases := []struct {
		name     string
		input    string
		wantNone string // must NOT appear in output
		wantSome string // must appear in output
	}{
		{
			name:     "zero-width space stripped",
			input:    "hello\u200Bworld",
			wantNone: "\u200B",
			wantSome: "helloworld",
		},
		{
			name:     "zero-width non-joiner stripped",
			input:    "test\u200Cvalue",
			wantNone: "\u200C",
			wantSome: "testvalue",
		},
		{
			name:     "zero-width joiner stripped",
			input:    "a\u200Db",
			wantNone: "\u200D",
			wantSome: "ab",
		},
		{
			name:     "BOM stripped",
			input:    "\uFEFFstart of line",
			wantNone: "\uFEFF",
			wantSome: "start of line",
		},
		{
			name:     "word joiner stripped",
			input:    "word\u2060joiner",
			wantNone: "\u2060",
			wantSome: "wordjoiner",
		},
		{
			name:     "soft hyphen stripped",
			input:    "hyphe\u00ADnated",
			wantNone: "\u00AD",
			wantSome: "hyphenated",
		},
		{
			name:     "multiple zero-width chars stripped",
			input:    "a\u200Bb\u200Cc\u200Dd",
			wantNone: "\u200B",
			wantSome: "abcd",
		},
		{
			name:     "no zero-width chars unchanged",
			input:    "hello world",
			wantSome: "hello world",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := sanitizePreviewContent(tc.input)
			if tc.wantNone != "" && strings.Contains(out, tc.wantNone) {
				t.Errorf("output contains %q; got %q", tc.wantNone, out)
			}
			if tc.wantSome != "" && !strings.Contains(out, tc.wantSome) {
				t.Errorf("output missing %q; got %q", tc.wantSome, out)
			}
		})
	}
}

func TestPreviewView_ZeroWidthChars_HeightInvariant(t *testing.T) {
	// Content with zero-width chars must render to exact height.
	// The ZWSP (U+200B) was causing misaligned borders in production.
	zwspContent := "line1\u200B\nline2\u200Bwith zwsp\nline3\n"
	zwspContent += strings.Repeat("normal line\u200B\n", 20)
	for _, h := range []int{10, 20, 30} {
		p := newPreview(80, h, zwspContent)
		out := p.View("sess-1")
		got := countLines(out)
		if got != h {
			t.Errorf("height=%d: got %d lines", h, got)
		}
	}
}

func TestPreviewView_ZeroWidthChars_WidthInvariant(t *testing.T) {
	// Lines with zero-width chars must have consistent display widths.
	// This test ensures the viewport renders uniformly without ZWSP causing
	// some lines to be physically longer.
	const w, h = 80, 10
	content := "normal line\nline with\u200Bzwsp\nanother\u200C\u200Dline\n"
	p := newPreview(w, h, content)
	out := p.View("sess-1")

	// Every rendered line must fit within p.Width display columns.
	for i, line := range strings.Split(out, "\n") {
		if lw := lipgloss.Width(line); lw > w {
			t.Errorf("line[%d] width=%d exceeds preview width %d", i, lw, w)
		}
	}
}

func TestPreviewView_TabContent_HeightInvariant(t *testing.T) {
	tabContent := "\t\tvar x = someFunction(\n\t\t\targument1,\n\t\t\targument2,\n\t\t)\n"
	tabContent += strings.Repeat("\tsome line of code\n", 20)
	for _, h := range []int{10, 20, 30} {
		p := newPreview(80, h, tabContent)
		out := p.View("sess-1")
		got := countLines(out)
		if got != h {
			t.Errorf("height=%d: got %d lines\n%s", h, got, out)
		}
	}
}

// countLines returns the number of lines in s (newline-separated).
func countLines(s string) int { return strings.Count(s, "\n") + 1 }

func TestPreviewView_ExactHeight(t *testing.T) {
	cases := []struct {
		name    string
		width   int
		height  int
		content string
		session string
	}{
		{"empty-no-session", 80, 24, "", ""},
		{"empty-with-session", 80, 24, "", "sess-1"},
		{"three-lines", 80, 24, "line1\nline2\nline3", "sess-1"},
		{"exact-fill", 80, 24, strings.Repeat("line\n", 20), "sess-1"},
		{"overflow-content", 80, 24, strings.Repeat("line\n", 100), "sess-1"},
		{"narrow", 40, 20, "hello world", "sess-1"},
		{"tall", 120, 50, "hello", "sess-1"},
		{"minimum", 10, 5, "hi", "sess-1"},
		{"wide-many-lines", 160, 40, strings.Repeat("x", 200) + strings.Repeat("\n"+strings.Repeat("y", 200), 50), "sess-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPreview(tc.width, tc.height, tc.content)
			out := p.View(tc.session)
			got := countLines(out)
			if got != tc.height {
				t.Errorf("View() = %d lines, want exactly %d (w=%d h=%d content=%q…)",
					got, tc.height, tc.width, tc.height, tc.content[:min(len(tc.content), 20)])
			}
		})
	}
}

func TestPreviewView_ShortContent_FillsHeight(t *testing.T) {
	// When content has fewer lines than innerH, the entire pane must still be
	// p.Height lines so Bubble Tea overwrites all previous terminal content.
	p := newPreview(80, 24, "only three lines\nof content\nhere")
	out := p.View("sess-1")
	if countLines(out) != 24 {
		t.Errorf("View() with short content = %d lines, want 24", countLines(out))
	}
}

func TestPreviewView_SwitchSession_FillsHeight(t *testing.T) {
	// Simulate what happens after switching sessions: content is cleared.
	// The preview must still fill p.Height lines so old content is overwritten.
	p := newPreview(80, 24, "")
	for _, session := range []string{"sess-1", ""} {
		out := p.View(session)
		if countLines(out) != 24 {
			t.Errorf("View() with empty content (session=%q) = %d lines, want 24", session, countLines(out))
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestPreviewView_WideLineTruncation(t *testing.T) {
	// Lines wider than the preview inner width (w - horizontalFrame) must be
	// truncated so the bordered viewport cannot render wider than p.Width.
	// Without truncation, full-width separator bars from wide tmux panes
	// (e.g. 245-column) overflow the 193-column inner area, making the
	// rendered preview box ~249 columns wide.  Combined with the sidebar the
	// total frame exceeds the terminal width, each row wraps into extra physical
	// lines, and the TUI scrolls ("screen corruption when switching sessions").
	const w, h = 80, 10
	// Inner width is determined by the preview style's horizontal frame size.
	innerW := w - styles.PreviewStyle.GetHorizontalFrameSize()
	wide := strings.Repeat("─", innerW+50) // substantially wider than allowed
	p := newPreview(w, h, wide)
	out := p.View("sess-1")

	if got := countLines(out); got != h {
		t.Errorf("height = %d, want %d", got, h)
	}
	// Every rendered line must fit within p.Width display columns.
	for i, line := range strings.Split(out, "\n") {
		if lw := lipgloss.Width(line); lw > w {
			t.Errorf("line[%d] width=%d exceeds preview width %d", i, lw, w)
		}
	}
}

func TestPreviewView_ExactHeight_ANSIContent(t *testing.T) {
	// Lines with SGR color codes must not affect the rendered line count.
	// These simulate real tmux capture-pane output after sanitization.
	coloredLines := []string{
		"\x1b[32mThis is green text\x1b[0m",
		"\x1b[34;1mThis is bold blue\x1b[0m",
		"\x1b[31mError: something failed\x1b[0m",
		"\x1b[0;33mwarning: check your input\x1b[0m",
		"\x1b[1;37m> \x1b[0muser prompt here",
	}

	cases := []struct {
		name    string
		width   int
		height  int
		content string
	}{
		{"few-colored-lines", 80, 24, strings.Join(coloredLines, "\n")},
		{"many-colored-lines", 80, 24, strings.Repeat(strings.Join(coloredLines, "\n")+"\n", 20)},
		{"narrow-colored", 40, 20, strings.Join(coloredLines, "\n")},
		// Long lines with colors that might overflow width limit
		{"long-colored-lines", 80, 24, strings.Repeat("\x1b[32m", 5) + strings.Repeat("x", 300) + "\x1b[0m\n" + "short line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPreview(tc.width, tc.height, tc.content)
			out := p.View("sess-1")
			got := countLines(out)
			if got != tc.height {
				t.Errorf("View() with ANSI content = %d lines, want exactly %d (w=%d h=%d)",
					got, tc.height, tc.width, tc.height)
			}
		})
	}
}

func TestStatusBarView_ExactHeight_ANSIContent(t *testing.T) {
	// Status bar must stay exactly 2 lines even when session info contains ANSI.
	// Agent badges and status dots inject ANSI color codes.
	sb := &StatusBar{Width: 80}
	appState := &state.AppState{
		Projects: []*state.Project{
			{
				ID:   "p1",
				Name: "my-project",
				Sessions: []*state.Session{
					{
						ID:        "s1",
						ProjectID: "p1",
						Title:     "very-long-session-title-that-might-cause-wrapping-issues",
						AgentType: state.AgentClaude,
						Status:    state.StatusRunning,
					},
				},
				Teams: []*state.Team{},
			},
		},
		ActiveProjectID: "p1",
		ActiveSessionID: "s1",
	}
	out := sb.View(appState, state.PaneSidebar, false, "")
	got := strings.Count(out, "\n") + 1
	if got != 2 {
		t.Errorf("StatusBar.View() with ANSI content = %d lines, want exactly 2", got)
	}
}

func TestSanitizePreviewContent_StripNonSGRVariants(t *testing.T) {
	// The ECMA-48 CSI grammar covers parameter bytes in \x30-\x3f.
	// Old regex only matched [0-9;?!], missing :, <, =, >.
	// Non-SGR sequences ending in 'm' but with non-SGR params (e.g. >4m)
	// must also be stripped so they can't move the cursor.
	cases := []struct {
		name    string
		input   string
		wantIn  string // must appear in output
		wantOut string // must NOT appear in output
	}{
		{
			name:    "colon-separated 24-bit color kept",
			input:   "hi\x1b[38:2:255:128:0mcolor\x1b[0m",
			wantIn:  "color",
			wantOut: "", // colon SGR should be kept as a color
		},
		{
			name:    "greater-than mode stripped",
			input:   "before\x1b[>4mafter",
			wantIn:  "beforeafter",
			wantOut: "\x1b[>4m",
		},
		{
			name:    "less-than sequence stripped",
			input:   "before\x1b[<0mafter",
			wantIn:  "beforeafter",
			wantOut: "\x1b[<",
		},
		{
			name:    "equals sequence stripped",
			input:   "before\x1b[=1mafter",
			wantIn:  "beforeafter",
			wantOut: "\x1b[=",
		},
		{
			name:    "intermediate byte sequence stripped",
			input:   "before\x1b[ @after",
			wantIn:  "beforeafter",
			wantOut: "\x1b[",
		},
		{
			name:    "colon SGR color codes kept and terminal-safe",
			input:   "\x1b[38:2:0:128:255mblue\x1b[0m text",
			wantIn:  "blue",
			wantOut: "\x1b[>",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := sanitizePreviewContent(tc.input)
			if tc.wantIn != "" && !strings.Contains(out, tc.wantIn) {
				t.Errorf("output missing %q; got %q", tc.wantIn, out)
			}
			if tc.wantOut != "" && strings.Contains(out, tc.wantOut) {
				t.Errorf("output still contains %q; got %q", tc.wantOut, out)
			}
		})
	}
}

func TestPreviewUpdatedMsg_Fields(t *testing.T) {
	msg := PreviewUpdatedMsg{
		SessionID:  "sess-123",
		Content:    "preview content",
		Generation: 42,
	}
	if msg.SessionID != "sess-123" {
		t.Errorf("SessionID=%q, want sess-123", msg.SessionID)
	}
	if msg.Content != "preview content" {
		t.Errorf("Content=%q, want 'preview content'", msg.Content)
	}
	if msg.Generation != 42 {
		t.Errorf("Generation=%d, want 42", msg.Generation)
	}
}
