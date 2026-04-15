package tui

import (
	"fmt"
	"io"
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
	m.helpPanel.Width = m.appState.TermWidth
	m.helpPanel.Height = m.appState.TermHeight
	m.helpPanel.HelpKeyLabel = m.keys.HelpKeyLabel()
	m.helpPanel.QuitKeyLabel = m.keys.QuitKeyLabel()
	return m.helpPanel.View(m.keys)
}

// gridInputHintView renders the first-use hint for grid input mode.
func (m Model) gridInputHintView() string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 3).
		Render(
			styles.TitleStyle.Render("Grid Input Mode") + "\n\n" +
				"Keystrokes are now forwarded to this session.\n" +
				"The grid stays visible — you remain in overview.\n\n" +
				lipgloss.NewStyle().Bold(true).Render("To return to navigation:") + "  press  " +
				lipgloss.NewStyle().
					Foreground(styles.ColorAccent).
					Bold(true).
					Render("Ctrl+Q") +
				"\n\n" +
				styles.MutedStyle.Render("enter: continue  d: don't show again  esc: cancel"),
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

// altScreenExecCmd wraps an exec.Cmd as a tea.ExecCommand that re-enters
// alt-screen at the very start of Run(). BubbleTea's ReleaseTerminal exits
// alt-screen (writing \033[?1049l) and sleeps 10ms before calling Run(),
// during which the primary terminal buffer (blank from the startup pre-clear
// in writePrimaryBufferClear) is briefly visible. By writing
// \033[?1049h\033[2J\033[H immediately in Run() we minimize that gap to
// just the 10ms sleep — down from 10ms + subprocess startup latency (~50ms).
//
// termOut is the writer for the enter-alt-screen sequence. It is separate
// from cmd.Stdout so it can be injected in tests without affecting the
// subprocess's own stdout.
type altScreenExecCmd struct {
	cmd     *exec.Cmd
	termOut io.Writer
}

func (e *altScreenExecCmd) Run() error {
	// Re-enter alt-screen and clear immediately. BubbleTea's ReleaseTerminal
	// already exited alt-screen; this write covers the primary buffer as fast
	// as possible, before the subprocess starts.
	fmt.Fprint(e.termOut, "\033[?1049h\033[2J\033[H")
	return e.cmd.Run()
}

func (e *altScreenExecCmd) SetStdin(r io.Reader) {
	if e.cmd.Stdin == nil {
		e.cmd.Stdin = r
	}
}

func (e *altScreenExecCmd) SetStdout(w io.Writer) {
	if e.cmd.Stdout == nil {
		e.cmd.Stdout = w
	}
}

func (e *altScreenExecCmd) SetStderr(w io.Writer) {
	if e.cmd.Stderr == nil {
		e.cmd.Stderr = w
	}
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

	fmt.Fprint(m.attachOut, "\033[2J\033[H")

	// Start background bell watcher so custom audio plays and bell badges are
	// tracked while the BubbleTea event loop is suspended.
	watcher := newAttachBellWatcher()
	watcher.start(m.cfg.BellSound, m.cfg.BellVolume, buildSessionTargets(&m.appState))

	// Route the alt-screen enter through m.attachOut so end-to-end tests can
	// intercept the same writer they inject for the synchronous pre-clear above.
	// In production m.attachOut is os.Stdout (set in New()).
	execCmd := &altScreenExecCmd{cmd: cmd, termOut: m.attachOut}
	return tea.Exec(execCmd, func(err error) tea.Msg {
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
