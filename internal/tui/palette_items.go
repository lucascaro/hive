package tui

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/lucascaro/hive/internal/tui/components"
)

// Palette action constants. Used by both paletteItems (to build the list) and
// handlePalettePicked (to dispatch). Keeping them in one place prevents silent
// no-ops from typos.
const (
	paletteAttach           = "attach"
	paletteNewSession       = "new-session"
	paletteNewWorktree      = "new-worktree"
	paletteKillSession      = "kill-session"
	paletteRename           = "rename"
	paletteNewProject       = "new-project"
	paletteNewTeam          = "new-team"
	paletteKillTeam         = "kill-team"
	paletteGrid             = "grid"
	paletteGridAll          = "grid-all"
	paletteSidebar          = "sidebar"
	paletteFilter           = "filter"
	paletteColorNext        = "color-next"
	paletteColorPrev        = "color-prev"
	paletteSessionColorNext = "session-color-next"
	paletteSessionColorPrev = "session-color-prev"
	paletteHelp             = "help"
	paletteTmuxHelp         = "tmux-help"
	paletteSettings         = "settings"
	paletteQuit             = "quit"
	paletteQuitKill         = "quit-kill"
)

// paletteItems builds the list of actions for the command palette from the
// current KeyMap. Each item shows the action name and its shortcut so users
// learn keybindings over time.
func (m *Model) paletteItems() []list.Item {
	km := m.keys
	return []list.Item{
		// Session actions
		components.NewPaletteItem(paletteAttach, "Attach to session", km.Attach.Help().Key),
		components.NewPaletteItem(paletteNewSession, "New session", km.NewSession.Help().Key),
		components.NewPaletteItem(paletteNewWorktree, "New worktree session", km.NewWorktreeSession.Help().Key),
		components.NewPaletteItem(paletteKillSession, "Kill session", km.KillSession.Help().Key),
		components.NewPaletteItem(paletteRename, "Rename session", km.Rename.Help().Key),

		// Project & team actions
		components.NewPaletteItem(paletteNewProject, "New project", km.NewProject.Help().Key),
		components.NewPaletteItem(paletteNewTeam, "New team", km.NewTeam.Help().Key),
		components.NewPaletteItem(paletteKillTeam, "Kill team", km.KillTeam.Help().Key),

		// View actions
		components.NewPaletteItem(paletteGrid, "Grid view (project)", km.GridOverview.Help().Key),
		components.NewPaletteItem(paletteGridAll, "Grid view (all)", km.ToggleAll.Help().Key),
		components.NewPaletteItem(paletteSidebar, "Sidebar view", km.SidebarView.Help().Key),
		components.NewPaletteItem(paletteFilter, "Filter sessions", km.Filter.Help().Key),

		// Appearance
		components.NewPaletteItem(paletteColorNext, "Next project color", km.ColorNext.Help().Key),
		components.NewPaletteItem(paletteColorPrev, "Previous project color", km.ColorPrev.Help().Key),
		components.NewPaletteItem(paletteSessionColorNext, "Next session color", km.SessionColorNext.Help().Key),
		components.NewPaletteItem(paletteSessionColorPrev, "Previous session color", km.SessionColorPrev.Help().Key),

		// Help & settings
		components.NewPaletteItem(paletteHelp, "Help", km.Help.Help().Key),
		components.NewPaletteItem(paletteTmuxHelp, "Tmux shortcuts", km.TmuxHelp.Help().Key),
		components.NewPaletteItem(paletteSettings, "Settings", km.Settings.Help().Key),

		// Quit
		components.NewPaletteItem(paletteQuit, "Quit", km.Quit.Help().Key),
		components.NewPaletteItem(paletteQuitKill, "Quit and kill all", km.QuitKill.Help().Key),
	}
}
