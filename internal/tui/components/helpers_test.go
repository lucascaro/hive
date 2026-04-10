package components

import tea "github.com/charmbracelet/bubbletea"

// keyPress creates a tea.KeyMsg for a printable character key.
func keyPress(k string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// keyType creates a tea.KeyMsg for a special key type (enter, esc, arrows, etc.).
func keyType(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}
