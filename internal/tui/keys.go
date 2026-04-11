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
	Confirm        key.Binding
	Cancel         key.Binding
}

// NewKeyMap builds a KeyMap from the loaded config.
func NewKeyMap(kb config.KeybindingsConfig) KeyMap {
	return KeyMap{
		NewWorktreeSession: key.NewBinding(key.WithKeys(kb.NewWorktreeSession), key.WithHelp(kb.NewWorktreeSession, "new worktree session")),
		NewProject:     key.NewBinding(key.WithKeys(kb.NewProject), key.WithHelp(kb.NewProject, "new project")),
		NewSession:     key.NewBinding(key.WithKeys(kb.NewSession), key.WithHelp(kb.NewSession, "new session")),
		NewTeam:        key.NewBinding(key.WithKeys(kb.NewTeam), key.WithHelp(kb.NewTeam, "new team")),
		KillSession:    key.NewBinding(key.WithKeys(kb.KillSession, "d"), key.WithHelp(kb.KillSession+"/d", "kill session")),
		KillTeam:       key.NewBinding(key.WithKeys(kb.KillTeam), key.WithHelp(kb.KillTeam, "kill team")),
		Rename:         key.NewBinding(key.WithKeys(kb.Rename), key.WithHelp(kb.Rename, "rename")),
		Attach:         key.NewBinding(key.WithKeys(kb.Attach, "enter"), key.WithHelp(kb.Attach+"/enter", "attach")),
		ToggleCollapse: key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
		CollapseItem:   key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "collapse")),
		ExpandItem:     key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "expand")),
		FocusToggle:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch pane")),
		NavUp:          key.NewBinding(key.WithKeys(kb.NavUp, "up"), key.WithHelp(kb.NavUp, "up")),
		NavDown:        key.NewBinding(key.WithKeys(kb.NavDown, "down"), key.WithHelp(kb.NavDown, "down")),
		NavProjectUp:   key.NewBinding(key.WithKeys(kb.NavProjectUp), key.WithHelp(kb.NavProjectUp, "prev project")),
		NavProjectDown: key.NewBinding(key.WithKeys(kb.NavProjectDown), key.WithHelp(kb.NavProjectDown, "next project")),
		Filter:         key.NewBinding(key.WithKeys(kb.Filter), key.WithHelp(kb.Filter, "filter")),
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
		Confirm:        key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y/enter", "confirm")),
		Cancel:         key.NewBinding(key.WithKeys("esc", "n"), key.WithHelp("esc/n", "cancel")),
	}
}
