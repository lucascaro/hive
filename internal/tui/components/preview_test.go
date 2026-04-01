package components

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/state"
)

func TestPreviewView_EmptyContent_NoSession(t *testing.T) {
	p := &Preview{Width: 80, Height: 24, Content: ""}
	out := p.View("")
	if !strings.Contains(out, "No active session") {
		t.Error("empty content + no session should show 'No active session'")
	}
}

func TestPreviewView_EmptyContent_WithSession(t *testing.T) {
	p := &Preview{Width: 80, Height: 24, Content: ""}
	out := p.View("sess-1")
	if !strings.Contains(out, "Waiting for output") {
		t.Error("empty content + session should show 'Waiting for output…'")
	}
}

func TestPreviewView_WithContent(t *testing.T) {
	p := &Preview{Width: 80, Height: 24, Content: "Hello, world!"}
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

	p := &Preview{Width: 80, Height: 10, Content: content}
	out := p.View("sess-1")

	// Output should be bounded by height
	outLines := strings.Split(out, "\n")
	if len(outLines) > 12 { // height + some border/padding
		t.Errorf("output has %d lines, expected <= 12 for height=10", len(outLines))
	}
}

func TestPreviewView_MinDimensions(t *testing.T) {
	// Very small dimensions should not panic
	p := &Preview{Width: 1, Height: 1, Content: "test"}
	out := p.View("sess-1")
	if out == "" {
		t.Error("should produce output even with tiny dimensions")
	}
}

func TestPreviewView_ZeroDimensions(t *testing.T) {
	// Zero dimensions should not panic
	p := &Preview{Width: 0, Height: 0, Content: "test"}
	out := p.View("sess-1")
	if out == "" {
		t.Error("should produce output even with zero dimensions")
	}
}

func TestPreviewView_CaptureErrorContent(t *testing.T) {
	// When CapturePane fails, PollPreview now returns an error message
	errContent := "[capture error: tmux capture-pane: exit status 1]"
	p := &Preview{Width: 80, Height: 24, Content: errContent}
	out := p.View("sess-1")
	if !strings.Contains(out, "capture error") {
		t.Error("capture error content should be displayed")
	}
	if strings.Contains(out, "Waiting for output") {
		t.Error("should NOT show waiting when there's error content")
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

func TestSanitizePreviewContent_StripsPrivateMode(t *testing.T) {
	// Private mode sequences like \033[?1049h (alt screen), \033[?25l (hide cursor)
	input := "before\x1b[?1049hafter\x1b[?25l"
	out := sanitizePreviewContent(input)
	if strings.Contains(out, "\x1b[?") {
		t.Error("private mode sequences should be stripped")
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
			p := &Preview{Width: tc.width, Height: tc.height, Content: tc.content}
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
	p := &Preview{Width: 80, Height: 24, Content: "only three lines\nof content\nhere"}
	out := p.View("sess-1")
	if countLines(out) != 24 {
		t.Errorf("View() with short content = %d lines, want 24", countLines(out))
	}
}

func TestPreviewView_SwitchSession_FillsHeight(t *testing.T) {
	// Simulate what happens after switching sessions: content is cleared.
	// The preview must still fill p.Height lines so old content is overwritten.
	p := &Preview{Width: 80, Height: 24, Content: ""}
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
			p := &Preview{Width: tc.width, Height: tc.height, Content: tc.content}
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
