package tui

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/lucascaro/hive/internal/tui/components"
)

// paletteItems builds the command palette list from the command registry. The
// scope is picked from the active view so grid-only and sidebar-only entries
// surface (or hide) correctly. Entries whose Enabled predicate returns false
// are skipped entirely — simpler than a disabled-row style, and prevents
// confusion when a command would no-op on the current selection.
func (m *Model) paletteItems() []list.Item {
	scope := ScopeGlobal
	if m.TopView() == ViewGrid {
		scope = ScopeGrid
	}
	items := make([]list.Item, 0, len(commands))
	for i := range commands {
		c := &commands[i]
		if c.Scopes&scope == 0 {
			continue
		}
		if c.Enabled != nil && !c.Enabled(m) {
			continue
		}
		shortcut := ""
		if c.Binding != nil {
			shortcut = c.Binding(m.keys).Help().Key
		}
		items = append(items, components.NewPaletteItem(c.ID, c.Label, shortcut))
	}
	return items
}
