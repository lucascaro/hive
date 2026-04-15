package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// TestWritePrimaryBufferClear verifies that writePrimaryBufferClear writes
// exactly the right ANSI escape sequence to blank the primary screen buffer.
//
// Background: before entering alt-screen BubbleTea saves the primary buffer.
// Any exit of alt-screen (during attach or detach) then reveals that saved
// buffer. If it contains old terminal history the user sees a flash. By
// writing \033[2J\033[H to the primary buffer first we ensure the saved
// snapshot is blank.
//
// The sequence must:
//   - contain \033[2J (erase entire display)
//   - contain \033[H  (cursor home)
//   - NOT contain \033[?1049l (exit alt-screen), which would itself trigger
//     a flash of the primary buffer before it has been cleared.
func TestWritePrimaryBufferClear(t *testing.T) {
	var buf bytes.Buffer
	writePrimaryBufferClear(&buf)
	got := buf.String()

	if !strings.Contains(got, "\033[2J") {
		t.Errorf("writePrimaryBufferClear output missing \\033[2J; got %q", got)
	}
	if !strings.Contains(got, "\033[H") {
		t.Errorf("writePrimaryBufferClear output missing \\033[H; got %q", got)
	}
	if strings.Contains(got, "\033[?1049l") {
		t.Errorf("writePrimaryBufferClear output contains \\033[?1049l (alt-screen exit), which would cause a terminal flash; got %q", got)
	}
}
