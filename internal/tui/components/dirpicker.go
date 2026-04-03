package components

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// DirPickedMsg is sent when the user confirms a directory selection.
type DirPickedMsg struct{ Dir string }

// DirPickerCancelMsg is sent when the user cancels the directory picker.
type DirPickerCancelMsg struct{}

// overlayOverhead is the number of terminal lines consumed by the overlay
// chrome (borders, padding, header, path line, separator, footer).
const overlayOverhead = 10

// DirPicker wraps bubbles/filepicker to provide a directory-only modal
// picker that integrates with the hive theme and message protocol.
//
// Keys:
//   - ↑/↓ / j/k    — navigate list
//   - enter / l / → — navigate into highlighted directory (also selects it)
//   - h / ← / backspace — go up one level
//   - .             — confirm current directory without descending
//   - esc           — cancel, return to project name step
type DirPicker struct {
	Active bool
	fp     filepicker.Model
}

// SetHeight tells the picker how tall the terminal is so it can size the
// file list to fill as much of the overlay as possible.
func (dp *DirPicker) SetHeight(termHeight int) {
	listHeight := termHeight - overlayOverhead
	if listHeight < 5 {
		listHeight = 5
	}
	dp.fp.Height = listHeight
}

// NewDirPicker creates a DirPicker with hive-themed styles.
func NewDirPicker() DirPicker {
	fp := filepicker.New()
	fp.DirAllowed = true
	fp.FileAllowed = false
	fp.ShowPermissions = false
	fp.ShowSize = false
	fp.ShowHidden = false
	fp.AutoHeight = false
	fp.Height = 15 // default until first WindowSizeMsg
	fp.Cursor = "▸"

	// Remove esc from Back so we can intercept it as cancel.
	km := filepicker.DefaultKeyMap()
	km.Back = key.NewBinding(
		key.WithKeys("h", "backspace", "left"),
		key.WithHelp("h/←", "up"),
	)
	fp.KeyMap = km

	// Apply hive colour theme.
	s := filepicker.DefaultStyles()
	s.Cursor = s.Cursor.Foreground(styles.ColorAccent)
	s.Directory = s.Directory.Foreground(styles.ColorAccent).Bold(true)
	s.Selected = s.Selected.Foreground(styles.ColorText).Bold(true)
	s.DisabledCursor = s.DisabledCursor.Foreground(styles.ColorMuted)
	s.DisabledFile = s.DisabledFile.Foreground(styles.ColorMuted)
	fp.Styles = s

	return DirPicker{fp: fp}
}

// Show activates the picker rooted at initialDir and returns the Init cmd
// that loads the first directory listing. The caller must include this cmd
// in the returned tea.Batch.
func (dp *DirPicker) Show(initialDir string) tea.Cmd {
	dp.Active = true
	dir := expandHome(initialDir)
	if dir == "" {
		dir, _ = os.Getwd()
	}
	dp.fp.CurrentDirectory = dir
	return dp.fp.Init()
}

// Update handles all tea.Msg events (not just KeyMsg) so the filepicker's
// internal readDirMsg messages are delivered correctly.
// Returns a tea.Cmd and whether the event was consumed.
// When the user confirms (enter on a dir, or ".") or cancels (esc),
// Active is set to false and the corresponding message is emitted.
func (dp *DirPicker) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !dp.Active {
		return nil, false
	}

	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			dp.Active = false
			return func() tea.Msg { return DirPickerCancelMsg{} }, true
		case ".":
			// Confirm the currently browsed directory without descending.
			dp.Active = false
			dir := dp.fp.CurrentDirectory
			return func() tea.Msg { return DirPickedMsg{Dir: dir} }, true
		}
	}

	// Capture the path before Update so we can detect a navigation event.
	// DidSelectFile is unreliable because m.files is stale (readDir is async)
	// after the filepicker navigates; comparing fp.Path before/after is safe.
	prevPath := dp.fp.Path

	var cmd tea.Cmd
	dp.fp, cmd = dp.fp.Update(msg)

	if dp.fp.Path != prevPath && dp.fp.Path != "" {
		dp.Active = false
		dir := dp.fp.Path
		dp.fp.Path = "" // reset for reuse
		return func() tea.Msg { return DirPickedMsg{Dir: dir} }, true
	}

	return cmd, false
}

// View renders the picker as a styled modal overlay. Returns empty when inactive.
func (dp *DirPicker) View() string {
	if !dp.Active {
		return ""
	}

	displayPath := shortenPath(dp.fp.CurrentDirectory)
	header := styles.TitleStyle.Render("Select Directory") + "\n" +
		styles.MutedStyle.Render(displayPath) + "\n" +
		strings.Repeat("─", 54)

	footer := styles.MutedStyle.Render(
		"↑/↓: navigate  enter/→: open  ←: up  .: here  esc: cancel",
	)

	body := header + "\n" +
		dp.fp.View() + "\n" +
		footer

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(62).
		Render(body)
}

// expandHome replaces a leading "~" with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return home + path[1:]
}

// shortenPath replaces the home directory prefix with "~" for display.
func shortenPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
