package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/tui/styles"
)

func (m Model) overlayView(overlay string) string {
	w := m.appState.TermWidth
	h := m.appState.TermHeight
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	// Place overlay centered over a dark background filling the terminal.
	return lipgloss.Place(w, h,
		lipgloss.Center, lipgloss.Center,
		overlay,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#111827")),
	)
}

func (m Model) renameDialogView() string {
	title := "Rename Project"
	if m.titleEditor.SessionID != "" {
		title = "Rename Session"
	} else if m.titleEditor.TeamID != "" {
		title = "Rename Team"
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(50).
		Render(
			styles.TitleStyle.Render(title) + "\n\n" +
				m.titleEditor.View() + "\n\n" +
				styles.MutedStyle.Render("enter: save  esc: cancel  ctrl+u: clear"),
		)
}

func (m Model) nameInputView(title, prompt, hint string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(56).
		Render(
			styles.TitleStyle.Render(title) + "\n\n" +
				prompt + "\n" +
				m.nameInput.View() + "\n\n" +
				styles.MutedStyle.Render(hint),
		)
}

func (m Model) dirConfirmView() string {
	dir := strings.TrimSpace(m.nameInput.Value())
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(56).
		Render(
			styles.TitleStyle.Render("New Project (2/2)") + "\n\n" +
				"Directory does not exist:\n" +
				styles.MutedStyle.Render(dir) + "\n\n" +
				"Create it?" + "\n\n" +
				styles.MutedStyle.Render("y/enter: create  n/esc: back"),
		)
}

func (m Model) helpView() string {
	m.helpModel.ShowAll = true
	m.helpModel.Width = m.appState.TermWidth - 8 // account for border + padding

	content := styles.TitleStyle.Render("Hive — Keyboard Shortcuts") + "\n\n" +
		m.helpModel.View(m.keys) + "\n\n" +
		styles.MutedStyle.Render("Press ? or esc to close")

	return lipgloss.Place(m.appState.TermWidth, m.appState.TermHeight,
		lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(styles.ColorAccent).
			Padding(1, 3).
			Render(content),
	)
}

func (m Model) tmuxHelpView() string {
	type binding struct{ key, desc string }
	bindings := []binding{
		{mux.DetachKey(), "detach from session (return to hive)"},
		{"ctrl+b c", "create a new window"},
		{"ctrl+b n / p", "next / previous window"},
		{"ctrl+b 0-9", "switch to window by number"},
		{"ctrl+b ,", "rename current window"},
		{"ctrl+b %", "split pane vertically"},
		{"ctrl+b \"", "split pane horizontally"},
		{"ctrl+b arrow", "navigate between panes"},
		{"ctrl+b z", "zoom/unzoom current pane"},
		{"ctrl+b x", "kill current pane"},
		{"ctrl+b [", "enter scroll/copy mode (q to exit)"},
		{"ctrl+b ]", "paste from tmux buffer"},
		{"ctrl+b ?", "show all tmux key bindings"},
		{"ctrl+b t", "show clock"},
		{"ctrl+b $", "rename current session"},
	}
	var rows []string
	for _, b := range bindings {
		row := fmt.Sprintf("  %s  %s",
			styles.HelpKeyStyle.Width(18).Render(b.key),
			styles.HelpDescStyle.Render(b.desc),
		)
		rows = append(rows, row)
	}
	content := styles.TitleStyle.Render("tmux Shortcuts Reference") + "\n\n" +
		strings.Join(rows, "\n") + "\n\n" +
		styles.MutedStyle.Render("Press H or esc to close")

	return lipgloss.Place(m.appState.TermWidth, m.appState.TermHeight,
		lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(styles.ColorAccent).
			Padding(1, 3).
			Render(content),
	)
}

// attachHintView renders the attach hint dialog content.
func (m Model) attachHintView() string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 3).
		Render(
			styles.TitleStyle.Render("Attaching to session") + "\n\n" +
				"You are about to attach to a running agent session.\n" +
				"The Hive TUI will be suspended while you work.\n\n" +
				lipgloss.NewStyle().Bold(true).Render("To return to Hive:") + "  press  " +
				lipgloss.NewStyle().
					Foreground(styles.ColorAccent).
					Bold(true).
					Render(mux.DetachKey()) +
				"\n\n" +
				styles.MutedStyle.Render("enter: proceed  d: don't show again  esc: cancel"),
		)
}

// whatsNewView renders the "What's New" changelog overlay.
func (m Model) whatsNewView() string {
	w := m.appState.TermWidth
	h := m.appState.TermHeight
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	// Size the dialog to fit comfortably.
	dialogW := w - 10
	if dialogW > 70 {
		dialogW = 70
	}
	if dialogW < 20 {
		dialogW = 20
	}
	dialogH := h - 8
	if dialogH > 30 {
		dialogH = 30
	}
	if dialogH < 5 {
		dialogH = 5
	}

	title := styles.TitleStyle.Render("What's New in Hive")
	hint := styles.MutedStyle.Render("enter/esc: close  d: don't show again  j/k ↑↓: scroll")
	body := m.whatsNewViewport.View()

	content := title + "\n\n" + body + "\n\n" + hint

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(dialogW).
		Height(dialogH).
		Render(content)

	return lipgloss.Place(w, h,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#111827")),
	)
}

// doAttach returns the tea.Cmd that performs session attachment.
func (m *Model) doAttach(sess SessionAttachMsg) tea.Cmd {
	target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
	restoreMode := sess.RestoreGridMode

	if !mux.UseExecAttach() {
		// Native backend: use the classic quit+restart path.
		m.attachPending = &sess
		return tea.Quit
	}

	header := buildSessionHeader(sess)
	script := mux.AttachScript(target, header)
	cmd := exec.Command("sh", "-c", script)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	os.Stdout.WriteString("\033[?1049l\033[2J\033[H\033[?1049h")

	// Start background bell watcher so custom audio plays and bell badges are
	// tracked while the BubbleTea event loop is suspended.
	watcher := newAttachBellWatcher()
	watcher.start(m.cfg.BellSound, buildSessionTargets(&m.appState))

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		newBells := watcher.stop()
		return AttachDoneMsg{Err: err, RestoreGridMode: restoreMode, NewBells: newBells}
	})
}

// RunAttach handles the attach flow for the native backend.
func RunAttach(sess SessionAttachMsg) error {
	fmt.Print("\033[22;0t")
	fmt.Printf("\033]0;%s\007", buildAttachTitle(sess))
	defer fmt.Print("\033[23;0t")

	target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
	return mux.Attach(target)
}

func buildSessionHeader(sess SessionAttachMsg) string {
	var dot string
	switch string(sess.Status) {
	case "running":
		dot = "●"
	case "waiting":
		dot = "◉"
	case "dead":
		dot = "✕"
	default:
		dot = "○"
	}

	esc := func(s string) string { return strings.ReplaceAll(s, "#", "##") }

	title := fmt.Sprintf("%s [%s] %s", dot, esc(string(sess.AgentType)), esc(sess.SessionTitle))
	if sess.ProjectName != "" {
		title += " · " + esc(sess.ProjectName)
	}
	if sess.WorktreePath != "" {
		if sess.WorktreeBranch != "" && sess.WorktreeBranch != sess.SessionTitle {
			title += " ⎇ " + esc(sess.WorktreeBranch)
		} else {
			title += " ⎇"
		}
	}
	return title
}

func buildAttachTitle(sess SessionAttachMsg) string {
	agent := string(sess.AgentType)
	if sess.ProjectName != "" && sess.SessionTitle != "" {
		return fmt.Sprintf("Hive | %s / %s (%s)", sess.ProjectName, sess.SessionTitle, agent)
	}
	if sess.SessionTitle != "" {
		return fmt.Sprintf("Hive | %s (%s)", sess.SessionTitle, agent)
	}
	if sess.ProjectName != "" {
		return fmt.Sprintf("Hive | %s (%s)", sess.ProjectName, agent)
	}
	return "Hive"
}
