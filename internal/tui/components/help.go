package components

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/tui/styles"
)

const helpNumTabs = 4

// helpPanelBg is the pure-black background used throughout the help panel
// interior. Defined locally so the rest of the app (which uses the slightly
// lighter ColorBg) is unaffected.
var helpPanelBg = lipgloss.Color("#000000")

// helpPanelBgANSI is the ANSI SGR sequence for helpPanelBg (pure black).
// Pre-computed so re-applying the background after resets is allocation-free.
var helpPanelBgANSI = func() string {
	hex := strings.TrimPrefix(string(helpPanelBg), "#")
	if len(hex) != 6 {
		return ""
	}
	r, _ := strconv.ParseUint(hex[0:2], 16, 8)
	g, _ := strconv.ParseUint(hex[2:4], 16, 8)
	b, _ := strconv.ParseUint(hex[4:6], 16, 8)
	return fmt.Sprintf("\033[48;2;%d;%d;%dm", r, g, b)
}()

// fixBgResets re-applies the panel background after every ANSI SGR reset
// in the line so that inner styled spans (which emit \033[0m) don't expose
// the terminal's default background colour inside the help panel.
func fixBgResets(line string) string {
	repl := "\033[0m" + helpPanelBgANSI
	s := helpPanelBgANSI + strings.ReplaceAll(line, "\033[0m", repl)
	// Also handle the shorter \033[m form.
	s = strings.ReplaceAll(s, "\033[m", "\033[m"+helpPanelBgANSI)
	return s
}

// helpTabTitles is the ordered list of tab labels.
var helpTabTitles = [helpNumTabs]string{"Keys", "tmux", "Usage", "Features"}

// HelpPanel renders the full-screen help overlay with a tabbed interface.
// Tab 0 = Keys (keyboard shortcuts), 1 = tmux, 2 = Usage guide, 3 = Features.
type HelpPanel struct {
	// ActiveTab is the currently selected tab index (0–3).
	ActiveTab    int
	// ScrollOffset is the line scroll position for scrollable tabs.
	ScrollOffset int
	// Width and Height are the terminal dimensions (set before each View call).
	Width  int
	Height int

	helpModel help.Model // owned help.Model pre-styled by the caller
}

// NewHelpPanel creates a HelpPanel using the provided pre-styled help.Model.
// All help model styles get an explicit background so the Keys tab renders
// consistently without grey bleed from ANSI resets inside the dark panel.
func NewHelpPanel(h help.Model) HelpPanel {
	bg := helpPanelBg
	h.Styles.FullKey = h.Styles.FullKey.Background(bg)
	h.Styles.FullDesc = h.Styles.FullDesc.Background(bg)
	h.Styles.FullSeparator = h.Styles.FullSeparator.Background(bg)
	h.Styles.ShortKey = h.Styles.ShortKey.Background(bg)
	h.Styles.ShortDesc = h.Styles.ShortDesc.Background(bg)
	h.Styles.ShortSeparator = h.Styles.ShortSeparator.Background(bg)
	h.Styles.Ellipsis = h.Styles.Ellipsis.Background(bg)
	return HelpPanel{helpModel: h}
}

// Open sets the initial tab and resets the scroll position.
func (hp *HelpPanel) Open(tab int) {
	if tab >= 0 && tab < helpNumTabs {
		hp.ActiveTab = tab
	}
	hp.ScrollOffset = 0
}

// Update handles key events inside the help panel.
// Returns true if the key was consumed (caller should not close the panel).
func (hp *HelpPanel) Update(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "left":
		if hp.ActiveTab > 0 {
			hp.ActiveTab--
			hp.ScrollOffset = 0
		}
		return true
	case "right":
		if hp.ActiveTab < helpNumTabs-1 {
			hp.ActiveTab++
			hp.ScrollOffset = 0
		}
		return true
	case "j", "down":
		hp.ScrollOffset++
		return true
	case "k", "up":
		if hp.ScrollOffset > 0 {
			hp.ScrollOffset--
		}
		return true
	}
	return false
}

// View renders the full help overlay.
// km must implement help.KeyMap (our tui.KeyMap satisfies this interface).
func (hp *HelpPanel) View(km help.KeyMap) string {
	w := hp.Width
	h := hp.Height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}

	// Clamp panel width on very wide terminals.
	panelW := w - 8
	if panelW > 120 {
		panelW = 120
	}
	if panelW < 30 {
		panelW = 30
	}

	// --- Header ---
	// Use ColorBg (black) so the entire panel interior is uniformly dark.
	headerStyle := lipgloss.NewStyle().Background(helpPanelBg).Width(panelW).Padding(0, 1)
	header := headerStyle.Render(styles.TitleStyle.Background(helpPanelBg).Render("  Hive Help"))

	// --- Tab strip ---
	tabTop, tabLabels, tabBaseline := hp.renderTabStrip(panelW)
	tabWrap := lipgloss.NewStyle().Background(helpPanelBg).Width(panelW)
	tabStrip := lipgloss.JoinVertical(
		lipgloss.Left,
		tabWrap.Render(tabTop),
		tabWrap.Render(tabLabels),
		tabWrap.Render(tabBaseline),
	)

	// --- Footer ---
	footerHints := strings.Join([]string{
		hint("←/→", "switch tab"),
		hint("↑↓/j/k", "scroll"),
		hint("?/q/esc", "close"),
	}, "  ")
	// Use ColorBg background for footer to match the uniform dark interior.
	footerStyle := lipgloss.NewStyle().
		Background(helpPanelBg).
		Foreground(styles.ColorText).
		Width(panelW).
		Padding(0, 1)
	footer := footerStyle.Render(ansi.Truncate(footerHints, panelW, "…"))

	// --- Content area ---
	// header(1) + tabStrip(3) + footer(1) = 5 fixed rows
	contentH := h - 5 - 6 // extra 6 for outer border + padding
	if contentH < 3 {
		contentH = 3
	}
	innerW := panelW - 4 // 2 chars padding each side

	lines := hp.renderTabContent(km, innerW)

	// Clamp scroll
	maxOff := len(lines) - contentH
	if maxOff < 0 {
		maxOff = 0
	}
	if hp.ScrollOffset > maxOff {
		hp.ScrollOffset = maxOff
	}
	if hp.ScrollOffset < 0 {
		hp.ScrollOffset = 0
	}

	start := hp.ScrollOffset
	end := start + contentH
	if end > len(lines) {
		end = len(lines)
	}
	visible := lines[start:end]
	for len(visible) < contentH {
		visible = append(visible, "")
	}

	// Re-apply the panel background after every ANSI SGR reset so inner
	// styled spans (which emit \033[0m) don't expose the terminal default.
	fixed := make([]string, len(visible))
	for i, l := range visible {
		fixed[i] = fixBgResets(l)
	}

	body := lipgloss.NewStyle().
		Background(helpPanelBg).
		Width(panelW).
		Height(contentH).
		Padding(0, 2).
		Render(strings.Join(fixed, "\n"))

	panel := lipgloss.NewStyle().
		Background(helpPanelBg).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		BorderBackground(helpPanelBg).
		Render(lipgloss.JoinVertical(lipgloss.Left, header, tabStrip, body, footer))

	return lipgloss.Place(w, h,
		lipgloss.Center, lipgloss.Center,
		panel,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#111827")),
	)
}

// renderTabContent returns the lines for the active tab content.
func (hp *HelpPanel) renderTabContent(km help.KeyMap, innerW int) []string {
	var raw string
	switch hp.ActiveTab {
	case 0:
		hp.helpModel.ShowAll = true
		hp.helpModel.Width = innerW
		raw = hp.helpModel.View(km)
	case 1:
		raw = hp.renderTmuxTab(innerW)
	case 2:
		raw = hp.renderUsageTab(innerW)
	case 3:
		raw = hp.renderFeaturesTab(innerW)
	}
	return strings.Split(raw, "\n")
}

// renderTmuxTab renders the tmux key bindings reference.
func (hp *HelpPanel) renderTmuxTab(w int) string {
	bg := helpPanelBg
	keyStyle := styles.HelpKeyStyle.Background(bg).Width(18)
	descStyle := styles.HelpDescStyle.Background(bg)
	headerStyle := styles.MutedStyle.Background(bg)

	type binding struct{ key, desc string }
	bindings := []binding{
		{mux.DetachKey(), "detach from session (return to Hive)"},
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
	rows = append(rows, headerStyle.Render("Key bindings while inside an attached tmux session.")+"\n")
	for _, b := range bindings {
		row := keyStyle.Render(b.key) + "  " + descStyle.Render(b.desc)
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

// renderUsageTab renders the usage guide.
func (hp *HelpPanel) renderUsageTab(_ int) string {
	bg := helpPanelBg
	secStyle := styles.TitleStyle.Background(bg)
	keyStyle := styles.HelpKeyStyle.Background(bg)
	mutedStyle := styles.MutedStyle.Background(bg)
	sec := secStyle.Render
	muted := mutedStyle.Render
	key := keyStyle.Render

	lines := []string{
		sec("Views"),
		"",
		"Hive has two main layouts:",
		"",
		"  " + key("tab") + "    " + muted("Sidebar view") + " — tree of projects and sessions with a live preview pane.",
		"  " + key("g") + "     " + muted("Grid view") + "    — compact cells showing all sessions in the current project.",
		"  " + key("G") + "     " + muted("All-sessions grid") + " — every session across all projects.",
		"",
		sec("Navigation"),
		"",
		"  " + key("↑ / k") + "        move up",
		"  " + key("↓ / j") + "        move down",
		"  " + key("[ / ]") + "        jump to previous / next project",
		"  " + key("space") + "         toggle project collapse",
		"  " + key("←  / →") + "        collapse / expand project",
		"  " + key("tab") + "          switch between sidebar and preview pane",
		"",
		sec("Projects & Sessions"),
		"",
		"  " + key("p") + "    new project (choose directory)",
		"  " + key("n") + "    new session in the current project",
		"  " + key("W") + "    new worktree session (new git branch + directory)",
		"  " + key("r") + "    rename project or session",
		"  " + key("x") + "    kill session",
		"",
		sec("Attaching"),
		"",
		"Press " + key("enter") + " or " + key("a") + " to attach to a session.",
		"",
		"While attached, use the detach key to return to Hive:",
		"  " + muted("Default: ") + key(mux.DetachKey()) + "  (configurable in Settings)",
		"",
		"Session status indicators:",
		"  " + styles.HelpKeyStyle.Render("●") + "  running      " + styles.HelpKeyStyle.Render("◉") + "  waiting for input",
		"  " + styles.HelpKeyStyle.Render("✕") + "  dead         " + styles.HelpKeyStyle.Render("○") + "  idle",
		"",
		sec("Filtering"),
		"",
		"Press " + key("/") + " to open the filter bar. Type to narrow sessions by name.",
		"Press " + key("esc") + " to clear the filter and return to normal navigation.",
		"",
		sec("Grid Shortcuts"),
		"",
		"  " + key("enter / a") + "         attach to selected session",
		"  " + key("Shift+← / →") + "    reorder session within project",
		"  " + key("v / V") + "             cycle session color badge",
		"  " + key("c / C") + "             cycle project color",
		"  " + key("esc / g") + "           return to sidebar",
	}
	return strings.Join(lines, "\n")
}

// renderFeaturesTab renders the features reference.
func (hp *HelpPanel) renderFeaturesTab(_ int) string {
	bg := helpPanelBg
	secStyle := styles.TitleStyle.Background(bg)
	keyStyle := styles.HelpKeyStyle.Background(bg)
	mutedStyle := styles.MutedStyle.Background(bg)
	sec := secStyle.Render
	muted := mutedStyle.Render
	key := keyStyle.Render

	lines := []string{
		sec("Agent Types"),
		"",
		"Hive works with multiple AI agents:",
		"  " + key("claude") + "    Anthropic's Claude CLI",
		"  " + key("codex") + "     OpenAI Codex CLI",
		"  " + key("gemini") + "    Google Gemini CLI",
		"  " + key("copilot") + "   GitHub Copilot CLI",
		"  " + key("cmd") + "       any terminal command",
		"",
		"Press " + key("n") + " and select an agent type when creating a new session.",
		"",
		sec("Session Colors"),
		"",
		"Each session can have a color for easy identification.",
		"Colors appear as a gradient title bar in the sidebar and as a",
		"colored badge in grid cells.",
		"",
		"  " + key("v / V") + "    cycle session color (sidebar or grid)",
		"",
		sec("Project Colors"),
		"",
		"  " + key("c / C") + "    cycle project color (visible in grid header)",
		"",
		sec("Terminal Bell"),
		"",
		"When a session outputs a terminal bell, Hive:",
		"  • plays a sound (configurable in Settings)",
		"  • shows a " + muted("🔔") + " badge on the session",
		"",
		"The badge clears when you attach to the session.",
		"Configure sound file and volume in " + key("Settings") + " → Audio.",
		"",
		sec("Worktree Sessions"),
		"",
		"Press " + key("W") + " to create a session in a new git worktree.",
		"Each worktree session gets its own branch and working directory,",
		"letting multiple agents work on different tasks in parallel.",
		"The branch name is shown next to the session title (⎇ branch).",
		"",
		sec("Session Reordering"),
		"",
		"In " + muted("grid view") + ": press " + key("Shift+←/→") + " to move a session left or right.",
		"In " + muted("sidebar") + ": press " + key("Shift+↑/↓") + " (or configured move keys) to reorder.",
		"",
		sec("Settings"),
		"",
		"Press " + key("s") + " to open the Settings panel.",
		"  " + key("←/→") + "           switch between tabs (General, Audio, Keybindings…)",
		"  " + key("↑/↓ / j/k") + "   navigate fields",
		"  " + key("enter/space") + "  edit or toggle a setting",
		"  " + key("s") + "             save changes",
		"  " + key("esc") + "           discard and close",
		"",
		sec("Keybindings"),
		"",
		"All keybindings are configurable.",
		"Open " + key("Settings") + " → Keybindings to edit them interactively,",
		"or edit " + muted("~/.config/hive/config.toml") + " directly under [keybindings].",
		"Changes take effect after saving and restarting Hive.",
		"",
		sec("What's New"),
		"",
		"On startup, Hive shows a changelog overlay when a new version is",
		"detected. Dismiss with " + key("enter") + " or " + key("esc") + ", or press " + key("d") + " to suppress",
		"future notices for this version.",
	}
	return strings.Join(lines, "\n")
}

// renderTabStrip returns the three rows of the tab strip (top, labels, baseline).
// Adapted from SettingsView.renderTabStrip.
func (hp *HelpPanel) renderTabStrip(w int) (string, string, string) {
	const leftGutter = 2
	sepNextToActive := 1
	sepBetweenInactive := 3

	titles := helpTabTitles[:]

	cellW := func(i int, label string) int {
		L := ansi.StringWidth(label)
		if i == hp.ActiveTab {
			return L + 4
		}
		return L
	}
	sepW := func(i int) int {
		if i == hp.ActiveTab || i+1 == hp.ActiveTab {
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

	// Help panel uses ColorBg (black) for the entire interior — no grey tint.
	bgStyle := lipgloss.NewStyle().Background(helpPanelBg)
	baselineStyle := lipgloss.NewStyle().Foreground(styles.ColorBorder).Background(helpPanelBg)
	notchStyle := lipgloss.NewStyle().Foreground(styles.ColorAccent).Background(helpPanelBg)
	frameOnCapsule := lipgloss.NewStyle().Foreground(styles.ColorAccent).Background(helpPanelBg)
	frameOnBody := lipgloss.NewStyle().Foreground(styles.ColorAccent).Background(helpPanelBg)
	activeInterior := lipgloss.NewStyle().Background(helpPanelBg)
	activeLabel := lipgloss.NewStyle().Foreground(styles.ColorText).Background(helpPanelBg).Bold(true)
	inactiveLabel := lipgloss.NewStyle().Foreground(styles.ColorSubtext).Background(helpPanelBg)

	var top, mid, base strings.Builder

	spaces := func(n int) string { return strings.Repeat(" ", n) }
	dashes := func(n int) string { return strings.Repeat("─", n) }

	top.WriteString(bgStyle.Render(spaces(leftGutter)))
	mid.WriteString(bgStyle.Render(spaces(leftGutter)))
	base.WriteString(baselineStyle.Render(dashes(leftGutter)))

	used := leftGutter
	for i, l := range labels {
		L := ansi.StringWidth(l)
		if i == hp.ActiveTab {
			top.WriteString(frameOnBody.Render("╭" + dashes(L+2) + "╮"))
			mid.WriteString(frameOnCapsule.Render("│"))
			mid.WriteString(activeInterior.Render(" "))
			mid.WriteString(activeLabel.Render(l))
			mid.WriteString(activeInterior.Render(" "))
			mid.WriteString(frameOnCapsule.Render("│"))
			base.WriteString(notchStyle.Render("╯"))
			base.WriteString(bgStyle.Render(spaces(L + 2)))
			base.WriteString(notchStyle.Render("╰"))
			used += L + 4
		} else {
			top.WriteString(bgStyle.Render(spaces(L)))
			mid.WriteString(inactiveLabel.Render(l))
			base.WriteString(baselineStyle.Render(dashes(L)))
			used += L
		}

		if i < len(labels)-1 {
			sw := sepW(i)
			top.WriteString(bgStyle.Render(spaces(sw)))
			mid.WriteString(bgStyle.Render(spaces(sw)))
			base.WriteString(baselineStyle.Render(dashes(sw)))
			used += sw
		}
	}

	if used < w {
		remaining := w - used
		top.WriteString(bgStyle.Render(spaces(remaining)))
		mid.WriteString(bgStyle.Render(spaces(remaining)))
		base.WriteString(baselineStyle.Render(dashes(remaining)))
	}

	topRow := top.String()
	midRow := mid.String()
	baseRow := base.String()
	if ansi.StringWidth(topRow) > w {
		topRow = ansi.Truncate(topRow, w, "")
	}
	if ansi.StringWidth(midRow) > w {
		midRow = ansi.Truncate(midRow, w, "")
	}
	if ansi.StringWidth(baseRow) > w {
		baseRow = ansi.Truncate(baseRow, w, "")
	}
	return topRow, midRow, baseRow
}
