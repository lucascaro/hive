package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// TabStripPalette controls the colors used when rendering a tab strip.
// The strip is a 3-row "raised capsule": the active tab sits inside a
// rounded rectangle whose interior can share the canvas color (for a flat
// look, as in the help panel) or a lighter tint (for a seated-chrome look,
// as in settings).
type TabStripPalette struct {
	// CanvasBg is the background behind inactive tabs and separators.
	CanvasBg lipgloss.Color
	// ActiveBg is the background inside the active-tab capsule.
	// Set equal to CanvasBg for a flat look.
	ActiveBg lipgloss.Color
	// Accent is the frame color for the active capsule (the ╭╮╰╯│ glyphs).
	Accent lipgloss.Color
	// Baseline is the foreground color of the baseline dashes under inactive tabs.
	Baseline lipgloss.Color
	// ActiveText is the color of the active-tab label (rendered bold).
	ActiveText lipgloss.Color
	// InactiveText is the color of inactive-tab labels.
	InactiveText lipgloss.Color
}

// RenderTabStrip returns the three rows of the raised-capsule tab strip
// (top border, labels, baseline), each padded to printable width w. Labels
// degrade from full text to truncated to first-letter when the terminal is
// too narrow to fit them.
func RenderTabStrip(titles []string, activeTab, w int, p TabStripPalette) (top, mid, base string) {
	if len(titles) == 0 || w <= 0 {
		return "", "", ""
	}

	const leftGutter = 2
	sepNextToActive := 1
	sepBetweenInactive := 3

	cellW := func(i int, label string) int {
		L := ansi.StringWidth(label)
		if i == activeTab {
			return L + 4 // `│ label │`
		}
		return L
	}
	sepW := func(i int) int {
		if i == activeTab || i+1 == activeTab {
			return sepNextToActive
		}
		return sepBetweenInactive
	}
	fits := func(labels []string) bool {
		total := leftGutter
		for i, l := range labels {
			total += cellW(i, l)
			if i < len(labels)-1 {
				total += sepW(i)
			}
		}
		return total <= w
	}

	labels := make([]string, len(titles))
	copy(labels, titles)

	if !fits(labels) {
		for maxLen := 14; maxLen >= 2 && !fits(labels); maxLen-- {
			for i, t := range titles {
				if ansi.StringWidth(t) > maxLen {
					labels[i] = ansi.Truncate(t, maxLen, "…")
				} else {
					labels[i] = t
				}
			}
		}
	}
	if !fits(labels) {
		for i, t := range titles {
			runes := []rune(t)
			if len(runes) == 0 {
				labels[i] = "?"
			} else {
				labels[i] = string(runes[0])
			}
		}
	}

	bgStyle := lipgloss.NewStyle().Background(p.CanvasBg)
	baselineStyle := lipgloss.NewStyle().Foreground(p.Baseline).Background(p.CanvasBg)
	notchStyle := lipgloss.NewStyle().Foreground(p.Accent).Background(p.CanvasBg)
	frameOnCapsule := lipgloss.NewStyle().Foreground(p.Accent).Background(p.ActiveBg)
	frameOnBody := lipgloss.NewStyle().Foreground(p.Accent).Background(p.CanvasBg)
	activeInterior := lipgloss.NewStyle().Background(p.ActiveBg)
	activeLabel := lipgloss.NewStyle().Foreground(p.ActiveText).Background(p.ActiveBg).Bold(true)
	inactiveLabel := lipgloss.NewStyle().Foreground(p.InactiveText).Background(p.CanvasBg)

	var topB, midB, baseB strings.Builder
	spaces := func(n int) string { return strings.Repeat(" ", n) }
	dashes := func(n int) string { return strings.Repeat("─", n) }

	topB.WriteString(bgStyle.Render(spaces(leftGutter)))
	midB.WriteString(bgStyle.Render(spaces(leftGutter)))
	baseB.WriteString(baselineStyle.Render(dashes(leftGutter)))

	used := leftGutter
	for i, l := range labels {
		L := ansi.StringWidth(l)
		if i == activeTab {
			topB.WriteString(frameOnBody.Render("╭" + dashes(L+2) + "╮"))
			midB.WriteString(frameOnCapsule.Render("│"))
			midB.WriteString(activeInterior.Render(" "))
			midB.WriteString(activeLabel.Render(l))
			midB.WriteString(activeInterior.Render(" "))
			midB.WriteString(frameOnCapsule.Render("│"))
			baseB.WriteString(notchStyle.Render("╯"))
			baseB.WriteString(bgStyle.Render(spaces(L + 2)))
			baseB.WriteString(notchStyle.Render("╰"))
			used += L + 4
		} else {
			topB.WriteString(bgStyle.Render(spaces(L)))
			midB.WriteString(inactiveLabel.Render(l))
			baseB.WriteString(baselineStyle.Render(dashes(L)))
			used += L
		}
		if i < len(labels)-1 {
			sw := sepW(i)
			topB.WriteString(bgStyle.Render(spaces(sw)))
			midB.WriteString(bgStyle.Render(spaces(sw)))
			baseB.WriteString(baselineStyle.Render(dashes(sw)))
			used += sw
		}
	}

	if used < w {
		remaining := w - used
		topB.WriteString(bgStyle.Render(spaces(remaining)))
		midB.WriteString(bgStyle.Render(spaces(remaining)))
		baseB.WriteString(baselineStyle.Render(dashes(remaining)))
	}

	top = topB.String()
	mid = midB.String()
	base = baseB.String()
	if ansi.StringWidth(top) > w {
		top = ansi.Truncate(top, w, "")
	}
	if ansi.StringWidth(mid) > w {
		mid = ansi.Truncate(mid, w, "")
	}
	if ansi.StringWidth(base) > w {
		base = ansi.Truncate(base, w, "")
	}
	return top, mid, base
}
