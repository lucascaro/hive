package tui

import (
	"github.com/charmbracelet/bubbles/list"
	"github.com/lucascaro/hive/internal/tui/components"
)

// paletteItems builds the list of actions for the command palette from the
// current KeyMap. Each item shows the action name and its shortcut so users
// learn keybindings over time.
func (m *Model) paletteItems() []list.Item {
	km := m.keys
	return []list.Item{
		// Session actions
		components.NewPaletteItem("attach", "Attach to session", km.Attach.Help().Key),
		components.NewPaletteItem("new-session", "New session", km.NewSession.Help().Key),
		components.NewPaletteItem("new-worktree", "New worktree session", km.NewWorktreeSession.Help().Key),
		components.NewPaletteItem("kill-session", "Kill session", km.KillSession.Help().Key),
		components.NewPaletteItem("rename", "Rename session", km.Rename.Help().Key),

		// Project & team actions
		components.NewPaletteItem("new-project", "New project", km.NewProject.Help().Key),
		components.NewPaletteItem("new-team", "New team", km.NewTeam.Help().Key),
		components.NewPaletteItem("kill-team", "Kill team", km.KillTeam.Help().Key),

		// View actions
		components.NewPaletteItem("grid", "Grid view (project)", km.GridOverview.Help().Key),
		components.NewPaletteItem("grid-all", "Grid view (all)", km.ToggleAll.Help().Key),
		components.NewPaletteItem("sidebar", "Sidebar view", km.SidebarView.Help().Key),
		components.NewPaletteItem("filter", "Filter sessions", km.Filter.Help().Key),

		// Appearance
		components.NewPaletteItem("color-next", "Next project color", km.ColorNext.Help().Key),
		components.NewPaletteItem("color-prev", "Previous project color", km.ColorPrev.Help().Key),
		components.NewPaletteItem("session-color-next", "Next session color", km.SessionColorNext.Help().Key),
		components.NewPaletteItem("session-color-prev", "Previous session color", km.SessionColorPrev.Help().Key),

		// Help & settings
		components.NewPaletteItem("help", "Help", km.Help.Help().Key),
		components.NewPaletteItem("tmux-help", "Tmux shortcuts", km.TmuxHelp.Help().Key),
		components.NewPaletteItem("settings", "Settings", km.Settings.Help().Key),

		// Quit
		components.NewPaletteItem("quit", "Quit", km.Quit.Help().Key),
		components.NewPaletteItem("quit-kill", "Quit and kill all", km.QuitKill.Help().Key),
	}
}
