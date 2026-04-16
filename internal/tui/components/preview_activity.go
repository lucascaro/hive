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

	// activityPipPieFrames is the rotating progress-pie animation used for the
	// input-focused session: empty → quarter → half → three-quarter → full.
	activityPipPieFrames = []string{"○", "◔", "◑", "◕", "●"}
	activityPipFocusStyle = lipgloss.NewStyle().Foreground(styles.ColorSuccess)
)

type PreviewActivityPanel struct {
	Width         int
	Sessions      []*state.Session
	LastChange    map[string]time.Time
	FlashDuration time.Duration
	// FocusedID, when non-empty, marks the session whose pip should render the
	// rotating progress-pie animation in green. Other sessions render a
	// standard on/off pip in yellow (background) or green (default).
	FocusedID string
	// PipFrame is the current animation frame counter. Used to index into
	// activityPipPieFrames for the focused session's progress-pie animation.
	PipFrame int
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
		var pip string
		isFocused := p.FocusedID != "" && sess.ID == p.FocusedID
		if isFocused {
			// Focused input-mode session: rotating green progress pie.
			frame := activityPipPieFrames[p.PipFrame%len(activityPipPieFrames)]
			pip = activityPipFocusStyle.Render(frame)
		} else if t, ok := p.LastChange[sess.ID]; ok && now.Sub(t) < flash {
			if p.FocusedID != "" {
				pip = activityPipActiveBackground
			} else {
				pip = activityPipActive
			}
		} else {
			pip = activityPipIdle
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
