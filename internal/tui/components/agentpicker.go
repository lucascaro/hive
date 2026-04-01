package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// AgentPickedMsg is sent when the user selects an agent type.
type AgentPickedMsg struct {
	AgentType state.AgentType
}

// agentItem implements list.Item for the agent picker.
type agentItem struct {
	agentType state.AgentType
	label     string
}

func (a agentItem) Title() string       { return styles.AgentBadge(string(a.agentType)) + " " + a.label }
func (a agentItem) Description() string { return "" }
func (a agentItem) FilterValue() string { return string(a.agentType) }

// DefaultAgentItems is the full list of agents in default order.
var DefaultAgentItems = []list.Item{
	agentItem{state.AgentClaude, "Claude (Anthropic)"},
	agentItem{state.AgentCodex, "Codex (OpenAI)"},
	agentItem{state.AgentGemini, "Gemini (Google)"},
	agentItem{state.AgentCopilot, "GitHub Copilot CLI"},
	agentItem{state.AgentAider, "Aider"},
	agentItem{state.AgentOpenCode, "OpenCode"},
	agentItem{state.AgentCustom, "Custom command"},
}

// AgentPicker is a modal list for choosing an agent type.
type AgentPicker struct {
	list        list.Model
	Active      bool
	Width       int
	Height      int
	filterQuery string
	allItems    []list.Item
}

// NewAgentPicker creates an AgentPicker with a compact single-line delegate.
func NewAgentPicker() AgentPicker {
	delegate := list.NewDefaultDelegate()
	delegate.SetHeight(1)
	delegate.SetSpacing(0)
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(styles.ColorAccent).
		BorderForeground(styles.ColorAccent)
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.Padding(0, 0, 0, 2)

	n := len(DefaultAgentItems)
	l := list.New(DefaultAgentItems, delegate, 38, n+2)
	l.Title = ""
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false) // We handle filtering ourselves.
	return AgentPicker{list: l}
}

// Show makes the picker visible with items sorted by usage score.
func (ap *AgentPicker) Show(sorted []list.Item) {
	ap.Active = true
	ap.filterQuery = ""
	ap.allItems = sorted
	ap.list.SetItems(sorted)
	ap.list.Select(0)
	ap.resizeList(len(sorted))
}

// Hide dismisses the picker.
func (ap *AgentPicker) Hide() {
	ap.Active = false
	ap.filterQuery = ""
}

// FilterQuery returns the current filter string (for testing).
func (ap *AgentPicker) FilterQuery() string { return ap.filterQuery }

// Update handles key events for the picker.
func (ap *AgentPicker) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !ap.Active {
		return nil, false
	}
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.String() {
		case "enter":
			item, ok := ap.list.SelectedItem().(agentItem)
			if ok {
				ap.Hide()
				return func() tea.Msg { return AgentPickedMsg{AgentType: item.agentType} }, true
			}
			return nil, true
		case "esc":
			if ap.filterQuery != "" {
				ap.filterQuery = ""
				ap.applyFilter()
				return nil, true
			}
			ap.Hide()
			return func() tea.Msg { return CancelledMsg{} }, true
		case "backspace":
			if len(ap.filterQuery) > 0 {
				ap.filterQuery = ap.filterQuery[:len(ap.filterQuery)-1]
				ap.applyFilter()
			}
			return nil, true
		case "up", "down":
			// Let the list handle navigation.
			var cmd tea.Cmd
			ap.list, cmd = ap.list.Update(msg)
			return cmd, true
		default:
			// Single printable character → add to filter.
			if len(m.String()) == 1 && m.String() >= " " {
				ap.filterQuery += m.String()
				ap.applyFilter()
				return nil, true
			}
		}
	}
	var cmd tea.Cmd
	ap.list, cmd = ap.list.Update(msg)
	return cmd, false
}

// applyFilter filters allItems by substring match against agent type and label.
func (ap *AgentPicker) applyFilter() {
	if ap.filterQuery == "" {
		ap.list.SetItems(ap.allItems)
	} else {
		filtered := FilterAgentItems(ap.allItems, ap.filterQuery)
		ap.list.SetItems(filtered)
	}
	ap.list.Select(0)
	ap.resizeList(len(ap.list.Items()))
}

func (ap *AgentPicker) resizeList(n int) {
	if n > 12 {
		n = 12
	}
	if n < 1 {
		n = 1
	}
	ap.list.SetSize(38, n+1)
}

// FilterAgentItems returns items whose agent type or label contains query as a substring (case-insensitive).
func FilterAgentItems(items []list.Item, query string) []list.Item {
	q := strings.ToLower(query)
	var out []list.Item
	for _, item := range items {
		ai, ok := item.(agentItem)
		if !ok {
			continue
		}
		if strings.Contains(strings.ToLower(string(ai.agentType)), q) ||
			strings.Contains(strings.ToLower(ai.label), q) {
			out = append(out, item)
		}
	}
	return out
}

// View renders the agent picker as a centered overlay.
func (ap *AgentPicker) View() string {
	if !ap.Active {
		return ""
	}

	filterLine := ""
	if ap.filterQuery != "" {
		filterLine = styles.HelpKeyStyle.Render("Search: "+ap.filterQuery) +
			lipgloss.NewStyle().Foreground(styles.ColorAccent).Render("▏") + "\n"
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(0, 1).
		Render(fmt.Sprintf("%s\n%s%s\n%s",
			styles.TitleStyle.Render("Select Agent"),
			filterLine,
			ap.list.View(),
			styles.MutedStyle.Render("↑/↓: navigate  type: search  enter: select  esc: cancel"),
		))
}

// CancelledMsg is sent when the user cancels a picker/dialog.
type CancelledMsg struct{}
