package components

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// SettingsSaveRequestMsg is sent when the user confirms they want to save settings.
type SettingsSaveRequestMsg struct {
	Config config.Config
}

// SettingsClosedMsg is sent when the settings screen is closed.
type SettingsClosedMsg struct{}

type fieldKind int

const (
	fieldBool   fieldKind = iota
	fieldString fieldKind = iota
	fieldInt    fieldKind = iota
	fieldSelect fieldKind = iota
)

// settingField describes a single editable setting.
type settingField struct {
	label       string
	description string
	kind        fieldKind
	options     []string // for fieldSelect
	get         func(config.Config) string
	set         func(*config.Config, string) error
}

// settingTab groups a set of fields under a single tab title.
type settingTab struct {
	title  string
	fields []*settingField
}

// SettingsView is a full-screen interactive settings overlay.
type SettingsView struct {
	Active bool
	Width  int
	Height int

	cfg      config.Config
	original config.Config

	tabs             []settingTab
	activeTab        int
	tabCursors       []int // per-tab cursor into tabs[i].fields
	tabScrollOffsets []int // per-tab top-line scroll offset
	dirty            bool

	editing        bool
	editInput      textinput.Model
	editErr        string
	pendingDiscard bool
	pendingSave    bool
}

// NewSettingsView creates a SettingsView.
func NewSettingsView() SettingsView {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 46
	return SettingsView{editInput: ti}
}

// Open activates the settings view with a copy of the given config.
func (sv *SettingsView) Open(cfg config.Config) {
	sv.Active = true
	sv.cfg = cfg
	sv.original = cfg
	sv.dirty = false
	sv.editing = false
	sv.editErr = ""
	sv.pendingDiscard = false
	sv.pendingSave = false
	sv.tabs = buildSettingTabs()
	sv.activeTab = 0
	sv.tabCursors = make([]int, len(sv.tabs))
	sv.tabScrollOffsets = make([]int, len(sv.tabs))
}

// Close deactivates the settings view.
func (sv *SettingsView) Close() {
	sv.Active = false
	sv.editing = false
	sv.editInput.Blur()
	sv.pendingDiscard = false
	sv.pendingSave = false
	sv.dirty = false
}

// IsDirty returns true if any setting has been changed from the original.
func (sv *SettingsView) IsDirty() bool { return sv.dirty }

// GetConfig returns the current (possibly modified) working config.
func (sv *SettingsView) GetConfig() config.Config { return sv.cfg }

// ActiveTab returns the index of the currently selected tab.
func (sv *SettingsView) ActiveTab() int { return sv.activeTab }

// TabCursor returns the cursor position within the given tab.
func (sv *SettingsView) TabCursor(tab int) int {
	if tab < 0 || tab >= len(sv.tabCursors) {
		return 0
	}
	return sv.tabCursors[tab]
}

// IsEditing reports whether a field is currently being edited.
func (sv *SettingsView) IsEditing() bool { return sv.editing }

// IsPendingSave reports whether the save-confirmation prompt is active.
func (sv *SettingsView) IsPendingSave() bool { return sv.pendingSave }

func (sv *SettingsView) currentFields() []*settingField {
	if len(sv.tabs) == 0 {
		return nil
	}
	return sv.tabs[sv.activeTab].fields
}

func (sv *SettingsView) cursor() int {
	if len(sv.tabCursors) == 0 {
		return 0
	}
	return sv.tabCursors[sv.activeTab]
}

func (sv *SettingsView) setCursor(n int) {
	if len(sv.tabCursors) == 0 {
		return
	}
	sv.tabCursors[sv.activeTab] = n
}

func (sv *SettingsView) scrollOffset() int {
	if len(sv.tabScrollOffsets) == 0 {
		return 0
	}
	return sv.tabScrollOffsets[sv.activeTab]
}

func (sv *SettingsView) setScrollOffset(n int) {
	if len(sv.tabScrollOffsets) == 0 {
		return
	}
	sv.tabScrollOffsets[sv.activeTab] = n
}

func (sv *SettingsView) selectedField() *settingField {
	fields := sv.currentFields()
	if len(fields) == 0 {
		return nil
	}
	c := sv.cursor()
	if c >= len(fields) {
		c = len(fields) - 1
		sv.setCursor(c)
	}
	if c < 0 {
		return nil
	}
	return fields[c]
}

// Update handles key messages. Returns (cmd, consumed).
func (sv *SettingsView) Update(msg tea.KeyMsg) (tea.Cmd, bool) {
	if !sv.Active {
		return nil, false
	}

	if sv.editing {
		return sv.handleEditKey(msg)
	}

	// Handle pending save confirmation inline (no app-level overlay needed).
	if sv.pendingSave {
		switch msg.String() {
		case "y", "enter":
			sv.pendingSave = false
			cfg := sv.cfg
			sv.Close()
			return func() tea.Msg { return SettingsSaveRequestMsg{Config: cfg} }, true
		default:
			sv.pendingSave = false
		}
		return nil, true
	}

	// Any key other than esc clears the pending-discard warning.
	if msg.String() != "esc" {
		sv.pendingDiscard = false
	}

	switch msg.String() {
	case "esc":
		if sv.IsDirty() {
			if sv.pendingDiscard {
				sv.Close()
				return func() tea.Msg { return SettingsClosedMsg{} }, true
			}
			sv.pendingDiscard = true
			return nil, true
		}
		sv.Close()
		return func() tea.Msg { return SettingsClosedMsg{} }, true

	case "s":
		if !sv.IsDirty() {
			sv.Close()
			return func() tea.Msg { return SettingsClosedMsg{} }, true
		}
		sv.pendingSave = true
		return nil, true

	case "left", "h":
		if sv.activeTab > 0 {
			sv.activeTab--
		}

	case "right", "l":
		if sv.activeTab < len(sv.tabs)-1 {
			sv.activeTab++
		}

	case "j", "down":
		if sv.cursor() < len(sv.currentFields())-1 {
			sv.setCursor(sv.cursor() + 1)
		}

	case "k", "up":
		if sv.cursor() > 0 {
			sv.setCursor(sv.cursor() - 1)
		}

	case "enter", " ":
		f := sv.selectedField()
		if f == nil {
			break
		}
		switch f.kind {
		case fieldBool:
			_ = f.set(&sv.cfg, strconv.FormatBool(f.get(sv.cfg) != "true"))
			sv.dirty = true
		case fieldSelect:
			cur := f.get(sv.cfg)
			matched := false
			for i, opt := range f.options {
				if opt == cur {
					_ = f.set(&sv.cfg, f.options[(i+1)%len(f.options)])
					matched = true
					break
				}
			}
			if !matched && len(f.options) > 0 {
				_ = f.set(&sv.cfg, f.options[0])
			}
			sv.dirty = true
		case fieldString, fieldInt:
			sv.startEditing(f)
		}
	}

	return nil, true
}

func (sv *SettingsView) handleEditKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "enter":
		val := sv.editInput.Value()
		f := sv.selectedField()
		if f != nil {
			if err := f.set(&sv.cfg, val); err != nil {
				sv.editErr = err.Error()
				return nil, true
			}
			sv.dirty = true
		}
		sv.editing = false
		sv.editInput.Blur()
		sv.editErr = ""
		return nil, true
	case "esc":
		sv.editing = false
		sv.editInput.Blur()
		sv.editErr = ""
		return nil, true
	}
	var cmd tea.Cmd
	sv.editInput, cmd = sv.editInput.Update(msg)
	return cmd, true
}

func (sv *SettingsView) startEditing(f *settingField) {
	sv.editing = true
	sv.editErr = ""
	sv.editInput.SetValue(f.get(sv.cfg))
	sv.editInput.CursorEnd()
	sv.editInput.Focus()
}

// View renders the full-screen settings panel.
func (sv *SettingsView) View() string {
	if !sv.Active {
		return ""
	}

	w := sv.Width
	h := sv.Height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	innerW := w - 4

	// Header bar — truncate to avoid wrapping on narrow terminals.
	configPath := styles.MutedStyle.Render(config.ConfigPath())
	rawHeader := styles.TitleStyle.Render("⚙  Settings") + "  " + configPath
	header := lipgloss.NewStyle().
		Background(lipgloss.Color("#1F2937")).
		Width(w).
		Padding(0, 1).
		Render(ansi.Truncate(rawHeader, w-2, "…"))

	// Tab strip sits directly under the header.
	tabStrip := lipgloss.NewStyle().
		Background(styles.ColorBg).
		Width(w).
		Padding(0, 2).
		Render(sv.renderTabStrip(innerW))

	// Footer hints — build content then truncate before styling.
	var footerParts []string
	if sv.pendingSave {
		footerParts = []string{
			lipgloss.NewStyle().Foreground(styles.ColorWarning).Bold(true).Render("Save to " + config.ConfigPath() + "?"),
			styles.HelpKeyStyle.Render("y/enter") + ":" + styles.HelpDescStyle.Render("confirm"),
			styles.HelpKeyStyle.Render("any other key") + ":" + styles.HelpDescStyle.Render("cancel"),
		}
	} else if sv.pendingDiscard {
		footerParts = []string{
			styles.ErrorStyle.Render("Unsaved changes! Press"),
			styles.HelpKeyStyle.Render("esc"),
			styles.ErrorStyle.Render("again to discard, or"),
			styles.HelpKeyStyle.Render("s"),
			styles.ErrorStyle.Render("to save"),
		}
	} else {
		if sv.IsDirty() {
			footerParts = append(footerParts,
				lipgloss.NewStyle().Foreground(styles.ColorWarning).Render("● unsaved  "),
			)
		}
		footerParts = append(footerParts,
			hint("←/→", "tab"),
			hint("j/k", "navigate"),
			hint("enter/space", "edit/toggle"),
			hint("s", func() string {
				if sv.IsDirty() {
					return "save"
				}
				return "close"
			}()),
			hint("esc", func() string {
				if sv.IsDirty() {
					return "discard (×2)"
				}
				return "close"
			}()),
		)
	}
	footer := styles.StatusBarStyle.Width(w).Render(ansi.Truncate(strings.Join(footerParts, "  "), w, "…"))

	// Content area height (header=1 + tab strip=1 + footer=1)
	contentH := h - 3
	if contentH < 1 {
		contentH = 1
	}

	// Render active-tab lines and track anchor for scrolling.
	anchorLine := 0
	lines := sv.renderLines(innerW, &anchorLine)

	// Scroll so the selected entry is always visible.
	// The selected block can be up to ~5 lines tall; keep it fully visible.
	const selectedBlockMaxH = 5
	off := sv.scrollOffset()
	if anchorLine < off {
		off = anchorLine
	}
	if anchorLine+selectedBlockMaxH > off+contentH {
		off = anchorLine + selectedBlockMaxH - contentH
	}
	if off < 0 {
		off = 0
	}
	maxOff := len(lines) - contentH
	if maxOff < 0 {
		maxOff = 0
	}
	if off > maxOff {
		off = maxOff
	}
	sv.setScrollOffset(off)

	// Window into lines.
	start := off
	end := start + contentH
	if end > len(lines) {
		end = len(lines)
	}
	visible := lines[start:end]
	for len(visible) < contentH {
		visible = append(visible, strings.Repeat(" ", innerW))
	}

	body := lipgloss.NewStyle().
		Background(styles.ColorBg).
		Width(w).
		Height(contentH).
		Padding(0, 2).
		Render(strings.Join(visible, "\n"))

	return lipgloss.JoinVertical(lipgloss.Left, header, tabStrip, body, footer)
}

// renderTabStrip renders the horizontal tab bar, degrading gracefully
// on narrow terminals (full labels → truncated → first-letter).
func (sv *SettingsView) renderTabStrip(innerW int) string {
	if len(sv.tabs) == 0 || innerW <= 0 {
		return ""
	}
	titles := make([]string, len(sv.tabs))
	for i, t := range sv.tabs {
		titles[i] = strings.TrimSpace(t.title)
	}

	const sep = "  "
	fits := func(labels []string) bool {
		total := 0
		for i, l := range labels {
			total += ansi.StringWidth(" " + l + " ")
			if i < len(labels)-1 {
				total += ansi.StringWidth(sep)
			}
		}
		return total <= innerW
	}

	labels := titles
	if !fits(labels) {
		// Truncate each label evenly.
		for w := 10; w >= 2 && !fits(labels); w-- {
			labels = make([]string, len(titles))
			for i, t := range titles {
				if ansi.StringWidth(t) > w {
					labels[i] = ansi.Truncate(t, w, "…")
				} else {
					labels[i] = t
				}
			}
		}
	}
	if !fits(labels) {
		// Fall back to first letter of each title.
		labels = make([]string, len(titles))
		for i, t := range titles {
			if t == "" {
				labels[i] = "?"
			} else {
				runes := []rune(t)
				labels[i] = string(runes[0])
			}
		}
	}

	active := lipgloss.NewStyle().
		Foreground(styles.ColorBg).
		Background(styles.ColorAccent).
		Bold(true).
		Padding(0, 1)
	inactive := lipgloss.NewStyle().
		Foreground(styles.ColorMuted).
		Padding(0, 1)

	var parts []string
	for i, l := range labels {
		if i == sv.activeTab {
			parts = append(parts, active.Render(l))
		} else {
			parts = append(parts, inactive.Render(l))
		}
	}
	row := strings.Join(parts, sep)
	if ansi.StringWidth(row) > innerW {
		row = ansi.Truncate(row, innerW, "…")
	}
	return row
}

// renderLines produces a flat []string of display lines for the active tab
// and sets *anchorLine to the line index where the selected field begins.
func (sv *SettingsView) renderLines(innerW int, anchorLine *int) []string {
	var lines []string
	fields := sv.currentFields()
	sel := sv.cursor()

	for i, f := range fields {
		isSelected := i == sel
		if isSelected {
			*anchorLine = len(lines)
		}

		val := f.get(sv.cfg)
		valDisplay := renderFieldValue(f, val)

		prefix := "  "
		if isSelected {
			prefix = styles.HelpKeyStyle.Render("▶ ")
		}
		label := prefix + f.label

		valW := ansi.StringWidth(valDisplay)
		if valW > innerW-4 {
			valW = innerW - 4
			valDisplay = ansi.Truncate(valDisplay, valW, "…")
		}
		labelW := innerW - valW - 1
		if labelW < 0 {
			labelW = 0
		}

		row := lipgloss.JoinHorizontal(
			lipgloss.Left,
			lipgloss.NewStyle().Width(labelW).Render(label),
			valDisplay,
		)
		if isSelected {
			row = lipgloss.NewStyle().Background(styles.ColorSelected).Width(innerW).Render(row)
		}
		lines = append(lines, row)

		if isSelected {
			if sv.editing {
				lines = append(lines, "    "+sv.editInput.View())
				if sv.editErr != "" {
					lines = append(lines, "    "+styles.ErrorStyle.Render("⚠ "+sv.editErr))
				}
			} else {
				if f.description != "" {
					for _, dline := range wrapText(f.description, innerW-4) {
						lines = append(lines, styles.MutedStyle.Render("    "+dline))
					}
				}
				switch f.kind {
				case fieldSelect:
					optsStr := strings.Join(f.options, " · ")
					for _, oline := range wrapText("Options: "+optsStr, innerW-4) {
						lines = append(lines, styles.MutedStyle.Render("    "+oline))
					}
				case fieldBool:
					lines = append(lines, styles.MutedStyle.Render("    [enter/space] to toggle"))
				case fieldString, fieldInt:
					lines = append(lines, styles.MutedStyle.Render("    [enter] to edit"))
				}
			}
		}

		lines = append(lines, "") // spacer between fields
	}

	return lines
}

func renderFieldValue(f *settingField, val string) string {
	switch f.kind {
	case fieldBool:
		if val == "true" {
			return lipgloss.NewStyle().Foreground(styles.ColorSuccess).Render("[on]")
		}
		return lipgloss.NewStyle().Foreground(styles.ColorMuted).Render("[off]")
	default:
		return lipgloss.NewStyle().Foreground(styles.ColorAccent).Render("[" + val + "]")
	}
}

func hint(key, desc string) string {
	return styles.HelpKeyStyle.Render(key) + ":" + styles.HelpDescStyle.Render(desc)
}

// wrapText wraps s to lines of at most maxW characters.
func wrapText(s string, maxW int) []string {
	if maxW <= 0 {
		return []string{s}
	}
	words := strings.Fields(s)
	var lines []string
	cur := ""
	for _, w := range words {
		if cur == "" {
			cur = w
		} else if len(cur)+1+len(w) <= maxW {
			cur += " " + w
		} else {
			lines = append(lines, cur)
			cur = w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func buildSettingTabs() []settingTab {
	return []settingTab{
		{
			title: "General",
			fields: []*settingField{
				{
					label:       "Theme",
					description: "Color scheme for the UI.",
					kind:        fieldSelect,
					options:     []string{"dark", "light"},
					get:         func(c config.Config) string { return c.Theme },
					set: func(c *config.Config, v string) error {
						c.Theme = v
						return nil
					},
				},
				{
					label:       "Multiplexer",
					description: "Backend for managing terminal sessions. 'tmux' uses the external tmux binary. 'native' uses a built-in PTY daemon (no external binary needed).",
					kind:        fieldSelect,
					options:     []string{"tmux", "native"},
					get:         func(c config.Config) string { return c.Multiplexer },
					set: func(c *config.Config, v string) error {
						c.Multiplexer = v
						return nil
					},
				},
				{
					label:       "Preview Refresh (ms)",
					description: "How often the session preview pane refreshes, in milliseconds. Lower values feel more responsive but use more CPU.",
					kind:        fieldInt,
					get:         func(c config.Config) string { return strconv.Itoa(c.PreviewRefreshMs) },
					set: func(c *config.Config, v string) error {
						n, err := strconv.Atoi(strings.TrimSpace(v))
						if err != nil || n < 50 || n > 30000 {
							return fmt.Errorf("must be a number between 50 and 30000")
						}
						c.PreviewRefreshMs = n
						return nil
					},
				},
				{
					label:       "Agent Title Overrides User Title",
					description: "When enabled, agent-detected session titles overwrite titles you set manually.",
					kind:        fieldBool,
					get: func(c config.Config) string {
						return strconv.FormatBool(c.AgentTitleOverridesUserTitle)
					},
					set: func(c *config.Config, v string) error {
						b, err := strconv.ParseBool(v)
						if err != nil {
							return fmt.Errorf("must be true or false")
						}
						c.AgentTitleOverridesUserTitle = b
						return nil
					},
				},
				{
					label:       "Hide Attach Hint",
					description: "When enabled, skip the informational dialog shown before attaching to a session.",
					kind:        fieldBool,
					get:         func(c config.Config) string { return strconv.FormatBool(c.HideAttachHint) },
					set: func(c *config.Config, v string) error {
						b, err := strconv.ParseBool(v)
						if err != nil {
							return fmt.Errorf("must be true or false")
						}
						c.HideAttachHint = b
						return nil
					},
				},
				{
					label:       "Hide What's New",
					description: "When enabled, skip the changelog dialog shown after updates. Disable to see it again on the next version change.",
					kind:        fieldBool,
					get:         func(c config.Config) string { return strconv.FormatBool(c.HideWhatsNew) },
					set: func(c *config.Config, v string) error {
						b, err := strconv.ParseBool(v)
						if err != nil {
							return fmt.Errorf("must be true or false")
						}
						c.HideWhatsNew = b
						return nil
					},
				},
			},
		},
		{
			title: "Team Defaults",
			fields: []*settingField{
				{
					label:       "Orchestrator Agent",
					description: "Default agent type for the orchestrator role when creating a new team.",
					kind:        fieldSelect,
					options:     []string{"claude", "codex", "gemini", "copilot", "aider", "opencode"},
					get:         func(c config.Config) string { return c.TeamDefaults.Orchestrator },
					set: func(c *config.Config, v string) error {
						c.TeamDefaults.Orchestrator = v
						return nil
					},
				},
				{
					label:       "Default Worker Count",
					description: "Number of worker sessions created when spawning a new team.",
					kind:        fieldInt,
					get:         func(c config.Config) string { return strconv.Itoa(c.TeamDefaults.WorkerCount) },
					set: func(c *config.Config, v string) error {
						n, err := strconv.Atoi(strings.TrimSpace(v))
						if err != nil || n < 1 || n > 20 {
							return fmt.Errorf("must be a number between 1 and 20")
						}
						c.TeamDefaults.WorkerCount = n
						return nil
					},
				},
				{
					label:       "Default Worker Agent",
					description: "Default agent type for worker sessions in a new team.",
					kind:        fieldSelect,
					options:     []string{"claude", "codex", "gemini", "copilot", "aider", "opencode"},
					get:         func(c config.Config) string { return c.TeamDefaults.WorkerAgent },
					set: func(c *config.Config, v string) error {
						c.TeamDefaults.WorkerAgent = v
						return nil
					},
				},
			},
		},
		{
			title: "Hooks",
			fields: []*settingField{
				{
					label:       "Hooks Enabled",
					description: "When enabled, shell scripts in the hooks directory are executed on lifecycle events (session create, kill, attach, etc.).",
					kind:        fieldBool,
					get:         func(c config.Config) string { return strconv.FormatBool(c.Hooks.Enabled) },
					set: func(c *config.Config, v string) error {
						b, err := strconv.ParseBool(v)
						if err != nil {
							return fmt.Errorf("must be true or false")
						}
						c.Hooks.Enabled = b
						return nil
					},
				},
				{
					label:       "Hooks Directory",
					description: "Directory containing hook scripts. Supports ~ for home directory.",
					kind:        fieldString,
					get:         func(c config.Config) string { return c.Hooks.Dir },
					set: func(c *config.Config, v string) error {
						v = strings.TrimSpace(v)
						if v == "" {
							return fmt.Errorf("directory cannot be empty")
						}
						c.Hooks.Dir = v
						return nil
					},
				},
			},
		},
		{
			title: "Keybindings",
			fields: []*settingField{
				keybindField("Toggle Collapse", "Collapse or expand the selected project in the sidebar.", func(c config.Config) string { return c.Keybindings.ToggleCollapse }, func(c *config.Config, v string) { c.Keybindings.ToggleCollapse = v }),
				keybindField("Focus Preview", "Move focus to the preview pane.", func(c config.Config) string { return c.Keybindings.FocusPreview }, func(c *config.Config, v string) { c.Keybindings.FocusPreview = v }),
				keybindField("Focus Sidebar", "Move focus back to the sidebar.", func(c config.Config) string { return c.Keybindings.FocusSidebar }, func(c *config.Config, v string) { c.Keybindings.FocusSidebar = v }),
				keybindField("Jump to Project 1", "Jump directly to the first project (repeatable pattern for 2–9).", func(c config.Config) string { return c.Keybindings.JumpProject1 }, func(c *config.Config, v string) { c.Keybindings.JumpProject1 = v }),
				keybindField("New Project", "Create a new project.", func(c config.Config) string { return c.Keybindings.NewProject }, func(c *config.Config, v string) { c.Keybindings.NewProject = v }),
				keybindField("New Session", "Open the agent picker to start a new session.", func(c config.Config) string { return c.Keybindings.NewSession }, func(c *config.Config, v string) { c.Keybindings.NewSession = v }),
				keybindField("New Team", "Open the team builder wizard.", func(c config.Config) string { return c.Keybindings.NewTeam }, func(c *config.Config, v string) { c.Keybindings.NewTeam = v }),
				keybindField("New Worktree Session", "Create a session in a new git worktree.", func(c config.Config) string { return c.Keybindings.NewWorktreeSession }, func(c *config.Config, v string) { c.Keybindings.NewWorktreeSession = v }),
				keybindField("Attach to Session", "Attach to the selected session.", func(c config.Config) string { return c.Keybindings.Attach }, func(c *config.Config, v string) { c.Keybindings.Attach = v }),
				keybindField("Kill Session", "Kill the selected session or project.", func(c config.Config) string { return c.Keybindings.KillSession }, func(c *config.Config, v string) { c.Keybindings.KillSession = v }),
				keybindField("Kill Team", "Kill the selected team and all its sessions.", func(c config.Config) string { return c.Keybindings.KillTeam }, func(c *config.Config, v string) { c.Keybindings.KillTeam = v }),
				keybindField("Rename", "Rename the selected session or team.", func(c config.Config) string { return c.Keybindings.Rename }, func(c *config.Config, v string) { c.Keybindings.Rename = v }),
				keybindField("Navigate Up", "Move cursor up in the sidebar.", func(c config.Config) string { return c.Keybindings.NavUp }, func(c *config.Config, v string) { c.Keybindings.NavUp = v }),
				keybindField("Navigate Down", "Move cursor down in the sidebar.", func(c config.Config) string { return c.Keybindings.NavDown }, func(c *config.Config, v string) { c.Keybindings.NavDown = v }),
				keybindField("Previous Project", "Jump to the previous project in the sidebar.", func(c config.Config) string { return c.Keybindings.NavProjectUp }, func(c *config.Config, v string) { c.Keybindings.NavProjectUp = v }),
				keybindField("Next Project", "Jump to the next project in the sidebar.", func(c config.Config) string { return c.Keybindings.NavProjectDown }, func(c *config.Config, v string) { c.Keybindings.NavProjectDown = v }),
				keybindField("Filter", "Open the session filter.", func(c config.Config) string { return c.Keybindings.Filter }, func(c *config.Config, v string) { c.Keybindings.Filter = v }),
				keybindField("Grid Overview", "Switch to grid view (current project).", func(c config.Config) string { return c.Keybindings.GridOverview }, func(c *config.Config, v string) { c.Keybindings.GridOverview = v }),
				keybindField("Command Palette", "Open the command palette.", func(c config.Config) string { return c.Keybindings.Palette }, func(c *config.Config, v string) { c.Keybindings.Palette = v }),
				keybindField("Help", "Toggle the keyboard shortcuts overlay.", func(c config.Config) string { return c.Keybindings.Help }, func(c *config.Config, v string) { c.Keybindings.Help = v }),
				keybindField("Tmux Help", "Toggle the tmux shortcuts reference.", func(c config.Config) string { return c.Keybindings.TmuxHelp }, func(c *config.Config, v string) { c.Keybindings.TmuxHelp = v }),
				keybindField("Quit", "Quit hive (sessions keep running in tmux).", func(c config.Config) string { return c.Keybindings.Quit }, func(c *config.Config, v string) { c.Keybindings.Quit = v }),
				keybindField("Quit and Kill All", "Quit and terminate all managed sessions.", func(c config.Config) string { return c.Keybindings.QuitKill }, func(c *config.Config, v string) { c.Keybindings.QuitKill = v }),
				keybindField("Open Settings", "Open this settings screen.", func(c config.Config) string { return c.Keybindings.Settings }, func(c *config.Config, v string) { c.Keybindings.Settings = v }),
				keybindField("Next Color", "Cycle the selected project to the next color.", func(c config.Config) string { return c.Keybindings.ColorNext }, func(c *config.Config, v string) { c.Keybindings.ColorNext = v }),
				keybindField("Previous Color", "Cycle the selected project to the previous color.", func(c config.Config) string { return c.Keybindings.ColorPrev }, func(c *config.Config, v string) { c.Keybindings.ColorPrev = v }),
			},
		},
	}
}

// keybindField builds a settingField for a single keybinding.
func keybindField(label, desc string, get func(config.Config) string, set func(*config.Config, string)) *settingField {
	return &settingField{
		label:       label,
		description: desc,
		kind:        fieldString,
		get:         get,
		set: func(c *config.Config, v string) error {
			v = strings.TrimSpace(v)
			if v == "" {
				return fmt.Errorf("keybinding cannot be empty")
			}
			set(c, v)
			return nil
		},
	}
}
