package tui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/git"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

// handleKey dispatches keyboard events based on the view stack top.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ctrl+c always quits, regardless of focus.
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.TopView() {
	case ViewSettings:
		cmd, _ := m.settings.Update(msg)
		return m, cmd
	case ViewGrid:
		cmd := m.handleGridKey(msg)
		return m, cmd
	case ViewHelp, ViewTmuxHelp:
		if msg.String() == "esc" {
			m.PopView()
		}
		return m, nil
	case ViewAttachHint:
		return m.handleAttachHint(msg)
	case ViewConfirm:
		return m.handleConfirm(msg)
	case ViewRecovery:
		updated, cmd := m.recoveryPicker.Update(msg)
		m.recoveryPicker = updated
		return m, cmd
	case ViewOrphan:
		updated, cmd := m.orphanPicker.Update(msg)
		m.orphanPicker = updated
		return m, cmd
	case ViewAgentPicker:
		cmd, _ := m.agentPicker.Update(msg)
		return m, cmd
	case ViewTeamBuilder:
		cmd := m.teamBuilder.Update(msg)
		return m, cmd
	case ViewRename:
		return m.handleTitleEdit(msg)
	case ViewProjectName:
		return m.handleNameInput(msg)
	case ViewDirPicker:
		cmd, _ := m.dirPicker.Update(msg)
		return m, cmd
	case ViewDirConfirm:
		return m.handleDirConfirm(msg)
	case ViewCustomCmd:
		return m.handleCustomCommandInput(msg)
	case ViewWorktreeBranch:
		return m.handleWorktreeBranchInput(msg)
	case ViewFilter:
		return m.handleFilter(msg)
	}

	return m.handleGlobalKey(msg)
}

// handleGridKey handles keys when the grid overview is active.
func (m *Model) handleGridKey(msg tea.KeyMsg) tea.Cmd {
	m.gridView.Width = m.appState.TermWidth
	m.gridView.Height = m.appState.TermHeight
	switch msg.String() {
	case "G":
		prevID := ""
		if s := m.gridView.Selected(); s != nil {
			prevID = s.ID
		}
		m.gridView.Show(m.gridSessions(state.GridRestoreAll), state.GridRestoreAll)
		m.gridView.SetProjectNames(m.gridProjectNames())
		m.gridView.SyncCursor(prevID)
		return m.scheduleGridPoll()
	case "x":
		if sess := m.gridView.Selected(); sess != nil {
			s := sess
			// Grid stays in the stack; confirm dialog is pushed on top.
			return func() tea.Msg {
				return ConfirmActionMsg{
					Message: fmt.Sprintf("Kill session %q?", s.Title),
					Action:  "kill-session:" + s.ID,
				}
			}
		}
	case "r":
		if sess := m.gridView.Selected(); sess != nil {
			m.sidebar.SyncActiveSession(sess.ID)
			m.appState.ActiveSessionID = sess.ID
			// Grid stays in the stack; rename dialog is pushed on top.
			return m.startRename()
		}
	case "t":
		if sess := m.gridView.Selected(); sess != nil && sess.ProjectID != "" {
			m.pendingProjectID = sess.ProjectID
			m.pendingWorktree = false
			m.inputMode = "new-session"
			m.agentPicker.Show(m.sortedAgentItems())
			m.PushView(ViewAgentPicker)
			return nil
		}
	case "W":
		if sess := m.gridView.Selected(); sess != nil && sess.ProjectID != "" {
			projDir := ""
			if proj := state.FindProject(&m.appState, sess.ProjectID); proj != nil {
				projDir = proj.Directory
			}
			if projDir == "" {
				projDir, _ = os.Getwd()
			}
			if !git.IsGitRepo(projDir) {
				return func() tea.Msg {
					return ErrorMsg{Err: fmt.Errorf("project directory is not a git repository")}
				}
			}
			m.pendingProjectID = sess.ProjectID
			m.pendingWorktree = true
			m.inputMode = "new-session"
			m.agentPicker.Show(m.sortedAgentItems())
			m.PushView(ViewAgentPicker)
			return nil
		}
	}
	prevSel := m.gridView.Selected()
	cmd, _ := m.gridView.Update(msg)
	// gridView.Update may set Active=false (esc/q/enter). Detect that and
	// pop the grid from the stack, syncing state to the selected session.
	if !m.gridView.Active && prevSel != nil {
		// Grid closed itself — pop it from the stack.
		m.PopView()
		m.appState.ActiveSessionID = prevSel.ID
		if prevSel.ProjectID != "" {
			m.appState.ActiveProjectID = prevSel.ProjectID
		}
		if prevSel.TeamID != "" {
			m.appState.ActiveTeamID = prevSel.TeamID
		}
		m.sidebar.SyncActiveSession(prevSel.ID)
		m.previewPollGen++
		return tea.Batch(cmd, m.schedulePollPreview())
	}
	return cmd
}

// handleGlobalKey handles keys when no overlay or modal has focus.
func (m Model) handleGlobalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.QuitKill):
		return m, func() tea.Msg {
			return ConfirmActionMsg{
				Message: "Quit and kill ALL sessions?",
				Action:  "quit-kill",
			}
		}

	case key.Matches(msg, m.keys.Help):
		m.PushView(ViewHelp)
		return m, nil

	case key.Matches(msg, m.keys.Settings):
		m.settings.Width = m.appState.TermWidth
		m.settings.Height = m.appState.TermHeight
		m.settings.Open(m.cfg)
		m.PushView(ViewSettings)
		return m, nil

	case key.Matches(msg, m.keys.TmuxHelp):
		m.PushView(ViewTmuxHelp)
		return m, nil

	case key.Matches(msg, m.keys.FocusToggle):
		if m.appState.FocusedPane == state.PaneSidebar {
			m.appState.FocusedPane = state.PanePreview
		} else {
			m.appState.FocusedPane = state.PaneSidebar
		}
		return m, nil

	case key.Matches(msg, m.keys.Filter):
		m.appState.FilterQuery = ""
		m.PushView(ViewFilter)
		return m, nil

	case key.Matches(msg, m.keys.GridOverview):
		m.openGrid(state.GridRestoreProject)
		return m, m.scheduleGridPoll()

	case msg.String() == "G":
		m.openGrid(state.GridRestoreAll)
		return m, m.scheduleGridPoll()

	case key.Matches(msg, m.keys.NewProject):
		m.nameInput.Placeholder = "my-project"
		m.nameInput.Reset()
		m.PushView(ViewProjectName)
		blinkCmd := m.nameInput.Focus()
		return m, blinkCmd

	case key.Matches(msg, m.keys.NewSession):
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

	case key.Matches(msg, m.keys.NewWorktreeSession):
		sel := m.sidebar.Selected()
		if sel == nil {
			return m, nil
		}
		pid := sel.ProjectID
		if pid == "" {
			return m, nil
		}
		// Verify this project is a git repo before proceeding.
		projDir := ""
		if proj := state.FindProject(&m.appState, pid); proj != nil {
			projDir = proj.Directory
		}
		if projDir == "" {
			projDir, _ = os.Getwd()
		}
		if !git.IsGitRepo(projDir) {
			return m, func() tea.Msg {
				return ErrorMsg{Err: fmt.Errorf("project directory is not a git repository")}
			}
		}
		m.pendingProjectID = pid
		m.pendingWorktree = true
		m.inputMode = "new-session"
		m.agentPicker.Show(m.sortedAgentItems())
		m.PushView(ViewAgentPicker)
		return m, nil

	case key.Matches(msg, m.keys.NewTeam):
		sel := m.sidebar.Selected()
		if sel == nil {
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

	case key.Matches(msg, m.keys.Attach):
		if !m.cfg.HideAttachHint {
			attach := m.pendingAttachDetails()
			if attach != nil {
				m.pendingAttach = attach
				m.PushView(ViewAttachHint)
				return m, nil
			}
		}
		return m, m.attachActiveSession()

	case key.Matches(msg, m.keys.Rename):
		return m, m.startRename()

	case key.Matches(msg, m.keys.KillSession):
		sel := m.sidebar.Selected()
		if sel != nil && sel.Kind == components.KindProject {
			return m, func() tea.Msg {
				return ConfirmActionMsg{
					Message: fmt.Sprintf("Kill project %q and all its sessions?", sel.Label),
					Action:  "kill-project:" + sel.ProjectID,
				}
			}
		}
		if sel != nil && sel.SessionID != "" {
			return m, func() tea.Msg {
				return ConfirmActionMsg{
					Message: fmt.Sprintf("Kill session %q?", sel.Label),
					Action:  "kill-session:" + sel.SessionID,
				}
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.KillTeam):
		sel := m.sidebar.Selected()
		if sel != nil && sel.TeamID != "" {
			teamName := m.teamNameByID(sel.TeamID)
			return m, func() tea.Msg {
				return ConfirmActionMsg{
					Message: fmt.Sprintf("Kill team %q and all its sessions?", teamName),
					Action:  "kill-team:" + sel.TeamID,
				}
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.ToggleCollapse):
		sel := m.sidebar.Selected()
		if sel != nil {
			if sel.Kind == components.KindProject {
				m.appState = *state.ToggleProjectCollapsed(&m.appState, sel.ProjectID)
			} else if sel.Kind == components.KindTeam {
				m.appState = *state.ToggleTeamCollapsed(&m.appState, sel.TeamID)
			}
			m.sidebar.Rebuild(&m.appState)
		}
		return m, nil

	case key.Matches(msg, m.keys.CollapseItem):
		sel := m.sidebar.Selected()
		if sel == nil {
			return m, nil
		}
		if sel.Kind == components.KindSession {
			// Left on a session collapses the immediate parent (team or project).
			if sel.TeamID != "" {
				m.appState = *state.ToggleTeamCollapsed(&m.appState, sel.TeamID)
			} else {
				m.appState = *state.ToggleProjectCollapsed(&m.appState, sel.ProjectID)
			}
			m.sidebar.Rebuild(&m.appState)
			// Move cursor to the parent item.
			for i, item := range m.sidebar.Items {
				if sel.TeamID != "" && item.Kind == components.KindTeam && item.TeamID == sel.TeamID {
					m.sidebar.Cursor = i
					break
				} else if sel.TeamID == "" && item.Kind == components.KindProject && item.ProjectID == sel.ProjectID {
					m.sidebar.Cursor = i
					break
				}
			}
		} else if !sel.Collapsed {
			if sel.Kind == components.KindProject {
				m.appState = *state.ToggleProjectCollapsed(&m.appState, sel.ProjectID)
				m.sidebar.Rebuild(&m.appState)
			} else if sel.Kind == components.KindTeam {
				m.appState = *state.ToggleTeamCollapsed(&m.appState, sel.TeamID)
				m.sidebar.Rebuild(&m.appState)
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.ExpandItem):
		sel := m.sidebar.Selected()
		if sel != nil && sel.Collapsed {
			if sel.Kind == components.KindProject {
				m.appState = *state.ToggleProjectCollapsed(&m.appState, sel.ProjectID)
				m.sidebar.Rebuild(&m.appState)
			} else if sel.Kind == components.KindTeam {
				m.appState = *state.ToggleTeamCollapsed(&m.appState, sel.TeamID)
				m.sidebar.Rebuild(&m.appState)
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.NavUp):
		prev := m.sidebar.Cursor
		prevSession := m.appState.ActiveSessionID
		m.sidebar.MoveUp()
		debugLog.Printf("NavUp: cursor %d->%d activeSession=%s", prev, m.sidebar.Cursor, m.appState.ActiveSessionID)
		if m.sidebar.Cursor != prev {
			m.syncActiveFromSidebar()
			if m.appState.ActiveSessionID != prevSession {
				m.previewPollGen++ // switched to a different session, start fresh poll
				return m, m.schedulePollPreview()
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.NavDown):
		prev := m.sidebar.Cursor
		prevSession := m.appState.ActiveSessionID
		m.sidebar.MoveDown()
		debugLog.Printf("NavDown: cursor %d->%d activeSession=%s", prev, m.sidebar.Cursor, m.appState.ActiveSessionID)
		if m.sidebar.Cursor != prev {
			m.syncActiveFromSidebar()
			if m.appState.ActiveSessionID != prevSession {
				m.previewPollGen++ // switched to a different session, start fresh poll
				return m, m.schedulePollPreview()
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.NavProjectUp):
		prev := m.sidebar.Cursor
		prevSession := m.appState.ActiveSessionID
		m.sidebar.JumpPrevProject()
		debugLog.Printf("NavProjectUp: cursor %d->%d", prev, m.sidebar.Cursor)
		if m.sidebar.Cursor != prev {
			m.syncActiveFromSidebar()
			if m.appState.ActiveSessionID != prevSession {
				m.previewPollGen++
				return m, m.schedulePollPreview()
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.NavProjectDown):
		prev := m.sidebar.Cursor
		prevSession := m.appState.ActiveSessionID
		m.sidebar.JumpNextProject()
		debugLog.Printf("NavProjectDown: cursor %d->%d", prev, m.sidebar.Cursor)
		if m.sidebar.Cursor != prev {
			m.syncActiveFromSidebar()
			if m.appState.ActiveSessionID != prevSession {
				m.previewPollGen++
				return m, m.schedulePollPreview()
			}
		}
		return m, nil

	// Jump to project by number
	case msg.String() >= "1" && msg.String() <= "9":
		idx := int(msg.String()[0]-'0') - 1
		count := 0
		for i, item := range m.sidebar.Items {
			if item.Kind == components.KindProject {
				if count == idx {
					m.sidebar.Cursor = i
					m.syncActiveFromSidebar()
					break
				}
				count++
			}
		}
		return m, nil
	}
	return m, nil
}

// handleMouse routes mouse press and scroll-wheel events to the appropriate
// component. Motion and release events are silently ignored.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Only act on press events and wheel scrolls.
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}

	// Settings screen: ignore mouse (keyboard-only).
	if m.TopView() == ViewSettings {
		return m, nil
	}

	// Grid view: cell selection and attach.
	if m.TopView() == ViewGrid {
		m.gridView.Width = m.appState.TermWidth
		m.gridView.Height = m.appState.TermHeight
		switch msg.Button {
		case tea.MouseButtonLeft:
			if idx, ok := m.gridView.CellAt(msg.X, msg.Y); ok {
				m.gridView.Cursor = idx
				// Clicking a grid cell activates (attaches) that session.
				if sess := m.gridView.Selected(); sess != nil {
					m.PopView() // pops ViewGrid
					s := sess
					return m, func() tea.Msg {
						return components.GridSessionSelectedMsg{
							TmuxSession: s.TmuxSession,
							TmuxWindow:  s.TmuxWindow,
						}
					}
				}
			}
		case tea.MouseButtonWheelUp:
			m.gridView.MoveUp()
		case tea.MouseButtonWheelDown:
			m.gridView.MoveDown()
		}
		return m, nil
	}

	// Ignore mouse when any modal overlay is active.
	if m.TopView() != ViewMain && m.TopView() != ViewFilter {
		return m, nil
	}

	sw, _, _ := computeLayout(m.appState.TermWidth, m.appState.TermHeight)
	inSidebar := msg.X < sw

	switch msg.Button {
	case tea.MouseButtonLeft:
		if inSidebar {
			return m.handleSidebarClick(msg.Y)
		}
		// Click in preview area: attach the active session (same as pressing 'a').
		if !m.cfg.HideAttachHint {
			attach := m.pendingAttachDetails()
			if attach != nil {
				m.pendingAttach = attach
				m.PushView(ViewAttachHint)
				return m, nil
			}
		}
		return m, m.attachActiveSession()

	case tea.MouseButtonWheelUp:
		if inSidebar {
			prev := m.sidebar.Cursor
			prevSession := m.appState.ActiveSessionID
			m.sidebar.MoveUp()
			if m.sidebar.Cursor != prev {
				m.syncActiveFromSidebar()
				if m.appState.ActiveSessionID != prevSession {
					m.previewPollGen++
					return m, m.schedulePollPreview()
				}
			}
		} else {
			m.preview.ScrollUp(3)
		}

	case tea.MouseButtonWheelDown:
		if inSidebar {
			prev := m.sidebar.Cursor
			prevSession := m.appState.ActiveSessionID
			m.sidebar.MoveDown()
			if m.sidebar.Cursor != prev {
				m.syncActiveFromSidebar()
				if m.appState.ActiveSessionID != prevSession {
					m.previewPollGen++
					return m, m.schedulePollPreview()
				}
			}
		} else {
			m.preview.ScrollDown(3)
		}
	}
	return m, nil
}

// handleSidebarClick processes a left-click at sidebar row y.
func (m Model) handleSidebarClick(y int) (tea.Model, tea.Cmd) {
	idx := m.sidebar.ItemAtRow(y)
	if idx < 0 {
		return m, nil
	}
	prev := m.sidebar.Cursor
	prevSession := m.appState.ActiveSessionID
	m.sidebar.Cursor = idx
	m.sidebar.EnsureCursorVisible(m.sidebar.Height)
	sel := m.sidebar.Selected()
	if sel == nil {
		return m, nil
	}
	switch sel.Kind {
	case components.KindProject:
		m.appState = *state.ToggleProjectCollapsed(&m.appState, sel.ProjectID)
		m.commitState()
		return m, nil
	case components.KindTeam:
		m.appState = *state.ToggleTeamCollapsed(&m.appState, sel.TeamID)
		m.commitState()
		return m, nil
	case components.KindSession:
		if m.sidebar.Cursor != prev {
			m.syncActiveFromSidebar()
			if m.appState.ActiveSessionID != prevSession {
				m.previewPollGen++
				return m, m.schedulePollPreview()
			}
		}
	}
	return m, nil
}

// handleAttachHint handles key input while the attach hint overlay is shown.
func (m Model) handleAttachHint(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y", " ":
		m.PopView()
		attach := m.pendingAttach
		m.pendingAttach = nil
		if attach == nil {
			return m, nil
		}
		cmd := m.doAttach(*attach)
		return m, cmd
	case "d":
		// Don't show again: save to config.
		m.PopView()
		m.cfg.HideAttachHint = true
		_ = config.Save(m.cfg)
		attach := m.pendingAttach
		m.pendingAttach = nil
		if attach == nil {
			return m, nil
		}
		cmd := m.doAttach(*attach)
		return m, cmd
	case "esc", "q":
		m.PopView()
		m.pendingAttach = nil
	}
	return m, nil
}
