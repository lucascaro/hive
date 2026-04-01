package components

import (
	"strings"
	"testing"
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
