package components

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKeyToBytes_PrintableRunes(t *testing.T) {
	cases := []struct {
		rune string
		want string
	}{
		{"y", "y"},
		{"n", "n"},
		{"a", "a"},
		{"Z", "Z"},
		{"1", "1"},
		{" ", " "},
	}
	for _, tc := range cases {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.rune)}
		got := keyToBytes(msg)
		if got != tc.want {
			t.Errorf("keyToBytes(%q) = %q, want %q", tc.rune, got, tc.want)
		}
	}
}

func TestKeyToBytes_SpecialKeys(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		want string
	}{
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, "\r"},
		{"backspace", tea.KeyMsg{Type: tea.KeyBackspace}, "\x7f"},
		{"delete", tea.KeyMsg{Type: tea.KeyDelete}, "\x1b[3~"},
		{"tab", tea.KeyMsg{Type: tea.KeyTab}, "\t"},
		{"space", tea.KeyMsg{Type: tea.KeySpace}, " "},
		{"up", tea.KeyMsg{Type: tea.KeyUp}, "\033[A"},
		{"down", tea.KeyMsg{Type: tea.KeyDown}, "\033[B"},
		{"right", tea.KeyMsg{Type: tea.KeyRight}, "\033[C"},
		{"left", tea.KeyMsg{Type: tea.KeyLeft}, "\033[D"},
		{"esc", tea.KeyMsg{Type: tea.KeyEscape}, "\033"},
		{"ctrl+c", tea.KeyMsg{Type: tea.KeyCtrlC}, "\x03"},
		{"ctrl+d", tea.KeyMsg{Type: tea.KeyCtrlD}, "\x04"},
		{"ctrl+a", tea.KeyMsg{Type: tea.KeyCtrlA}, "\x01"},
		{"ctrl+u", tea.KeyMsg{Type: tea.KeyCtrlU}, "\x15"},
	}
	for _, tc := range cases {
		got := keyToBytes(tc.msg)
		if got != tc.want {
			t.Errorf("keyToBytes(%s) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestKeyToBytes_UnknownKeyReturnsEmpty(t *testing.T) {
	// An unrecognised key type should return "" (not forwarded).
	msg := tea.KeyMsg{Type: tea.KeyF1}
	got := keyToBytes(msg)
	if got != "" {
		t.Errorf("keyToBytes(F1) = %q, want empty string", got)
	}
}
