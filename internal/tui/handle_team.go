package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

func (m Model) handleTeamCreated(msg TeamCreatedMsg) (tea.Model, tea.Cmd) {
	m.appState.ActiveTeamID = msg.Team.ID
	m.commitState()
	return m, nil
}

func (m Model) handleTeamKilled(msg TeamKilledMsg) (tea.Model, tea.Cmd) {
	m.appState = *state.RemoveTeam(&m.appState, msg.TeamID)
	m.commitState()
	return m, nil
}

func (m Model) handleTeamBuilt(msg components.TeamBuiltMsg) (tea.Model, tea.Cmd) {
	m.PopView() // pop team builder
	cmd := m.createTeam(msg.Spec)
	return m, cmd
}
