package styles

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Base colors
	ColorAccent   = lipgloss.Color("#7C3AED") // purple
	ColorMuted    = lipgloss.Color("#6B7280")
	ColorSuccess  = lipgloss.Color("#10B981")
	ColorWarning  = lipgloss.Color("#F59E0B")
	ColorError    = lipgloss.Color("#EF4444")
	ColorText     = lipgloss.Color("#F9FAFB")
	ColorSubtext  = lipgloss.Color("#9CA3AF")
	ColorBorder   = lipgloss.Color("#374151")
	ColorSelected = lipgloss.Color("#1E3A5F")
	ColorBg       = lipgloss.Color("#111827")

	// Agent type colors
	AgentColors = map[string]lipgloss.Color{
		"claude":   lipgloss.Color("#F97316"), // orange
		"codex":    lipgloss.Color("#3B82F6"), // blue
		"gemini":   lipgloss.Color("#8B5CF6"), // violet
		"copilot":  lipgloss.Color("#10B981"), // green
		"aider":    lipgloss.Color("#EC4899"), // pink
		"opencode": lipgloss.Color("#06B6D4"), // cyan
		"custom":   lipgloss.Color("#6B7280"), // gray
	}

	// Layout
	SidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(ColorBorder).
			Padding(0, 1)

	PreviewStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(0, 1)

	PreviewFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorAccent).
				Padding(0, 1)

	StatusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1F2937")).
			Foreground(ColorText).
			Padding(0, 1)

	StatusBarAccentStyle = lipgloss.NewStyle().
				Background(ColorAccent).
				Foreground(ColorText).
				Padding(0, 1)

	// Sidebar items
	ProjectStyle = lipgloss.NewStyle().
			Foreground(ColorText).
			Bold(true)

	ProjectSelectedStyle = lipgloss.NewStyle().
				Foreground(ColorText).
				Background(ColorSelected).
				Bold(true)

	TeamStyle = lipgloss.NewStyle().
			Foreground(ColorSubtext).
			Italic(true)

	SessionStyle = lipgloss.NewStyle().
			Foreground(ColorText)

	SessionSelectedStyle = lipgloss.NewStyle().
				Foreground(ColorText).
				Background(ColorSelected)

	OrchestratorStyle = lipgloss.NewStyle().
				Foreground(ColorWarning)

	// Status dots
	DotRunning = lipgloss.NewStyle().Foreground(ColorSuccess).Render("●")
	DotIdle    = lipgloss.NewStyle().Foreground(ColorMuted).Render("○")
	DotWaiting = lipgloss.NewStyle().Foreground(ColorWarning).Render("◉")
	DotDead    = lipgloss.NewStyle().Foreground(ColorError).Render("✕")

	// Bell badge shown when a session has an unacknowledged bell.
	BellBadge = lipgloss.NewStyle().Foreground(ColorWarning).Render("♪")

	// Misc
	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	HelpKeyStyle  = lipgloss.NewStyle().Foreground(ColorAccent)
	HelpDescStyle = lipgloss.NewStyle().Foreground(ColorMuted)

	ErrorStyle = lipgloss.NewStyle().Foreground(ColorError)

	MutedStyle = lipgloss.NewStyle().Foreground(ColorMuted)

	// BreadcrumbSeparatorStyle styles the "/" between breadcrumb segments in
	// the status bar.  Accent color makes the path visually parse-able at a
	// glance instead of reading as one undifferentiated muted string.
	BreadcrumbSeparatorStyle = lipgloss.NewStyle().Foreground(ColorAccent)

	// WorktreeBadgeStyle is a touch brighter than MutedStyle so worktree
	// sessions get a subtle visual lift in the sidebar without competing
	// with the status dot or agent badge.
	WorktreeBadgeStyle = lipgloss.NewStyle().Foreground(ColorSubtext)
)

// ProjectPalette is a curated set of visually distinct colors for projects.
// These are chosen to be distinguishable on dark backgrounds.
var ProjectPalette = []string{
	"#7C3AED", // violet
	"#3B82F6", // blue
	"#10B981", // emerald
	"#F59E0B", // amber
	"#EF4444", // red
	"#EC4899", // pink
	"#06B6D4", // cyan
	"#F97316", // orange
	"#8B5CF6", // purple
	"#14B8A6", // teal
}

// NextProjectColor returns a palette color for the given index, cycling.
func NextProjectColor(index int) string {
	return ProjectPalette[index%len(ProjectPalette)]
}

// NextFreeColor returns the first palette color not present in usedColors.
// If all palette colors are used, it falls back to cycling by count.
func NextFreeColor(usedColors []string) string {
	used := make(map[string]bool, len(usedColors))
	for _, c := range usedColors {
		used[strings.ToUpper(c)] = true
	}
	for _, c := range ProjectPalette {
		if !used[strings.ToUpper(c)] {
			return c
		}
	}
	return NextProjectColor(len(usedColors))
}

// NextFreeSessionColor returns the first palette color not in usedColors and
// not equal to projectColor. This ensures session colors differ from each other
// and from their parent project's color.
func NextFreeSessionColor(projectColor string, usedColors []string) string {
	used := make(map[string]bool, len(usedColors)+1)
	used[strings.ToUpper(projectColor)] = true
	for _, c := range usedColors {
		used[strings.ToUpper(c)] = true
	}
	for _, c := range ProjectPalette {
		if !used[strings.ToUpper(c)] {
			return c
		}
	}
	// All palette colors are taken; fall back to cycling by count.
	return NextProjectColor(len(usedColors))
}

// CycleColor returns the next (direction=+1) or previous (direction=-1) palette
// color relative to the current color, skipping colors in usedByOthers.
func CycleColor(current string, direction int, usedByOthers []string) string {
	used := make(map[string]bool, len(usedByOthers))
	for _, c := range usedByOthers {
		used[strings.ToUpper(c)] = true
	}

	// Find current index in palette.
	startIdx := 0
	upper := strings.ToUpper(current)
	for i, c := range ProjectPalette {
		if strings.ToUpper(c) == upper {
			startIdx = i
			break
		}
	}

	n := len(ProjectPalette)
	for step := 1; step < n; step++ {
		idx := ((startIdx + direction*step) % n + n) % n
		if !used[strings.ToUpper(ProjectPalette[idx])] {
			return ProjectPalette[idx]
		}
	}
	// All colors used by others — just cycle without skipping.
	idx := ((startIdx + direction) % n + n) % n
	return ProjectPalette[idx]
}

// ContrastForeground returns a light or dark foreground color that contrasts
// well with the given hex background color (e.g. "#7C3AED").
// It picks whichever foreground yields the higher WCAG contrast ratio.
func ContrastForeground(hexBg string) lipgloss.Color {
	bgLum := relativeLuminance(hexBg)
	lightLum := relativeLuminance("#F9FAFB")
	darkLum := relativeLuminance("#1F2937")
	if contrastRatio(lightLum, bgLum) >= contrastRatio(darkLum, bgLum) {
		return lipgloss.Color("#F9FAFB") // light text on dark bg
	}
	return lipgloss.Color("#1F2937") // dark text on light bg
}

// contrastRatio computes the WCAG 2.0 contrast ratio between two luminances.
func contrastRatio(l1, l2 float64) float64 {
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// parseHexRGB extracts r, g, b components (0–255) from a "#RRGGBB" string.
func parseHexRGB(hex string) (uint8, uint8, uint8, bool) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0, false
	}
	r, err1 := strconv.ParseUint(hex[0:2], 16, 8)
	g, err2 := strconv.ParseUint(hex[2:4], 16, 8)
	b, err3 := strconv.ParseUint(hex[4:6], 16, 8)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	return uint8(r), uint8(g), uint8(b), true
}

// lerpColor linearly interpolates between two hex colors at t ∈ [0,1].
func lerpColor(from, to string, t float64) string {
	r1, g1, b1, ok1 := parseHexRGB(from)
	r2, g2, b2, ok2 := parseHexRGB(to)
	if !ok1 || !ok2 {
		return from
	}
	lerp := func(a, b uint8) uint8 {
		return uint8(float64(a) + t*(float64(b)-float64(a)) + 0.5)
	}
	return fmt.Sprintf("#%02X%02X%02X", lerp(r1, r2), lerp(g1, g2), lerp(b1, b2))
}

// GradientBg renders text with a background that transitions from colorA to
// colorB across the string width. Each rune gets an interpolated background
// and the appropriate contrast foreground. If both colors are the same or the
// string is empty, falls back to a flat background.
func GradientBg(text, colorA, colorB string, bold bool) string {
	if colorA == colorB || text == "" {
		fg := ContrastForeground(colorA)
		return lipgloss.NewStyle().
			Background(lipgloss.Color(colorA)).
			Foreground(fg).
			Bold(bold).
			Render(text)
	}
	runes := []rune(text)
	n := len(runes)
	var sb strings.Builder
	for i, r := range runes {
		t := 0.0
		if n > 1 {
			t = float64(i) / float64(n-1)
		}
		bg := lerpColor(colorA, colorB, t)
		fg := ContrastForeground(bg)
		sb.WriteString(lipgloss.NewStyle().
			Background(lipgloss.Color(bg)).
			Foreground(fg).
			Bold(bold).
			Render(string(r)))
	}
	return sb.String()
}

// GradientFg renders text with a foreground color gradient from colorA to
// colorB. Optionally applies a background color. If both colors are the same,
// falls back to flat foreground.
func GradientFg(text, colorA, colorB string, bg lipgloss.Color, hasBg, bold bool) string {
	if colorA == colorB || text == "" {
		st := lipgloss.NewStyle().Foreground(lipgloss.Color(colorA)).Bold(bold)
		if hasBg {
			st = st.Background(bg)
		}
		return st.Render(text)
	}
	runes := []rune(text)
	n := len(runes)
	var sb strings.Builder
	for i, r := range runes {
		t := 0.0
		if n > 1 {
			t = float64(i) / float64(n-1)
		}
		fg := lerpColor(colorA, colorB, t)
		st := lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Bold(bold)
		if hasBg {
			st = st.Background(bg)
		}
		sb.WriteString(st.Render(string(r)))
	}
	return sb.String()
}

// relativeLuminance computes sRGB relative luminance from a hex color string.
func relativeLuminance(hex string) float64 {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0 // dark fallback
	}
	r, err1 := strconv.ParseUint(hex[0:2], 16, 8)
	g, err2 := strconv.ParseUint(hex[2:4], 16, 8)
	b, err3 := strconv.ParseUint(hex[4:6], 16, 8)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0
	}
	return 0.2126*linearize(float64(r)/255) +
		0.7152*linearize(float64(g)/255) +
		0.0722*linearize(float64(b)/255)
}

func linearize(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// ProjectColorBar returns a 1-char-wide colored bar string for use in the sidebar.
func ProjectColorBar(hexColor string) string {
	return lipgloss.NewStyle().
		Background(lipgloss.Color(hexColor)).
		Render(" ")
}

// ProjectColorOrDefault returns the given color, or ColorAccent hex if empty.
func ProjectColorOrDefault(color string) string {
	if color == "" {
		return fmt.Sprintf("%s", ColorAccent)
	}
	return color
}

// AgentBadge returns a styled agent type badge.
func AgentBadge(agentType string) string {
	color, ok := AgentColors[agentType]
	if !ok {
		color = AgentColors["custom"]
	}
	return lipgloss.NewStyle().
		Foreground(color).
		Render("[" + agentType + "]")
}

// AgentBadgeOnBg returns a styled agent type badge with an explicit background.
func AgentBadgeOnBg(agentType string, bg lipgloss.Color) string {
	color, ok := AgentColors[agentType]
	if !ok {
		color = AgentColors["custom"]
	}
	return lipgloss.NewStyle().
		Foreground(color).
		Background(bg).
		Render("[" + agentType + "]")
}

// StatusDotOnBg returns a styled status dot with an explicit background.
func StatusDotOnBg(status string, bg lipgloss.Color) string {
	var fg lipgloss.Color
	var glyph string
	switch status {
	case "running":
		fg = ColorSuccess
		glyph = "●"
	case "idle":
		fg = ColorMuted
		glyph = "○"
	case "waiting":
		fg = ColorWarning
		glyph = "◉"
	case "dead":
		fg = ColorError
		glyph = "✕"
	default:
		fg = ColorMuted
		glyph = "○"
	}
	return lipgloss.NewStyle().Foreground(fg).Background(bg).Render(glyph)
}

// StatusDot returns the styled status indicator for a session status.
func StatusDot(status string) string {
	switch status {
	case "running":
		return DotRunning
	case "idle":
		return DotIdle
	case "waiting":
		return DotWaiting
	case "dead":
		return DotDead
	default:
		return DotIdle
	}
}

// StatusLegend renders a compact inline legend for session status colors.
func StatusLegend() string {
	return lipgloss.JoinHorizontal(lipgloss.Left,
		StatusDot("idle")+" idle",
		"  ",
		StatusDot("running")+" working",
		"  ",
		StatusDot("waiting")+" waiting",
		"  ",
		StatusDot("dead")+" dead",
	)
}
