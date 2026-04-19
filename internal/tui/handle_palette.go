package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/tui/components"
)

// handlePalettePicked dispatches the selected command palette action. Each
// palette item's ID is a command registry entry — we pop the palette view,
// find the command by ID, and invoke its executor. Palette and direct-key
// paths share the same executor so the two can never drift.
func (m Model) handlePalettePicked(msg components.CommandPalettePickedMsg) (tea.Model, tea.Cmd) {
	m.PopView()
	if c := findCommand(msg.Action); c != nil {
		return c.Exec(&m)
	}
	return m, nil
}
