package tui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

// handlePalettePicked dispatches the selected command palette action. Each
// action maps to the same code path as pressing the corresponding keybinding.
func (m Model) handlePalettePicked(msg components.CommandPalettePickedMsg) (tea.Model, tea.Cmd) {
	m.PopView() // pop palette

	switch msg.Action {
	// Session actions
	case "attach":
		if attach := m.pendingAttachDetails(); attach != nil {
			cmd := m.doAttach(*attach)
			return m, cmd
		}
	case "new-session":
		sel := m.sidebar.Selected()
		if sel == nil {
			return m, nil
		}
		pid := sel.ProjectID
		if pid == "" {
			return m, nil
		}
		m.pendingProjectID = pid
		m.pendingWorktree = false
		m.inputMode = "new-session"
		m.agentPicker.Show(m.sortedAgentItems())
		m.PushView(ViewAgentPicker)
		return m, nil
	case "new-worktree":
		sel := m.sidebar.Selected()
		if sel == nil {
			return m, nil
		}
		pid := sel.ProjectID
		if pid == "" {
			return m, nil
		}
		if cmd := m.initWorktreeSession(pid); cmd != nil {
			return m, cmd
		}
	case "kill-session":
		sel := m.sidebar.Selected()
		if sel != nil && sel.SessionID != "" {
			return m, func() tea.Msg {
				return ConfirmActionMsg{
					Message: "Kill session \"" + sel.Label + "\"?",
					Action:  "kill-session:" + sel.SessionID,
				}
			}
		}
	case "rename":
		return m, m.startRename()

	// Project & team actions
	case "new-project":
		m.nameInput.Placeholder = "my-project"
		m.nameInput.Reset()
		m.PushView(ViewProjectName)
		blinkCmd := m.nameInput.Focus()
		return m, blinkCmd
	case "new-team":
		sel := m.sidebar.Selected()
		if sel == nil || sel.ProjectID == "" {
			return m, nil
		}
		workDir := ""
		if proj := state.FindProject(&m.appState, sel.ProjectID); proj != nil {
			workDir = proj.Directory
		}
		if workDir == "" {
			workDir, _ = os.Getwd()
		}
		m.teamBuilder.Start(workDir)
		m.pendingProjectID = sel.ProjectID
		m.PushView(ViewTeamBuilder)
		return m, nil
	case "kill-team":
		sel := m.sidebar.Selected()
		if sel != nil && sel.TeamID != "" {
			return m, func() tea.Msg {
				return ConfirmActionMsg{
					Message: "Kill team \"" + sel.Label + "\" and all its sessions?",
					Action:  "kill-team:" + sel.TeamID,
				}
			}
		}

	// View actions
	case "grid":
		m.openGrid(state.GridRestoreProject)
		m.polling.Invalidate()
		return m, m.scheduleGridPoll()
	case "grid-all":
		m.openGrid(state.GridRestoreAll)
		m.polling.Invalidate()
		return m, m.scheduleGridPoll()
	case "sidebar":
		// Already in sidebar — no-op.
	case "filter":
		m.appState.FilterQuery = ""
		m.PushView(ViewFilter)
		return m, nil

	// Appearance
	case "color-next":
		if sel := m.sidebar.Selected(); sel != nil && sel.ProjectID != "" {
			m.cycleProjectColor(sel.ProjectID, 1)
		}
	case "color-prev":
		if sel := m.sidebar.Selected(); sel != nil && sel.ProjectID != "" {
			m.cycleProjectColor(sel.ProjectID, -1)
		}
	case "session-color-next":
		if sel := m.sidebar.Selected(); sel != nil && sel.SessionID != "" {
			m.cycleSessionColor(sel.SessionID, 1)
		}
	case "session-color-prev":
		if sel := m.sidebar.Selected(); sel != nil && sel.SessionID != "" {
			m.cycleSessionColor(sel.SessionID, -1)
		}

	// Help & settings
	case "help":
		m.helpPanel.Open(0)
		m.PushView(ViewHelp)
		return m, nil
	case "tmux-help":
		m.helpPanel.Open(1)
		m.PushView(ViewHelp)
		return m, nil
	case "settings":
		m.settings.Width = m.appState.TermWidth
		m.settings.Height = m.appState.TermHeight
		m.settings.Open(m.cfg)
		m.PushView(ViewSettings)
		return m, nil

	// Quit
	case "quit":
		return m, tea.Quit
	case "quit-kill":
		return m, func() tea.Msg {
			return ConfirmActionMsg{
				Message: "Quit and kill all sessions?",
				Action:  "quit-kill",
			}
		}
	}
	return m, nil
}
