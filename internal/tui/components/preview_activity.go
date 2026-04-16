package components

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// PreviewActivityPanelHeight is the reserved row count for the panel. Single
// horizontal line: pip + title pairs for every session, space-separated.
const PreviewActivityPanelHeight = 1

var (
	activityPipActive            = lipgloss.NewStyle().Foreground(styles.ColorSuccess).Render("●")
	activityPipActiveBackground   = lipgloss.NewStyle().Foreground(styles.ColorWarning).Render("●")
	activityPipIdle              = styles.MutedStyle.Render("○")
)

type PreviewActivityPanel struct {
	Width         int
	Sessions      []*state.Session
	LastChange    map[string]time.Time
	FlashDuration time.Duration
	// FocusedID, when non-empty, marks the session whose pip should render in
	// the "focused" color (green). All other sessions get the background color
	// (yellow). Used to distinguish the input-focused session in the grid.
	FocusedID string
}

func (p PreviewActivityPanel) View() string {
	if p.Width <= 0 {
		return ""
	}
	now := time.Now()
	flash := p.FlashDuration
	if flash <= 0 {
		flash = 150 * time.Millisecond
	}

	parts := make([]string, 0, len(p.Sessions))
	for _, sess := range p.Sessions {
		if sess == nil {
			continue
		}
		pip := activityPipIdle
		if t, ok := p.LastChange[sess.ID]; ok && now.Sub(t) < flash {
			if p.FocusedID != "" && sess.ID != p.FocusedID {
				pip = activityPipActiveBackground
			} else {
				pip = activityPipActive
			}
		}
		title := sess.Title
		if title == "" {
			title = sess.ID
		}
		parts = append(parts, pip+" "+styles.MutedStyle.Render(title))
	}
	line := strings.Join(parts, "  ")
	if ansi.StringWidth(line) > p.Width {
		line = ansi.Truncate(line, p.Width, "…")
	}
	pad := p.Width - ansi.StringWidth(line)
	if pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line
}
