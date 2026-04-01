package components

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// Confirm renders a yes/no confirmation overlay.
type Confirm struct {
	Message string
	Action  string
	Width   int
}

// View renders the confirmation dialog centered in the given dimensions.
func (c *Confirm) View() string {
	if c.Message == "" {
		return ""
	}
	dialog := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorWarning).
		Padding(1, 2).
		Width(50).
		Align(lipgloss.Center).
		Render(
			styles.TitleStyle.Render("Confirm") + "\n\n" +
				c.Message + "\n\n" +
				styles.HelpKeyStyle.Render("y/enter") + ": yes  " +
				styles.HelpKeyStyle.Render("n/esc") + ": no",
		)
	return dialog
}
