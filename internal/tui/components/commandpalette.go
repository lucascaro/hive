package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// CommandPalettePickedMsg is sent when the user selects an action from the
// command palette. Action is the internal action name (e.g. "attach", "kill-session").
type CommandPalettePickedMsg struct {
	Action string
}

// Compile-time check.
var _ tea.Msg = CommandPalettePickedMsg{}

// PaletteItem represents a single action in the command palette.
type PaletteItem struct {
	action   string // internal action name
	label    string // human-readable title
	shortcut string // current keybinding (e.g. "enter", "ctrl+p")
}

// Title returns the label with the shortcut key appended in a muted style.
// Rendered on a single line: "Attach session          enter"
func (p PaletteItem) Title() string {
	if p.shortcut == "" {
		return p.label
	}
	return p.label + "  " + styles.MutedStyle.Render(p.shortcut)
}
func (p PaletteItem) Description() string { return "" }
func (p PaletteItem) FilterValue() string { return p.label }

// Shortcut returns the raw shortcut string (for testing).
func (p PaletteItem) Shortcut() string { return p.shortcut }

// NewPaletteItem creates a palette item with an action name, label, and shortcut.
func NewPaletteItem(action, label, shortcut string) PaletteItem {
	return PaletteItem{action: action, label: label, shortcut: shortcut}
}

// CommandPalette is a modal list for searching and invoking any action by name.
type CommandPalette struct {
	list        list.Model
	Active      bool
	Width       int
	Height      int
	filterQuery string
	allItems    []list.Item
}

// NewCommandPalette creates a CommandPalette with a compact two-line delegate
// that shows the action name and its keyboard shortcut.
func NewCommandPalette() CommandPalette {
	delegate := list.NewDefaultDelegate()
	delegate.SetHeight(1)
	delegate.SetSpacing(0)
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(styles.ColorAccent).
		BorderForeground(styles.ColorAccent)
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.Padding(0, 0, 0, 2)

	l := list.New(nil, delegate, 50, 10)
	l.Title = ""
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()
	return CommandPalette{list: l}
}

// Show makes the palette visible with the given items.
func (cp *CommandPalette) Show(items []list.Item) {
	cp.Active = true
	cp.filterQuery = ""
	cp.allItems = items
	cp.list.SetItems(items)
	cp.list.Select(0)
	cp.resizeList(len(items))
}

// Hide dismisses the palette.
func (cp *CommandPalette) Hide() {
	cp.Active = false
	cp.filterQuery = ""
}

// FilterQuery returns the current filter string (for testing).
func (cp *CommandPalette) FilterQuery() string { return cp.filterQuery }

// Update handles key events for the palette.
func (cp *CommandPalette) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !cp.Active {
		return nil, false
	}
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.String() {
		case "enter":
			item, ok := cp.list.SelectedItem().(PaletteItem)
			if ok {
				cp.Hide()
				return func() tea.Msg { return CommandPalettePickedMsg{Action: item.action} }, true
			}
			return nil, true
		case "esc":
			if cp.filterQuery != "" {
				cp.filterQuery = ""
				cp.applyFilter()
				return nil, true
			}
			cp.Hide()
			return func() tea.Msg { return CancelledMsg{} }, true
		case "backspace":
			if len(cp.filterQuery) > 0 {
				cp.filterQuery = cp.filterQuery[:len(cp.filterQuery)-1]
				cp.applyFilter()
			}
			return nil, true
		case "up", "down":
			var cmd tea.Cmd
			cp.list, cmd = cp.list.Update(msg)
			return cmd, true
		default:
			if len(m.String()) == 1 && m.String() >= " " {
				cp.filterQuery += m.String()
				cp.applyFilter()
				return nil, true
			}
		}
	}
	var cmd tea.Cmd
	cp.list, cmd = cp.list.Update(msg)
	return cmd, false
}

// applyFilter filters allItems by substring match on label.
func (cp *CommandPalette) applyFilter() {
	if cp.filterQuery == "" {
		cp.list.SetItems(cp.allItems)
	} else {
		q := strings.ToLower(cp.filterQuery)
		var filtered []list.Item
		for _, item := range cp.allItems {
			pi, ok := item.(PaletteItem)
			if !ok {
				continue
			}
			if strings.Contains(strings.ToLower(pi.label), q) {
				filtered = append(filtered, item)
			}
		}
		cp.list.SetItems(filtered)
	}
	cp.list.Select(0)
	cp.resizeList(len(cp.list.Items()))
}

func (cp *CommandPalette) resizeList(n int) {
	if n > 16 {
		n = 16
	}
	if n < 1 {
		n = 1
	}
	cp.list.SetSize(50, n+1)
}

// View renders the command palette as a centered overlay.
func (cp *CommandPalette) View() string {
	if !cp.Active {
		return ""
	}

	filterLine := ""
	if cp.filterQuery != "" {
		filterLine = styles.HelpKeyStyle.Render("Search: "+cp.filterQuery) +
			lipgloss.NewStyle().Foreground(styles.ColorAccent).Render("▏") + "\n"
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(0, 1).
		Render(fmt.Sprintf("%s\n%s%s\n%s",
			styles.TitleStyle.Render("Command Palette"),
			filterLine,
			cp.list.View(),
			styles.MutedStyle.Render("↑/↓: navigate  type: search  enter: run  esc: cancel"),
		))
}
