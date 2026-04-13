package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// OrphanPickerDoneMsg is sent when the user finishes the orphan-picker interaction.
// Selected contains the tmux session names the user chose to kill (may be empty).
type OrphanPickerDoneMsg struct {
	Selected []string
}

// OrphanPicker is a multi-select overlay that lists orphaned hive-* tmux sessions
// and lets the user choose which ones to clean up.
type OrphanPicker struct {
	sessions []string // all orphaned session names
	selected []bool   // parallel slice: true = marked for deletion
	cursor   int
	Active   bool
	Width    int
	Height   int
}

// NewOrphanPicker creates an OrphanPicker for the given session names.
func NewOrphanPicker(sessions []string) OrphanPicker {
	return OrphanPicker{
		sessions: sessions,
		selected: make([]bool, len(sessions)),
		Active:   len(sessions) > 0,
	}
}

// Update handles keyboard navigation and selection.
func (o OrphanPicker) Update(msg tea.KeyMsg) (OrphanPicker, tea.Cmd) {
	switch msg.String() {
	case "up":
		if o.cursor > 0 {
			o.cursor--
		}
	case "down":
		if o.cursor < len(o.sessions)-1 {
			o.cursor++
		}
	case " ":
		if len(o.sessions) > 0 {
			o.selected[o.cursor] = !o.selected[o.cursor]
		}
	case "a":
		// Toggle all: if any unselected, select all; otherwise deselect all.
		anyUnselected := false
		for _, s := range o.selected {
			if !s {
				anyUnselected = true
				break
			}
		}
		for i := range o.selected {
			o.selected[i] = anyUnselected
		}
	case "enter":
		var picked []string
		for i, s := range o.selected {
			if s {
				picked = append(picked, o.sessions[i])
			}
		}
		o.Active = false
		return o, func() tea.Msg { return OrphanPickerDoneMsg{Selected: picked} }
	case "esc", "q":
		o.Active = false
		return o, func() tea.Msg { return OrphanPickerDoneMsg{Selected: nil} }
	}
	return o, nil
}

// View renders the overlay content (without the outer overlay frame).
func (o *OrphanPicker) View() string {
	if len(o.sessions) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(styles.TitleStyle.Render("Orphaned Tmux Sessions") + "\n\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(styles.ColorSubtext).Render(
		"These hive-* tmux sessions are running but have no entry in hive state.\n"+
			"Select which ones to clean up.\n",
	) + "\n")

	checkStyle := lipgloss.NewStyle().Foreground(styles.ColorSuccess)
	cursorStyle := lipgloss.NewStyle().Foreground(styles.ColorAccent).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(styles.ColorText)
	mutedStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)

	for i, name := range o.sessions {
		cursor := "  "
		if i == o.cursor {
			cursor = cursorStyle.Render("▶ ")
		}
		check := mutedStyle.Render("[ ]")
		if o.selected[i] {
			check = checkStyle.Render("[✓]")
		}
		line := fmt.Sprintf("%s%s %s", cursor, check, normalStyle.Render(name))
		sb.WriteString(line + "\n")
	}

	selectedCount := 0
	for _, s := range o.selected {
		if s {
			selectedCount++
		}
	}
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(styles.ColorSubtext).Render(
		fmt.Sprintf("%d of %d selected", selectedCount, len(o.sessions)),
	) + "\n\n")
	sb.WriteString(
		styles.HelpKeyStyle.Render("space") + " toggle  " +
			styles.HelpKeyStyle.Render("a") + " toggle all  " +
			styles.HelpKeyStyle.Render("↑/↓") + " navigate  " +
			styles.HelpKeyStyle.Render("enter") + " confirm  " +
			styles.HelpKeyStyle.Render("esc") + " skip",
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorWarning).
		Padding(1, 2).
		Width(min(60, o.Width-4)).
		Render(sb.String())
}
