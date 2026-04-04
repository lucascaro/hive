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

// handleKey handles keyboard events. It uses a priority-ordered list of
// KeyHandler adapters: the first focused handler exclusively receives the key
// event, preventing leakage to lower-priority handlers or global bindings.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ctrl+c always quits, regardless of focus.
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	if cmd, handled := dispatchKey(m.buildKeyHandlers(), msg); handled {
		return m, cmd
	}

	return m.handleGlobalKey(msg)
}

// buildKeyHandlers returns KeyHandler adapters in priority order. The first
// handler whose Focused() returns true wins exclusive key input. Adding a new
// modal is as simple as adding an entry here.
func (m *Model) buildKeyHandlers() []KeyHandler {
	return []KeyHandler{
		// Full-screen overlays
		componentHandler{
			focused: func() bool { return m.settings.Active },
			handle: func(msg tea.KeyMsg) tea.Cmd {
				cmd, _ := m.settings.Update(msg)
				return cmd
			},
		},
		componentHandler{
			focused: func() bool { return m.gridView.Active },
			handle:  func(msg tea.KeyMsg) tea.Cmd { return m.handleGridKey(msg) },
		},

		// Help overlays — esc closes, everything else is swallowed.
		componentHandler{
			focused: func() bool { return m.appState.ShowHelp || m.appState.ShowTmuxHelp },
			handle: func(msg tea.KeyMsg) tea.Cmd {
				if msg.String() == "esc" {
					m.appState.ShowHelp = false
					m.appState.ShowTmuxHelp = false
				}
				return nil
			},
		},

		// Modal dialogs and pickers
		componentHandler{
			focused: func() bool { return m.showAttachHint },
			handle: func(msg tea.KeyMsg) tea.Cmd {
				result, cmd := m.handleAttachHint(msg)
				*m = result.(Model)
				return cmd
			},
		},
		componentHandler{
			focused: func() bool { return m.appState.ShowConfirm },
			handle: func(msg tea.KeyMsg) tea.Cmd {
				result, cmd := m.handleConfirm(msg)
				*m = result.(Model)
				return cmd
			},
		},
		componentHandler{
			focused: func() bool { return m.recoveryPicker.Active },
			handle: func(msg tea.KeyMsg) tea.Cmd {
				updated, cmd := m.recoveryPicker.Update(msg)
				m.recoveryPicker = updated
				return cmd
			},
		},
		componentHandler{
			focused: func() bool { return m.orphanPicker.Active },
			handle: func(msg tea.KeyMsg) tea.Cmd {
				updated, cmd := m.orphanPicker.Update(msg)
				m.orphanPicker = updated
				return cmd
			},
		},
		componentHandler{
			focused: func() bool { return m.agentPicker.Active },
			handle: func(msg tea.KeyMsg) tea.Cmd {
				cmd, _ := m.agentPicker.Update(msg)
				return cmd
			},
		},
		componentHandler{
			focused: func() bool { return m.teamBuilder.Active },
			handle: func(msg tea.KeyMsg) tea.Cmd {
				return m.teamBuilder.Update(msg)
			},
		},

		// Inline editors and text inputs
		componentHandler{
			focused: func() bool { return m.appState.EditingTitle },
			handle: func(msg tea.KeyMsg) tea.Cmd {
				result, cmd := m.handleTitleEdit(msg)
				*m = result.(Model)
				return cmd
			},
		},
		componentHandler{
			focused: func() bool { return m.inputMode == "project-name" },
			handle: func(msg tea.KeyMsg) tea.Cmd {
				result, cmd := m.handleNameInput(msg)
				*m = result.(Model)
				return cmd
			},
		},
		componentHandler{
			focused: func() bool { return m.dirPicker.Active },
			handle: func(msg tea.KeyMsg) tea.Cmd {
				cmd, _ := m.dirPicker.Update(msg)
				return cmd
			},
		},
		componentHandler{
			focused: func() bool { return m.inputMode == "project-dir-confirm" },
			handle: func(msg tea.KeyMsg) tea.Cmd {
				result, cmd := m.handleDirConfirm(msg)
				*m = result.(Model)
				return cmd
			},
		},
		componentHandler{
			focused: func() bool { return m.inputMode == "custom-command" },
			handle: func(msg tea.KeyMsg) tea.Cmd {
				result, cmd := m.handleCustomCommandInput(msg)
				*m = result.(Model)
				return cmd
			},
		},
		componentHandler{
			focused: func() bool { return m.inputMode == "worktree-branch" },
			handle: func(msg tea.KeyMsg) tea.Cmd {
				result, cmd := m.handleWorktreeBranchInput(msg)
				*m = result.(Model)
				return cmd
			},
		},
		componentHandler{
			focused: func() bool { return m.appState.FilterActive },
			handle: func(msg tea.KeyMsg) tea.Cmd {
				result, cmd := m.handleFilter(msg)
				*m = result.(Model)
				return cmd
			},
		},
	}
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
			m.gridView.Hide()
			s := sess
			return func() tea.Msg {
				return ConfirmActionMsg{
					Message: fmt.Sprintf("Kill session %q?", s.Title),
					Action:  "kill-session:" + s.ID,
				}
			}
		}
	case "r":
		if sess := m.gridView.Selected(); sess != nil {
			m.gridView.Hide()
			m.sidebar.SyncActiveSession(sess.ID)
			m.appState.ActiveSessionID = sess.ID
			return m.startRename()
		}
	}
	wasActive := m.gridView.Active
	prevSel := m.gridView.Selected()
	cmd, _ := m.gridView.Update(msg)
	if wasActive && !m.gridView.Active && prevSel != nil {
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
		m.appState.ShowHelp = !m.appState.ShowHelp
		m.appState.ShowTmuxHelp = false
		return m, nil

	case key.Matches(msg, m.keys.Settings):
		m.settings.Width = m.appState.TermWidth
		m.settings.Height = m.appState.TermHeight
		m.settings.Open(m.cfg)
		return m, nil

	case key.Matches(msg, m.keys.TmuxHelp):
		m.appState.ShowTmuxHelp = !m.appState.ShowTmuxHelp
		m.appState.ShowHelp = false
		return m, nil

	case key.Matches(msg, m.keys.FocusToggle):
		if m.appState.FocusedPane == state.PaneSidebar {
			m.appState.FocusedPane = state.PanePreview
		} else {
			m.appState.FocusedPane = state.PaneSidebar
		}
		return m, nil

	case key.Matches(msg, m.keys.Filter):
		m.appState.FilterActive = true
		m.appState.FilterQuery = ""
		return m, nil

	case key.Matches(msg, m.keys.GridOverview):
		sessions := m.gridSessions(state.GridRestoreProject)
		m.gridView.Show(sessions, state.GridRestoreProject)
		m.gridView.SetProjectNames(m.gridProjectNames())
		m.gridView.SyncCursor(m.appState.ActiveSessionID)
		return m, m.scheduleGridPoll()

	case msg.String() == "G":
		m.gridView.Show(m.gridSessions(state.GridRestoreAll), state.GridRestoreAll)
		m.gridView.SetProjectNames(m.gridProjectNames())
		m.gridView.SyncCursor(m.appState.ActiveSessionID)
		return m, m.scheduleGridPoll()

	case key.Matches(msg, m.keys.NewProject):
		m.inputMode = "project-name"
		m.nameInput.Placeholder = "my-project"
		m.nameInput.Reset()
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
		return m, nil

	case key.Matches(msg, m.keys.Attach):
		if !m.cfg.HideAttachHint {
			attach := m.pendingAttachDetails()
			if attach != nil {
				m.pendingAttach = attach
				m.showAttachHint = true
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
	if m.settings.Active {
		return m, nil
	}

	// Grid view: cell selection and attach.
	if m.gridView.Active {
		m.gridView.Width = m.appState.TermWidth
		m.gridView.Height = m.appState.TermHeight
		switch msg.Button {
		case tea.MouseButtonLeft:
			if idx, ok := m.gridView.CellAt(msg.X, msg.Y); ok {
				m.gridView.Cursor = idx
				// Clicking a grid cell activates (attaches) that session.
				if sess := m.gridView.Selected(); sess != nil {
					m.gridView.Hide()
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
	if m.appState.ShowHelp || m.appState.ShowTmuxHelp || m.appState.ShowConfirm ||
		m.showAttachHint || m.recoveryPicker.Active || m.orphanPicker.Active || m.agentPicker.Active ||
		m.teamBuilder.Active || m.appState.EditingTitle ||
		m.inputMode != "" || m.dirPicker.Active {
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
				m.showAttachHint = true
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
		m.showAttachHint = false
		attach := m.pendingAttach
		m.pendingAttach = nil
		if attach == nil {
			return m, nil
		}
		cmd := m.doAttach(*attach)
		return m, cmd
	case "d":
		// Don't show again: save to config.
		m.showAttachHint = false
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
		m.showAttachHint = false
		m.pendingAttach = nil
	}
	return m, nil
}
