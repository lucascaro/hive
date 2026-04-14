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
		if msg.String() == "esc" || key.Matches(msg, m.keys.Help) {
			m.PopView()
		}
		return m, nil
	case ViewWhatsNew:
		return m.handleWhatsNew(msg)
	case ViewGridInputHint:
		return m.handleGridInputHint(msg)
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

	// Input mode: all keys (except ctrl+c which always quits) are forwarded to
	// the focused session. Ctrl+Q exits input mode (handled inside GridView.Update).
	if m.gridView.InputMode() {
		cmd, _ := m.gridView.Update(msg)
		return cmd
	}

	// Actions that work consistently across sidebar and grid.
	if key.Matches(msg, m.keys.Quit) {
		return tea.Quit
	}
	if key.Matches(msg, m.keys.SidebarView) {
		return m.closeGrid()
	}
	if key.Matches(msg, m.keys.Help) {
		m.PushView(ViewHelp)
		return nil
	}
	if key.Matches(msg, m.keys.TmuxHelp) {
		m.PushView(ViewTmuxHelp)
		return nil
	}
	if key.Matches(msg, m.keys.Settings) {
		m.settings.Width = m.appState.TermWidth
		m.settings.Height = m.appState.TermHeight
		m.settings.Open(m.cfg)
		m.PushView(ViewSettings)
		return nil
	}

	// Grid uses Shift+Left/Right for reorder (horizontal layout is more natural).
	// Also accept Shift+Up/Down as aliases for consistency.
	if key.Matches(msg, m.keys.MoveLeft) || key.Matches(msg, m.keys.MoveUp) {
		if sess := m.gridView.Selected(); sess != nil {
			if _, changed := state.MoveSessionUp(&m.appState, sess.ID); changed {
				m.commitState()
				m.syncGridState(sess.ID)
			}
		}
		return nil
	}
	if key.Matches(msg, m.keys.MoveRight) || key.Matches(msg, m.keys.MoveDown) {
		if sess := m.gridView.Selected(); sess != nil {
			if _, changed := state.MoveSessionDown(&m.appState, sess.ID); changed {
				m.commitState()
				m.syncGridState(sess.ID)
			}
		}
		return nil
	}

	switch msg.String() {
	case "g":
		if m.gridView.Mode == state.GridRestoreAll {
			// All-grid → switch to project grid.
			prevID := ""
			if s := m.gridView.Selected(); s != nil {
				prevID = s.ID
				// Sync active state to the selected session's project before
				// filtering — otherwise gridSessions(GridRestoreProject) uses
				// the stale ActiveProjectID and drops the session we want.
				m.appState.ActiveSessionID = s.ID
				m.appState.ActiveProjectID = s.ProjectID
				m.appState.ActiveTeamID = s.TeamID
			}
			m.gridView.SyncState(m.gridSessions(state.GridRestoreProject), state.GridRestoreProject, m.gridProjectNames(), m.gridProjectColors(), m.gridSessionColors(), prevID)
			return m.scheduleGridPoll()
		}
		// Already in project grid — close grid and return to main.
		return m.closeGrid()
	case "G":
		if m.gridView.Mode == state.GridRestoreAll {
			// Already in all-grid — close grid and return to main.
			return m.closeGrid()
		}
		// Project grid → switch to all-grid.
		prevID := ""
		if s := m.gridView.Selected(); s != nil {
			prevID = s.ID
			// Keep active state in sync with the selected session so grid
			// exit (popGridState) lands on the right project.
			m.appState.ActiveSessionID = s.ID
			m.appState.ActiveProjectID = s.ProjectID
			m.appState.ActiveTeamID = s.TeamID
		}
		m.gridView.SyncState(m.gridSessions(state.GridRestoreAll), state.GridRestoreAll, m.gridProjectNames(), m.gridProjectColors(), m.gridSessionColors(), prevID)
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
			m.focusSession(sess.ID)
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
	case "c", "C":
		if sess := m.gridView.Selected(); sess != nil && sess.ProjectID != "" {
			dir := +1
			if msg.String() == "C" {
				dir = -1
			}
			m.cycleProjectColor(sess.ProjectID, dir)
			m.gridView.SetProjectColors(m.gridProjectColors())
		}
		return nil
	case "v", "V":
		if sess := m.gridView.Selected(); sess != nil {
			dir := +1
			if msg.String() == "V" {
				dir = -1
			}
			m.cycleSessionColor(sess.ID, dir)
			m.gridView.SetSessionColors(m.gridSessionColors())
		}
		return nil
	case "W":
		if sess := m.gridView.Selected(); sess != nil && sess.ProjectID != "" {
			if cmd := m.initWorktreeSession(sess.ProjectID); cmd != nil {
				return cmd
			}
			return nil
		}
	}
	prevSel := m.gridView.Selected()
	prevInputMode := m.gridView.InputMode()
	// Remaining keys (including h/l) are delegated to the grid component.
	// CollapseItem/ExpandItem (h/l) are intentionally not wired here — in grid
	// mode h/l navigate the cursor left/right, which is the expected behavior.
	cmd, _ := m.gridView.Update(msg)
	// gridView.Update may set Active=false (esc/q/enter). Detect that and
	// pop the grid from the stack, syncing state to the selected session.
	if !m.gridView.Active && prevSel != nil {
		m.popGridState(prevSel)
		return tea.Batch(cmd, m.schedulePollPreview())
	}
	// If input mode was just activated, kick off the fast focused-session poll
	// (50 ms) so the user sees output quickly. The background poll continues
	// at 250 ms from the existing loop. Also show the first-use hint if not
	// suppressed.
	if !prevInputMode && m.gridView.InputMode() {
		if !m.cfg.HideGridInputHint {
			m.PushView(ViewGridInputHint)
		}
		return tea.Batch(cmd, m.scheduleFocusedSessionPoll())
	}
	return cmd
}

// closeGrid hides the grid, syncs state to the selected session, and pops the view.
func (m *Model) closeGrid() tea.Cmd {
	sel := m.gridView.Selected()
	m.gridView.Hide()
	if sel != nil {
		m.popGridState(sel)
	} else {
		m.PopView()
	}
	m.previewPollGen++
	return m.schedulePollPreview()
}

// popGridState pops the grid from the view stack and syncs the selected session.
func (m *Model) popGridState(sel *state.Session) {
	m.PopView()
	m.focusSession(sel.ID)
	m.previewPollGen++
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

	case key.Matches(msg, m.keys.SidebarView):
		m.appState.FocusedPane = state.PaneSidebar
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
		if cmd := m.initWorktreeSession(pid); cmd != nil {
			return m, cmd
		}
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

	case key.Matches(msg, m.keys.ColorNext), key.Matches(msg, m.keys.ColorPrev):
		sel := m.sidebar.Selected()
		if sel == nil {
			return m, nil
		}
		projectID := sel.ProjectID
		if projectID == "" {
			return m, nil
		}
		dir := +1
		if key.Matches(msg, m.keys.ColorPrev) {
			dir = -1
		}
		m.cycleProjectColor(projectID, dir)
		return m, nil

	case msg.String() == "v", msg.String() == "V":
		sel := m.sidebar.Selected()
		if sel == nil || sel.SessionID == "" {
			return m, nil
		}
		dir := +1
		if msg.String() == "V" {
			dir = -1
		}
		m.cycleSessionColor(sel.SessionID, dir)
		return m, nil

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
		return m.navigateSidebar((*components.Sidebar).MoveUp)

	case key.Matches(msg, m.keys.NavDown):
		return m.navigateSidebar((*components.Sidebar).MoveDown)

	case key.Matches(msg, m.keys.NavProjectUp):
		return m.navigateSidebar((*components.Sidebar).JumpPrevProject)

	case key.Matches(msg, m.keys.NavProjectDown):
		return m.navigateSidebar((*components.Sidebar).JumpNextProject)

	case key.Matches(msg, m.keys.MoveUp):
		return m.moveItem(-1)

	case key.Matches(msg, m.keys.MoveDown):
		return m.moveItem(+1)

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

// moveItem moves the currently selected sidebar item up (dir=-1) or down (dir=+1)
// within its group. Dispatches to the appropriate state reducer based on item kind.
// Skips persist and rebuild when nothing moved (boundary no-ops).
func (m Model) moveItem(dir int) (tea.Model, tea.Cmd) {
	sel := m.sidebar.Selected()
	if sel == nil {
		return m, nil
	}
	var changed bool
	switch sel.Kind {
	case components.KindSession:
		if dir < 0 {
			_, changed = state.MoveSessionUp(&m.appState, sel.SessionID)
		} else {
			_, changed = state.MoveSessionDown(&m.appState, sel.SessionID)
		}
	case components.KindTeam:
		if dir < 0 {
			_, changed = state.MoveTeamUp(&m.appState, sel.TeamID)
		} else {
			_, changed = state.MoveTeamDown(&m.appState, sel.TeamID)
		}
	case components.KindProject:
		if dir < 0 {
			_, changed = state.MoveProjectUp(&m.appState, sel.ProjectID)
		} else {
			_, changed = state.MoveProjectDown(&m.appState, sel.ProjectID)
		}
	default:
		return m, nil
	}
	if !changed {
		return m, nil
	}
	m.commitState() // also rebuilds sidebar
	// Re-sync cursor to the moved item.
	switch sel.Kind {
	case components.KindSession:
		m.sidebar.SyncActiveSession(sel.SessionID)
	case components.KindTeam:
		for i, item := range m.sidebar.Items {
			if item.Kind == components.KindTeam && item.TeamID == sel.TeamID {
				m.sidebar.Cursor = i
				break
			}
		}
	case components.KindProject:
		for i, item := range m.sidebar.Items {
			if item.Kind == components.KindProject && item.ProjectID == sel.ProjectID {
				m.sidebar.Cursor = i
				break
			}
		}
	}
	m.sidebar.EnsureCursorVisible(m.sidebar.Height)
	return m, nil
}

// initWorktreeSession verifies the project is a git repo and opens the agent
// picker in worktree mode. Returns an error tea.Cmd if the project is not a
// git repo; returns nil on success (agent picker is now open, caller should
// return nil to its own caller).
func (m *Model) initWorktreeSession(projectID string) tea.Cmd {
	projDir := ""
	if proj := state.FindProject(&m.appState, projectID); proj != nil {
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
	m.pendingProjectID = projectID
	m.pendingWorktree = true
	m.inputMode = "new-session"
	m.agentPicker.Show(m.sortedAgentItems())
	m.PushView(ViewAgentPicker)
	return nil
}

// navigateSidebar calls moveFn on the Model's own sidebar to move the cursor,
// then syncs active session state and starts a preview poll if the session changed.
// moveFn receives a pointer to the sidebar so it operates on the correct copy.
func (m Model) navigateSidebar(moveFn func(*components.Sidebar)) (tea.Model, tea.Cmd) {
	prev := m.sidebar.Cursor
	prevSession := m.appState.ActiveSessionID
	moveFn(&m.sidebar)
	if m.sidebar.Cursor != prev {
		m.syncActiveFromSidebar()
		if m.appState.ActiveSessionID != prevSession {
			m.previewPollGen++
			return m, m.schedulePollPreview()
		}
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

	// What's New overlay: scroll with mouse wheel.
	if m.TopView() == ViewWhatsNew {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.whatsNewViewport.LineUp(3)
		case tea.MouseButtonWheelDown:
			m.whatsNewViewport.LineDown(3)
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
			return m.navigateSidebar((*components.Sidebar).MoveUp)
		}
		m.preview.ScrollUp(3)

	case tea.MouseButtonWheelDown:
		if inSidebar {
			return m.navigateSidebar((*components.Sidebar).MoveDown)
		}
		m.preview.ScrollDown(3)
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

// handleWhatsNew handles key input while the What's New overlay is shown.
func (m Model) handleWhatsNew(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc", "q", " ":
		m.PopView()
	case "d":
		m.PopView()
		m.cfg.HideWhatsNew = true
		_ = config.Save(m.cfg)
	case "j", "down":
		m.whatsNewViewport.LineDown(1)
	case "k", "up":
		m.whatsNewViewport.LineUp(1)
	}
	return m, nil
}

func (m Model) handleGridInputHint(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y", " ":
		// Continue: dismiss overlay, stay in input mode.
		m.PopView()
	case "d":
		// Don't show again: dismiss and save preference.
		m.PopView()
		m.cfg.HideGridInputHint = true
		_ = config.Save(m.cfg)
	case "esc", "q":
		// Cancel: dismiss overlay and exit input mode.
		m.PopView()
		m.gridView.ExitInputMode()
	}
	return m, nil
}

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
