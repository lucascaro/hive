package styles

import "github.com/charmbracelet/lipgloss"

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

	// Misc
	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	HelpKeyStyle  = lipgloss.NewStyle().Foreground(ColorAccent)
	HelpDescStyle = lipgloss.NewStyle().Foreground(ColorMuted)

	ErrorStyle = lipgloss.NewStyle().Foreground(ColorError)

	MutedStyle = lipgloss.NewStyle().Foreground(ColorMuted)
)

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
