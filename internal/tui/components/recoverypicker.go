package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// RecoveryPickerDoneMsg is sent when the user finishes the recovery-picker interaction.
// Selected contains the sessions the user chose to recover, with AgentType set.
type RecoveryPickerDoneMsg struct {
	Selected []state.RecoverableSession
}

// allAgentTypes is the ordered list used for cycling with ←/→.
var allAgentTypes = []state.AgentType{
	state.AgentClaude,
	state.AgentCodex,
	state.AgentGemini,
	state.AgentCopilot,
	state.AgentAider,
	state.AgentOpenCode,
	state.AgentCustom,
}

// RecoveryPicker is a multi-select overlay that lists orphaned hive-* tmux windows
// and lets the user choose which ones to recover. Each row allows cycling the
// detected agent type with ←/→ before confirming.
type RecoveryPicker struct {
	sessions  []state.RecoverableSession // editable copy (agent type may be changed)
	selected  []bool
	typeIdx   []int // index into allAgentTypes for each row
	cursor    int
	Active    bool
	Width     int
	Height    int
}

// NewRecoveryPicker creates a RecoveryPicker for the given sessions.
func NewRecoveryPicker(sessions []state.RecoverableSession) RecoveryPicker {
	rp := RecoveryPicker{
		sessions: make([]state.RecoverableSession, len(sessions)),
		selected: make([]bool, len(sessions)),
		typeIdx:  make([]int, len(sessions)),
		Active:   len(sessions) > 0,
	}
	copy(rp.sessions, sessions)
	for i, s := range sessions {
		rp.typeIdx[i] = agentTypeIndex(s.DetectedAgentType)
	}
	return rp
}

// agentTypeIndex returns the index of at in allAgentTypes, defaulting to AgentCustom.
func agentTypeIndex(at state.AgentType) int {
	for i, t := range allAgentTypes {
		if t == at {
			return i
		}
	}
	// Default to AgentCustom (last entry).
	return len(allAgentTypes) - 1
}

// Update handles keyboard navigation and selection.
func (r RecoveryPicker) Update(msg tea.KeyMsg) (RecoveryPicker, tea.Cmd) {
	n := len(r.sessions)
	switch msg.String() {
	case "up", "k":
		if r.cursor > 0 {
			r.cursor--
		}
	case "down", "j":
		if r.cursor < n-1 {
			r.cursor++
		}
	case " ":
		if n > 0 {
			r.selected[r.cursor] = !r.selected[r.cursor]
		}
	case "a":
		anyUnselected := false
		for _, s := range r.selected {
			if !s {
				anyUnselected = true
				break
			}
		}
		for i := range r.selected {
			r.selected[i] = anyUnselected
		}
	case "left", "h":
		if n > 0 {
			idx := r.typeIdx[r.cursor]
			idx = (idx - 1 + len(allAgentTypes)) % len(allAgentTypes)
			r.typeIdx[r.cursor] = idx
			r.sessions[r.cursor].DetectedAgentType = allAgentTypes[idx]
		}
	case "right", "l":
		if n > 0 {
			idx := r.typeIdx[r.cursor]
			idx = (idx + 1) % len(allAgentTypes)
			r.typeIdx[r.cursor] = idx
			r.sessions[r.cursor].DetectedAgentType = allAgentTypes[idx]
		}
	case "enter":
		var picked []state.RecoverableSession
		for i, sel := range r.selected {
			if sel {
				picked = append(picked, r.sessions[i])
			}
		}
		r.Active = false
		return r, func() tea.Msg { return RecoveryPickerDoneMsg{Selected: picked} }
	case "esc", "q":
		r.Active = false
		return r, func() tea.Msg { return RecoveryPickerDoneMsg{Selected: nil} }
	}
	return r, nil
}

// View renders the overlay content (without the outer overlay frame).
func (r *RecoveryPicker) View() string {
	if len(r.sessions) == 0 {
		return ""
	}

	width := r.Width
	if width < 20 {
		width = 80
	}
	innerWidth := width - 8 // account for border + padding
	if innerWidth < 40 {
		innerWidth = 40
	}

	var sb strings.Builder
	sb.WriteString(styles.TitleStyle.Render("Recover Orphaned Sessions") + "\n\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(styles.ColorSubtext).Render(
		"These hive-* tmux windows have no hive state entry.\n"+
			"Select which ones to recover into a \"Recovered Sessions\" project.\n",
	) + "\n")

	checkStyle := lipgloss.NewStyle().Foreground(styles.ColorSuccess)
	cursorStyle := lipgloss.NewStyle().Foreground(styles.ColorAccent).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(styles.ColorText)
	mutedStyle := lipgloss.NewStyle().Foreground(styles.ColorMuted)
	unknownStyle := lipgloss.NewStyle().Foreground(styles.ColorWarning)

	for i, sess := range r.sessions {
		cursor := "  "
		if i == r.cursor {
			cursor = cursorStyle.Render("▶ ")
		}
		check := mutedStyle.Render("[ ]")
		if r.selected[i] {
			check = checkStyle.Render("[✓]")
		}

		sessionLabel := fmt.Sprintf("%s:%d", sess.TmuxSession, sess.WindowIndex)
		windowName := sess.WindowName
		if windowName == "" {
			windowName = "-"
		}

		agentType := allAgentTypes[r.typeIdx[i]]
		agentStr := string(agentType)
		var agentLabel string
		if agentType == state.AgentCustom {
			agentLabel = unknownStyle.Render(fmt.Sprintf("%-9s", agentStr))
		} else {
			agentColor := styles.AgentColors[agentStr]
			agentLabel = lipgloss.NewStyle().Foreground(agentColor).Render(fmt.Sprintf("%-9s", agentStr))
		}

		if i == r.cursor {
			agentLabel = cursorStyle.Render("◀") + agentLabel + cursorStyle.Render("▶")
		} else {
			agentLabel = "  " + agentLabel + "  "
		}

		line := fmt.Sprintf("%s%s %s %s %s",
			cursor,
			check,
			normalStyle.Render(fmt.Sprintf("%-22s", sessionLabel)),
			agentLabel,
			mutedStyle.Render(windowName),
		)
		sb.WriteString(line + "\n")
	}

	// Pane preview for the current row.
	cur := r.sessions[r.cursor]
	if cur.PanePreview != "" {
		sb.WriteString("\n")
		previewLabel := fmt.Sprintf("Preview (%s:%d):", cur.TmuxSession, cur.WindowIndex)
		sb.WriteString(mutedStyle.Render(previewLabel) + "\n")

		lines := strings.Split(strings.TrimRight(cur.PanePreview, "\n"), "\n")
		maxLines := 6
		if len(lines) > maxLines {
			lines = lines[len(lines)-maxLines:]
		}
		for _, l := range lines {
			// Truncate long lines.
			if len(l) > innerWidth {
				l = l[:innerWidth]
			}
			sb.WriteString(mutedStyle.Render(l) + "\n")
		}
	}

	selectedCount := 0
	for _, s := range r.selected {
		if s {
			selectedCount++
		}
	}
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(styles.ColorSubtext).Render(
		fmt.Sprintf("%d of %d selected", selectedCount, len(r.sessions)),
	) + "\n\n")
	sb.WriteString(
		styles.HelpKeyStyle.Render("space") + " toggle  " +
			styles.HelpKeyStyle.Render("a") + " toggle all  " +
			styles.HelpKeyStyle.Render("↑/↓") + " navigate  " +
			styles.HelpKeyStyle.Render("←/→") + " agent type  " +
			styles.HelpKeyStyle.Render("enter") + " confirm  " +
			styles.HelpKeyStyle.Render("esc") + " skip",
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(min(72, r.Width-4)).
		Render(sb.String())
}
