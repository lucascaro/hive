package tui

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/lucascaro/hive/internal/tui/components"
)

// paletteItems builds the command palette list from the command registry.
// Scope filters out actions that have no meaning in the active view (e.g.
// "Grid view" when already in grid). Actions whose Enabled predicate returns
// false are shown DIMMED rather than hidden so users still discover the
// keybinding even when the current selection makes the action unreachable.
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
		shortcut := ""
		if c.Binding != nil {
			shortcut = c.Binding(m.keys).Help().Key
		}
		if c.Enabled != nil && !c.Enabled(m) {
			items = append(items, components.NewDisabledPaletteItem(c.ID, c.Label, shortcut))
			continue
		}
		items = append(items, components.NewPaletteItem(c.ID, c.Label, shortcut))
	}
	return items
}
