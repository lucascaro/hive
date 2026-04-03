package components

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// DirPickedMsg is sent when the user confirms a directory selection.
type DirPickedMsg struct{ Dir string }

// DirPickerCancelMsg is sent when the user cancels the directory picker.
type DirPickerCancelMsg struct{}

// dirItem is a single directory entry in the list.
type dirItem struct{ name string }

func (d dirItem) FilterValue() string { return d.name }
func (d dirItem) Title() string       { return "  " + d.name }
func (d dirItem) Description() string { return "" }

// overlayOverhead is the number of terminal lines consumed by the overlay
// chrome (borders, padding, header, path line, separator, footer).
const overlayOverhead = 10

// DirPicker is a directory-only browser with search-as-you-type filtering.
// It wraps bubbles/list so only directories appear; files are never shown.
//
// Keys:
//   - ↑/↓ / j/k         — navigate list
//   - enter              — descend into highlighted directory (and select it)
//   - h / ← / backspace  — go up one level
//   - /                  — open search / filter as you type
//   - esc                — cancel filter if active, otherwise cancel picker
//   - .                  — confirm current directory without descending
type DirPicker struct {
	Active     bool
	currentDir string
	readErr    error // set when the last loadDir call failed; shown in View
	list       list.Model
	delegate   list.DefaultDelegate
	height     int
}

// NewDirPicker creates a DirPicker with hive-themed styles.
func NewDirPicker() DirPicker {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetHeight(1)
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(styles.ColorAccent).
		BorderForeground(styles.ColorAccent).
		Bold(true)
	delegate.Styles.NormalTitle = delegate.Styles.NormalTitle.
		Foreground(styles.ColorText)

	const defaultHeight = 15
	dp := DirPicker{
		delegate: delegate,
		height:   defaultHeight,
	}
	dp.list = dp.buildList(nil)
	return dp
}

// buildList creates a fresh list.Model with the current delegate and height.
// Called on construction and on every directory change to avoid stale state.
func (dp *DirPicker) buildList(items []list.Item) list.Model {
	l := list.New(items, dp.delegate, 58, dp.height)
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()

	// Remove h/left from PrevPage so we can intercept them as "go up".
	km := l.KeyMap
	km.PrevPage = key.NewBinding(
		key.WithKeys("pgup", "ctrl+b"),
		key.WithHelp("pgup", "prev page"),
	)
	l.KeyMap = km
	return l
}

// SetHeight tells the picker how tall the terminal is so it can size the
// file list to fill as much of the overlay as possible.
func (dp *DirPicker) SetHeight(termHeight int) {
	h := termHeight - overlayOverhead
	if h < 5 {
		h = 5
	}
	dp.height = h
	dp.list.SetHeight(h)
}

// Show activates the picker rooted at initialDir.
func (dp *DirPicker) Show(initialDir string) tea.Cmd {
	dp.Active = true
	dir := expandHome(initialDir)
	if dir == "" {
		dir, _ = os.Getwd()
	}
	dp.loadDir(dir)
	return nil
}

// loadDir builds a fresh list populated with the subdirectories of dir.
// Recreating the list on each navigation avoids stale cursor/filter state.
func (dp *DirPicker) loadDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Keep the previous directory so the user can navigate away.
		dp.readErr = err
		return
	}
	dp.readErr = nil
	dp.currentDir = dir
	items := make([]list.Item, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		items = append(items, dirItem{name: e.Name()})
	}
	dp.list = dp.buildList(items)
}

// Update handles all tea.Msg events.
// Returns a tea.Cmd and whether the event was consumed.
func (dp *DirPicker) Update(msg tea.Msg) (tea.Cmd, bool) {
	if !dp.Active {
		return nil, false
	}

	if km, ok := msg.(tea.KeyMsg); ok {
		filtering := dp.list.SettingFilter()

		if !filtering {
			switch km.String() {
			case "esc":
				dp.Active = false
				return func() tea.Msg { return DirPickerCancelMsg{} }, true
			case ".":
				dp.Active = false
				dir := dp.currentDir
				return func() tea.Msg { return DirPickedMsg{Dir: dir} }, true
			case "h", "left", "backspace":
				parent := filepath.Dir(dp.currentDir)
				if parent != dp.currentDir {
					dp.loadDir(parent)
				}
				return nil, true
			case "enter":
				if item, ok := dp.list.SelectedItem().(dirItem); ok {
					dp.loadDir(filepath.Join(dp.currentDir, item.name))
				}
				return nil, true
			}
		}
		// While filtering, all keys (including esc to clear) go to the list.
	}

	var cmd tea.Cmd
	dp.list, cmd = dp.list.Update(msg)
	return cmd, false
}

// View renders the picker as a styled modal overlay. Returns empty when inactive.
func (dp *DirPicker) View() string {
	if !dp.Active {
		return ""
	}

	displayPath := shortenPath(dp.currentDir)
	header := styles.TitleStyle.Render("Select Directory") + "\n" +
		styles.MutedStyle.Render(displayPath) + "\n" +
		strings.Repeat("─", 54)

	footer := styles.MutedStyle.Render(
		"↑/↓: navigate  enter: open  ←: up  /: search  .: here  esc: cancel",
	)

	listView := dp.list.View()
	if dp.readErr != nil {
		listView = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).
			Render("Error: " + dp.readErr.Error())
	}

	body := header + "\n" +
		listView + "\n" +
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
