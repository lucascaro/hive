package tui

import (
	"strings"

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
	CursorUp       key.Binding
	CursorDown     key.Binding
	CursorLeft     key.Binding
	CursorRight    key.Binding
	SessionColorNext key.Binding
	SessionColorPrev key.Binding
	ToggleAll      key.Binding
	InputMode      key.Binding
	Detach         key.Binding
	Confirm        key.Binding
	Cancel         key.Binding
	Dismiss        key.Binding
	JumpToProject  key.Binding
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

// bind builds a key.Binding from a configured KeyBinding plus optional
// always-on extras (e.g. arrow-key aliases that must work even when the user
// customized the action). Empty entries are skipped and duplicates removed,
// preserving order so the help label keeps the user's primary key first.
//
// label controls the help-overlay text: pass "" to auto-join the resolved keys
// with "/" (e.g. "a/enter"); pass an explicit string for a stylized form
// (e.g. "↑").
//
// When no keys resolve, the returned binding is disabled — bubbles' help
// widget filters disabled bindings from FullHelp, so this does not produce
// blank rows. Custom help renderers should also check Enabled().
func bind(kb config.KeyBinding, label, desc string, extras ...string) key.Binding {
	keys := uniqueKeys(append(append([]string(nil), kb...), extras...)...)
	if len(keys) == 0 {
		return key.NewBinding(key.WithDisabled())
	}
	if label == "" {
		label = strings.Join(keys, "/")
	}
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, desc))
}

// NewKeyMap builds a KeyMap from the loaded config.
func NewKeyMap(kb config.KeybindingsConfig) KeyMap {
	return KeyMap{
		NewWorktreeSession: bind(kb.NewWorktreeSession, "", "new worktree session"),
		NewProject:     bind(kb.NewProject, "", "new project"),
		NewSession:     bind(kb.NewSession, "", "new session"),
		NewTeam:        bind(kb.NewTeam, "", "new team"),
		KillSession:    bind(kb.KillSession, "", "kill session"),
		KillTeam:       bind(kb.KillTeam, "", "kill team"),
		Rename:         bind(kb.Rename, "", "rename"),
		Attach:         bind(kb.Attach, "", "attach"),
		ToggleCollapse: bind(kb.ToggleCollapse, "space", "toggle"),
		CollapseItem:   bind(kb.CollapseItem, "←/h", "collapse"),
		ExpandItem:     bind(kb.ExpandItem, "→/l", "expand"),
		NavUp:          bind(kb.NavUp, "↑", "up", "up"),
		NavDown:        bind(kb.NavDown, "↓", "down", "down"),
		NavProjectUp:   bind(kb.NavProjectUp, "", "prev project"),
		NavProjectDown: bind(kb.NavProjectDown, "", "next project"),
		Filter:         bind(kb.Filter, "", "filter"),
		SidebarView:    bind(kb.SidebarView, "", "sidebar view"),
		GridOverview:   bind(kb.GridOverview, "", "grid view"),
		Palette:        bind(kb.Palette, "", "palette"),
		Help:           bind(kb.Help, "", "help"),
		TmuxHelp:       bind(kb.TmuxHelp, "", "tmux shortcuts"),
		Settings:       bind(kb.Settings, "", "settings"),
		Quit:           bind(kb.Quit, "", "quit"),
		QuitKill:       bind(kb.QuitKill, "", "quit+kill"),
		ColorNext:      bind(kb.ColorNext, "", "next color"),
		ColorPrev:      bind(kb.ColorPrev, "", "prev color"),
		MoveUp:         bind(kb.MoveUp, "", "move up"),
		MoveDown:       bind(kb.MoveDown, "", "move down"),
		MoveLeft:       bind(kb.MoveLeft, "", "move left"),
		MoveRight:      bind(kb.MoveRight, "", "move right"),
		CursorUp:       bind(kb.CursorUp, "", "cursor up"),
		CursorDown:     bind(kb.CursorDown, "", "cursor down"),
		CursorLeft:     bind(kb.CursorLeft, "", "cursor left"),
		CursorRight:    bind(kb.CursorRight, "", "cursor right"),
		SessionColorNext: bind(kb.SessionColorNext, "", "next session color"),
		SessionColorPrev: bind(kb.SessionColorPrev, "", "prev session color"),
		ToggleAll:      bind(kb.ToggleAll, "", "toggle all-grid"),
		InputMode:      bind(kb.InputMode, "", "input mode"),
		Detach:         bind(kb.Detach, "", "detach"),
		Confirm:        key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y/enter", "confirm")),
		Cancel:         key.NewBinding(key.WithKeys("esc", "n"), key.WithHelp("esc/n", "cancel")),
		Dismiss:        key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
		JumpToProject:  bind(kb.JumpToProject, jumpLabel(kb.JumpToProject), "jump to project"),
	}
}

// jumpLabel renders the JumpToProject help string. For the 1-9 default we
// collapse to "[1-9]"; for any other binding we show the first and last key
// as a range (e.g. "[F1-F9]" or just "[F1]" for a single-key bind). Keeps
// the help overlay column narrow.
func jumpLabel(kb config.KeyBinding) string {
	keys := []string(kb)
	if len(keys) == 0 {
		return ""
	}
	if len(keys) == 1 {
		return "[" + keys[0] + "]"
	}
	return "[" + keys[0] + "-" + keys[len(keys)-1] + "]"
}

// HelpKeyLabel returns the display string for the Help key binding.
// Used by overlays that want to render the configured key in footers.
func (km KeyMap) HelpKeyLabel() string { return km.Help.Help().Key }

// QuitKeyLabel returns the display string for the Quit key binding.
func (km KeyMap) QuitKeyLabel() string { return km.Quit.Help().Key }

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
		km.Quit,
	}
}

// FullHelp returns all bindings grouped into columns for the full help overlay.
// Implements help.KeyMap.
func (km KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{km.NavUp, km.NavDown, km.NavProjectUp, km.NavProjectDown, km.CursorUp, km.CursorDown, km.CursorLeft, km.CursorRight, km.JumpToProject, km.CollapseItem, km.ExpandItem, km.ToggleCollapse},
		{km.MoveUp, km.MoveDown, km.MoveLeft, km.MoveRight, km.Attach, km.InputMode, km.Detach, km.NewSession, km.NewWorktreeSession, km.NewTeam, km.NewProject, km.Rename, km.KillSession, km.KillTeam},
		{km.ColorNext, km.ColorPrev, km.SessionColorNext, km.SessionColorPrev, km.Filter, km.SidebarView, km.GridOverview, km.ToggleAll},
		{km.Help, km.TmuxHelp, km.Settings, km.Palette, km.Quit, km.QuitKill},
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
		// ExitGrid.WithKeys only needs the Dismiss keys — g/G are consumed by
		// the outer grid switch in handleGridKey (they toggle project↔all
		// grid mode there), so they never reach the GridView component. The
		// help label still shows g/G because the user experiences them as
		// "exit grid" when already in the matching mode (see the
		// GridOverview/ToggleAll branches that call closeGrid).
		ExitGrid: key.NewBinding(key.WithKeys(km.Dismiss.Keys()...), key.WithHelp(km.Dismiss.Help().Key+"/g/G", "exit")),
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
