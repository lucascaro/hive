package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/lucascaro/hive/internal/config"
)

// KeyMap holds all key bindings for the application.
type KeyMap struct {
	NewWorktreeSession key.Binding
	NewProject     key.Binding
	NewSession     key.Binding
	NewTeam        key.Binding
	KillSession    key.Binding
	KillTeam       key.Binding
	Rename         key.Binding
	Attach         key.Binding
	ToggleCollapse key.Binding
	CollapseItem   key.Binding
	ExpandItem     key.Binding
	FocusToggle    key.Binding
	NavUp          key.Binding
	NavDown        key.Binding
	NavProjectUp   key.Binding
	NavProjectDown key.Binding
	Filter         key.Binding
	SidebarView    key.Binding
	GridOverview   key.Binding
	Palette        key.Binding
	Help           key.Binding
	TmuxHelp       key.Binding
	Settings       key.Binding
	Quit           key.Binding
	QuitKill       key.Binding
	ColorNext      key.Binding
	ColorPrev      key.Binding
	MoveUp         key.Binding
	MoveDown       key.Binding
	MoveLeft       key.Binding
	MoveRight      key.Binding
	Confirm        key.Binding
	Cancel         key.Binding
}

// uniqueKeys returns keys deduplicated, preserving order. Empty strings are skipped.
func uniqueKeys(keys ...string) []string {
	seen := make(map[string]bool, len(keys))
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if k != "" && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// NewKeyMap builds a KeyMap from the loaded config.
func NewKeyMap(kb config.KeybindingsConfig) KeyMap {
	return KeyMap{
		NewWorktreeSession: key.NewBinding(key.WithKeys(kb.NewWorktreeSession), key.WithHelp(kb.NewWorktreeSession, "new worktree session")),
		NewProject:     key.NewBinding(key.WithKeys(kb.NewProject), key.WithHelp(kb.NewProject, "new project")),
		NewSession:     key.NewBinding(key.WithKeys(kb.NewSession), key.WithHelp(kb.NewSession, "new session")),
		NewTeam:        key.NewBinding(key.WithKeys(kb.NewTeam), key.WithHelp(kb.NewTeam, "new team")),
		KillSession:    key.NewBinding(key.WithKeys(kb.KillSession), key.WithHelp(kb.KillSession, "kill session")),
		KillTeam:       key.NewBinding(key.WithKeys(kb.KillTeam), key.WithHelp(kb.KillTeam, "kill team")),
		Rename:         key.NewBinding(key.WithKeys(kb.Rename), key.WithHelp(kb.Rename, "rename")),
		Attach:         key.NewBinding(key.WithKeys(kb.Attach, "enter"), key.WithHelp(kb.Attach+"/enter", "attach")),
		ToggleCollapse: key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
		CollapseItem:   key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "collapse")),
		ExpandItem:     key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "expand")),
		FocusToggle:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch pane")),
		NavUp:          key.NewBinding(key.WithKeys(uniqueKeys(kb.NavUp, "up")...), key.WithHelp("↑", "up")),
		NavDown:        key.NewBinding(key.WithKeys(uniqueKeys(kb.NavDown, "down")...), key.WithHelp("↓", "down")),
		NavProjectUp:   key.NewBinding(key.WithKeys(kb.NavProjectUp), key.WithHelp(kb.NavProjectUp, "prev project")),
		NavProjectDown: key.NewBinding(key.WithKeys(kb.NavProjectDown), key.WithHelp(kb.NavProjectDown, "next project")),
		Filter:         key.NewBinding(key.WithKeys(kb.Filter), key.WithHelp(kb.Filter, "filter")),
		SidebarView:    key.NewBinding(key.WithKeys(kb.SidebarView), key.WithHelp(kb.SidebarView, "sidebar view")),
		GridOverview:   key.NewBinding(key.WithKeys(kb.GridOverview), key.WithHelp(kb.GridOverview, "grid view")),
		Palette:        key.NewBinding(key.WithKeys(kb.Palette), key.WithHelp(kb.Palette, "palette")),
		Help:           key.NewBinding(key.WithKeys(kb.Help), key.WithHelp(kb.Help, "help")),
		TmuxHelp:       key.NewBinding(key.WithKeys(kb.TmuxHelp), key.WithHelp(kb.TmuxHelp, "tmux shortcuts")),
		Settings:       key.NewBinding(key.WithKeys(kb.Settings), key.WithHelp(kb.Settings, "settings")),
		Quit:           key.NewBinding(key.WithKeys(kb.Quit), key.WithHelp(kb.Quit, "quit")),
		QuitKill:       key.NewBinding(key.WithKeys(kb.QuitKill), key.WithHelp(kb.QuitKill, "quit+kill")),
		ColorNext:      key.NewBinding(key.WithKeys(kb.ColorNext), key.WithHelp(kb.ColorNext, "next color")),
		ColorPrev:      key.NewBinding(key.WithKeys(kb.ColorPrev), key.WithHelp(kb.ColorPrev, "prev color")),
		MoveUp:         key.NewBinding(key.WithKeys(kb.MoveUp), key.WithHelp(kb.MoveUp, "move up")),
		MoveDown:       key.NewBinding(key.WithKeys(kb.MoveDown), key.WithHelp(kb.MoveDown, "move down")),
		MoveLeft:       key.NewBinding(key.WithKeys(kb.MoveLeft), key.WithHelp(kb.MoveLeft, "move left")),
		MoveRight:      key.NewBinding(key.WithKeys(kb.MoveRight), key.WithHelp(kb.MoveRight, "move right")),
		Confirm:        key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y/enter", "confirm")),
		Cancel:         key.NewBinding(key.WithKeys("esc", "n"), key.WithHelp("esc/n", "cancel")),
	}
}

// ShortHelp returns the most important sidebar bindings for the one-line status bar hint.
// Implements help.KeyMap.
func (km KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		km.Help,
		km.NewProject,
		km.NewSession,
		km.Attach,
		km.GridOverview,
		km.Rename,
		km.ColorNext,
		km.KillSession,
		km.Settings,
		km.FocusToggle,
		km.Quit,
	}
}

// FullHelp returns all bindings grouped into columns for the full help overlay.
// Implements help.KeyMap.
func (km KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{km.NavUp, km.NavDown, km.NavProjectUp, km.NavProjectDown, km.CollapseItem, km.ExpandItem, km.ToggleCollapse},
		{km.Attach, km.NewSession, km.NewWorktreeSession, km.NewTeam, km.NewProject, km.Rename, km.KillSession, km.KillTeam},
		{km.ColorNext, km.ColorPrev, km.MoveUp, km.MoveDown, km.MoveLeft, km.MoveRight, km.Filter, km.SidebarView, km.GridOverview},
		{km.Help, km.TmuxHelp, km.Settings, km.Palette, km.FocusToggle, km.Quit, km.QuitKill},
	}
}

// GridKeyMap is a KeyMap subset used for the grid view hint line.
type GridKeyMap struct {
	NavUp    key.Binding
	NavDown  key.Binding
	NavLeft  key.Binding
	NavRight key.Binding
	MoveLeft  key.Binding
	MoveRight key.Binding
	Attach   key.Binding
	Kill     key.Binding
	Rename   key.Binding
	ColorNext key.Binding
	ColorPrev key.Binding
	SessionColorNext key.Binding
	SessionColorPrev key.Binding
	ExitGrid  key.Binding
	ToggleAll key.Binding
	InputMode key.Binding
	Help      key.Binding
	Quit      key.Binding
}

// NewGridKeyMap builds a GridKeyMap from the main KeyMap.
func NewGridKeyMap(km KeyMap) GridKeyMap {
	return GridKeyMap{
		NavUp:    key.NewBinding(key.WithKeys("up"), key.WithHelp("↑↓←→", "navigate")),
		NavDown:  key.NewBinding(key.WithKeys("down"), key.WithHelp("", "")),
		NavLeft:  key.NewBinding(key.WithKeys("left"), key.WithHelp("", "")),
		NavRight: key.NewBinding(key.WithKeys("right"), key.WithHelp("", "")),
		MoveLeft:  key.NewBinding(key.WithKeys(km.MoveLeft.Keys()...), key.WithHelp(km.MoveLeft.Help().Key+"/"+km.MoveRight.Help().Key, "reorder")),
		MoveRight: key.NewBinding(key.WithKeys(km.MoveRight.Keys()...), key.WithHelp("", "")),
		Attach:   key.NewBinding(key.WithKeys(km.Attach.Keys()...), key.WithHelp(km.Attach.Help().Key, "attach")),
		Kill:     key.NewBinding(key.WithKeys(km.KillSession.Keys()...), key.WithHelp(km.KillSession.Help().Key, "kill")),
		Rename:   key.NewBinding(key.WithKeys(km.Rename.Keys()...), key.WithHelp(km.Rename.Help().Key, "rename")),
		ColorNext: key.NewBinding(key.WithKeys(km.ColorNext.Keys()...), key.WithHelp(km.ColorNext.Help().Key+"/"+km.ColorPrev.Help().Key, "project color")),
		ColorPrev: key.NewBinding(key.WithKeys(km.ColorPrev.Keys()...), key.WithHelp("", "")),
		SessionColorNext: key.NewBinding(key.WithKeys("v"), key.WithHelp("v/V", "session color")),
		SessionColorPrev: key.NewBinding(key.WithKeys("V"), key.WithHelp("", "")),
		ExitGrid:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc/g/G", "exit")),
		ToggleAll: key.NewBinding(key.WithKeys("G"), key.WithHelp("", "")),
		InputMode: key.NewBinding(key.WithKeys("i"), key.WithHelp("(i)", "input")),
		Help:      key.NewBinding(key.WithKeys(km.Help.Keys()...), key.WithHelp(km.Help.Help().Key, "help")),
		Quit:      key.NewBinding(key.WithKeys(km.Quit.Keys()...), key.WithHelp(km.Quit.Help().Key, "quit")),
	}
}

// ShortHelp returns the grid bindings for the one-line hint bar.
// Implements help.KeyMap.
func (gk GridKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		gk.NavUp,
		gk.MoveLeft,
		gk.Attach,
		gk.Kill,
		gk.Rename,
		gk.ColorNext,
		gk.SessionColorNext,
		gk.InputMode,
		gk.ExitGrid,
		gk.Help,
		gk.Quit,
	}
}

// FullHelp implements help.KeyMap (unused but required by the interface).
func (gk GridKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{gk.ShortHelp()}
}
