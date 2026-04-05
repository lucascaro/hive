package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

func (m Model) handleProjectCreated(msg ProjectCreatedMsg) (tea.Model, tea.Cmd) {
	m.appState.ActiveProjectID = msg.Project.ID
	m.commitState()
	return m, nil
}

func (m Model) handleProjectKilled(msg ProjectKilledMsg) (tea.Model, tea.Cmd) {
	m.appState = *state.RemoveProject(&m.appState, msg.ProjectID)
	m.commitState()
	return m, nil
}

func (m Model) handleDirPicked(msg components.DirPickedMsg) (tea.Model, tea.Cmd) {
	dir := msg.Dir
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			// Directory doesn't exist — ask for confirmation before creating.
			m.nameInput.SetValue(dir)
			m.ReplaceTop(ViewDirConfirm)
			return m, nil
		}
		// Unexpected error (e.g. permission denied) — surface it and abort.
		m.PopView()
		return m, func() tea.Msg {
			return ErrorMsg{Err: fmt.Errorf("check directory: %w", err)}
		}
	}
	name := m.pendingProjectName
	m.pendingProjectName = ""
	m.PopView()
	return m, m.createProject(name, dir)
}

func (m Model) handleDirPickerCancel() (tea.Model, tea.Cmd) {
	// Return to the project name step.
	m.nameInput.Reset()
	m.nameInput.SetValue(m.pendingProjectName)
	m.ReplaceTop(ViewProjectName)
	return m, m.nameInput.Focus()
}
