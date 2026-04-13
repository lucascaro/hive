package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/git"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

func (m Model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.appState.TermWidth = msg.Width
	m.appState.TermHeight = msg.Height
	m.dirPicker.SetHeight(msg.Height)
	if m.whatsNewContent != "" {
		vpW := msg.Width - 16 // account for border + padding
		if vpW > 64 {
			vpW = 64
		}
		if vpW < 10 {
			vpW = 10
		}
		vpH := msg.Height - 14 // account for border + padding + title + hints
		if vpH > 24 {
			vpH = 24
		}
		if vpH < 3 {
			vpH = 3
		}
		m.whatsNewViewport.Width = vpW
		m.whatsNewViewport.Height = vpH
	}
	m.recomputeLayout()
	return m, nil
}

func (m Model) handleStateWatch(msg stateWatchMsg) (tea.Model, tea.Cmd) {
	if !msg.mtime.Equal(m.stateLastKnownMtime) {
		debugLog.Printf("stateWatchMsg: external change detected (mtime=%v), reloading", msg.mtime)
		m.stateLastKnownMtime = msg.mtime
		m.reloadStateFromDisk()
		return m, tea.Batch(
			scheduleWatchState(m.stateLastKnownMtime),
			m.schedulePollPreview(),
		)
	}
	return m, scheduleWatchState(m.stateLastKnownMtime)
}

func (m Model) handleSettingsSaveRequest(msg components.SettingsSaveRequestMsg) (tea.Model, tea.Cmd) {
	newCfg := msg.Config
	if err := config.Save(newCfg); err != nil {
		// Pop the (already-deactivated) settings view so the main statusbar
		// can surface the error — otherwise the user sees a black screen.
		if m.TopView() == ViewSettings {
			m.settings.Close()
			m.PopView()
		}
		return m, func() tea.Msg {
			return ErrorMsg{Err: fmt.Errorf("save settings: %w", err)}
		}
	}
	m.cfg = newCfg
	m.keys = NewKeyMap(newCfg.Keybindings)
	return m, func() tea.Msg { return ConfigSavedMsg{Config: newCfg} }
}

func (m Model) handleSettingsClosed() (tea.Model, tea.Cmd) {
	m.settings.Close()
	m.PopView()
	return m, nil
}

func (m Model) handleConfigSaved() (tea.Model, tea.Cmd) {
	m.appState.LastError = "" // clear any previous error
	m.pendingSaveConfig = config.Config{}
	if m.TopView() == ViewSettings {
		m.settings.Close()
		m.PopView()
	}
	return m, nil
}

func (m Model) handleError(msg ErrorMsg) (tea.Model, tea.Cmd) {
	m.appState.LastError = msg.Err.Error()
	return m, nil
}

func (m Model) handlePersist() (tea.Model, tea.Cmd) {
	m.persist()
	return m, nil
}

func (m Model) handleQuitAndKill() (tea.Model, tea.Cmd) {
	m.killAllSessions()
	return m, tea.Quit
}

func (m Model) handleSettingsSaveConfirm(msg components.SettingsSaveConfirmMsg) (tea.Model, tea.Cmd) {
	m.pendingSaveConfig = msg.Config
	path := config.ConfigPath()
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			path = "~" + string(filepath.Separator) + rel
		}
	}
	confirmMsg := fmt.Sprintf("Save settings to %s?", path)
	m.appState.ConfirmMsg = confirmMsg
	m.appState.ConfirmAction = "save-settings"
	m.confirm.Message = confirmMsg
	m.confirm.Action = "save-settings"
	m.PushView(ViewConfirm)
	return m, nil
}

func (m Model) handleConfirmAction(msg ConfirmActionMsg) (tea.Model, tea.Cmd) {
	m.appState.ConfirmMsg = msg.Message
	m.appState.ConfirmAction = msg.Action
	m.confirm.Message = msg.Message
	m.confirm.Action = msg.Action
	m.PushView(ViewConfirm)
	return m, nil
}

func (m Model) handleConfirmed(msg ConfirmedMsg) (tea.Model, tea.Cmd) {
	m.confirm.Message = ""
	return m, m.handleConfirmedAction(msg.Action)
}

func (m Model) handleCancelled() (tea.Model, tea.Cmd) {
	popped := m.PopView()
	// Clean up component state for the popped view.
	switch popped {
	case ViewRename:
		m.titleEditor.Stop()
	case ViewAgentPicker:
		m.agentPicker.Hide()
	case ViewTeamBuilder:
		m.teamBuilder.Hide()
	case ViewProjectName, ViewCustomCmd, ViewWorktreeBranch, ViewDirConfirm:
		m.nameInput.Blur()
	}
	m.pendingWorktree = false
	m.pendingWorktreeAgentType = ""
	m.pendingWorktreeAgentCmd = nil
	return m, nil
}

func (m Model) handleAgentPicked(msg components.AgentPickedMsg) (tea.Model, tea.Cmd) {
	// Team builder owns agent selection while it is active.
	if m.HasView(ViewTeamBuilder) {
		cmd := m.teamBuilder.Update(msg)
		return m, cmd
	}
	agentTypeStr := string(msg.AgentType)
	// Custom command: prompt user for the command to run.
	if msg.AgentType == state.AgentCustom {
		m.nameInput.Placeholder = "command (empty = default shell)"
		m.nameInput.Reset()
		m.ReplaceTop(ViewCustomCmd)
		blinkCmd := m.nameInput.Focus()
		return m, blinkCmd
	}
	profile := m.cfg.Agents[agentTypeStr]
	agentBin := agentTypeStr
	if len(profile.Cmd) > 0 {
		agentBin = profile.Cmd[0]
	}
	if _, err := exec.LookPath(agentBin); err != nil {
		// Binary not found — prompt to install.
		m.pendingAgentType = agentTypeStr
		m.inputMode = ""
		m.PopView() // pop agent picker before confirm is pushed via message
		installInfo := ""
		if len(profile.InstallCmd) > 0 {
			installInfo = "\n\nInstall with: " + strings.Join(profile.InstallCmd, " ")
		}
		return m, func() tea.Msg {
			return ConfirmActionMsg{
				Message: fmt.Sprintf("%q not found in PATH.%s\n\nInstall now?", agentBin, installInfo),
				Action:  "install-agent:" + agentTypeStr,
			}
		}
	}
	if m.pendingWorktree {
		// Worktree flow: collect branch name next.
		m.pendingWorktreeAgentType = agentTypeStr
		m.pendingWorktreeAgentCmd = profile.Cmd
		m.nameInput.Placeholder = "branch-name"
		m.nameInput.Reset()
		m.nameInput.SetValue(git.RandomBranchName())
		m.ReplaceTop(ViewWorktreeBranch)
		blinkCmd := m.nameInput.Focus()
		return m, blinkCmd
	}
	m.inputMode = ""
	m.PopView() // pop agent picker
	cmd := m.createSession(m.pendingProjectID, agentTypeStr, profile.Cmd)
	return m, cmd
}

func (m Model) handleAgentInstalled(msg AgentInstalledMsg) (tea.Model, tea.Cmd) {
	m.appState.LastError = ""
	m.appState.InstallingAgent = ""
	cmd := m.createSession(m.pendingProjectID, msg.AgentType, m.cfg.Agents[msg.AgentType].Cmd)
	m.pendingAgentType = ""
	return m, cmd
}

func (m Model) handleRecoveryPickerDone(msg components.RecoveryPickerDoneMsg) (tea.Model, tea.Cmd) {
	m.PopView()
	if len(msg.Selected) > 0 {
		m.recoverSessions(msg.Selected)
	}
	return m, nil
}

func (m Model) handleOrphanPickerDone(msg components.OrphanPickerDoneMsg) (tea.Model, tea.Cmd) {
	m.PopView()
	for _, name := range msg.Selected {
		_ = mux.KillSession(name)
	}
	return m, nil
}
