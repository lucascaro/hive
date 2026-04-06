package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/git"
	"github.com/lucascaro/hive/internal/state"
)

func (m *Model) startRename() tea.Cmd {
	sel := m.sidebar.Selected()
	if sel == nil {
		return nil
	}
	current := sel.Label
	m.titleEditor.Start(sel.SessionID, sel.TeamID, sel.ProjectID, current)
	m.PushView(ViewRename)
	return m.titleEditor.Update(nil)
}

func defaultShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/sh"
}

func (m Model) handleFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter":
		m.PopView()
		if msg.String() == "esc" {
			m.appState.FilterQuery = ""
			m.sidebar.FilterQuery = ""
			m.sidebar.Rebuild(&m.appState)
		}
		return m, nil
	case "backspace":
		if len(m.appState.FilterQuery) > 0 {
			m.appState.FilterQuery = m.appState.FilterQuery[:len(m.appState.FilterQuery)-1]
			m.sidebar.FilterQuery = m.appState.FilterQuery
			m.sidebar.Rebuild(&m.appState)
		}
		return m, nil
	default:
		if len(msg.String()) == 1 {
			m.appState.FilterQuery += msg.String()
			m.sidebar.FilterQuery = m.appState.FilterQuery
			m.sidebar.Rebuild(&m.appState)
		}
	}
	return m, nil
}

func (m Model) handleNameInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		val := strings.TrimSpace(m.nameInput.Value())
		if val == "" {
			return m, nil
		}
		// Step 1 done: open the interactive directory picker.
		m.pendingProjectName = val
		m.nameInput.Blur()
		cwd, _ := os.Getwd()
		m.dirPicker.Show(cwd)
		m.ReplaceTop(ViewDirPicker)
		return m, nil
	case "esc":
		m.nameInput.Blur()
		m.PopView()
		m.pendingProjectName = ""
		return m, nil
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
}

func (m Model) handleDirConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		dir := strings.TrimSpace(m.nameInput.Value())
		if err := os.MkdirAll(dir, 0755); err != nil {
			m.PopView()
			m.pendingProjectName = ""
			return m, func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("create directory: %w", err)} }
		}
		m.PopView()
		cmd := m.createProject(m.pendingProjectName, dir)
		m.pendingProjectName = ""
		return m, cmd
	case "n", "N", "esc":
		// Return to directory picker so user can choose a different path.
		dir := strings.TrimSpace(m.nameInput.Value())
		m.dirPicker.Show(dir)
		m.ReplaceTop(ViewDirPicker)
		return m, nil
	}
	return m, nil
}

func (m Model) handleWorktreeBranchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		branch := strings.TrimSpace(m.nameInput.Value())
		if branch == "" {
			return m, nil
		}
		m.nameInput.Blur()
		m.PopView()
		agentType := m.pendingWorktreeAgentType
		agentCmd := m.pendingWorktreeAgentCmd
		m.pendingWorktreeAgentType = ""
		m.pendingWorktreeAgentCmd = nil
		m.pendingWorktree = false
		return m, m.createSessionWithWorktree(m.pendingProjectID, agentType, agentCmd, branch)
	case "esc":
		m.nameInput.Blur()
		m.PopView()
		m.pendingWorktree = false
		m.pendingWorktreeAgentType = ""
		m.pendingWorktreeAgentCmd = nil
		return m, nil
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
}

func (m Model) handleCustomCommandInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		raw := strings.TrimSpace(m.nameInput.Value())
		m.nameInput.Blur()

		var agentCmd []string
		if raw == "" {
			agentCmd = []string{defaultShell()}
		} else {
			agentCmd = []string{raw}
		}

		if m.pendingWorktree {
			// Chain to worktree branch input.
			m.pendingWorktreeAgentType = "custom"
			m.pendingWorktreeAgentCmd = agentCmd
			m.nameInput.Placeholder = "branch-name"
			m.nameInput.Reset()
			m.nameInput.SetValue(git.RandomBranchName())
			m.ReplaceTop(ViewWorktreeBranch)
			blinkCmd := m.nameInput.Focus()
			return m, blinkCmd
		}
		m.PopView()
		return m, m.createSession(m.pendingProjectID, "custom", agentCmd)
	case "esc":
		m.nameInput.Blur()
		m.PopView()
		m.pendingWorktree = false
		return m, nil
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
}

func (m Model) handleTitleEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		newTitle := m.titleEditor.Value()
		sessionID := m.titleEditor.SessionID
		teamID := m.titleEditor.TeamID
		projectID := m.titleEditor.ProjectID
		m.titleEditor.Stop()
		m.PopView()
		if newTitle == "" {
			return m, nil
		}
		if sessionID != "" {
			return m, func() tea.Msg {
				return SessionTitleChangedMsg{
					SessionID: sessionID,
					Title:     newTitle,
					Source:    state.TitleSourceUser,
				}
			}
		}
		if teamID != "" {
			return m, func() tea.Msg {
				return TeamNameChangedMsg{TeamID: teamID, Name: newTitle}
			}
		}
		if projectID != "" {
			return m, func() tea.Msg {
				return ProjectNameChangedMsg{ProjectID: projectID, Name: newTitle}
			}
		}
		return m, nil
	case "esc":
		m.titleEditor.Stop()
		m.PopView()
		return m, nil
	}
	cmd := m.titleEditor.Update(msg)
	return m, cmd
}

func (m Model) handleConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Confirm) {
		action := m.appState.ConfirmAction
		m.PopView()
		m.confirm.Message = ""
		return m, func() tea.Msg { return ConfirmedMsg{Action: action} }
	}
	if key.Matches(msg, m.keys.Cancel) {
		m.PopView()
		m.confirm.Message = ""
		return m, nil
	}
	return m, nil
}
