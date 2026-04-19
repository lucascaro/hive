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
	case ViewHelp:
		if key.Matches(msg, m.keys.Dismiss) ||
			key.Matches(msg, m.keys.Help) ||
			key.Matches(msg, m.keys.Quit) {
			m.PopView()
			return m, nil
		}
		m.helpPanel.Width = m.appState.TermWidth
		m.helpPanel.Height = m.appState.TermHeight
		m.helpPanel.Update(msg, m.keys)
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
	case ViewPalette:
		cmd, _ := m.palette.Update(msg)
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
	// the focused session. The configured Detach key exits input mode
	// (handled inside GridView.Update via gv.Keys.Detach).
	if m.gridView.InputMode() {
		cmd, _ := m.gridView.Update(msg)
		return cmd
	}

	// SidebarView in grid has grid-specific semantics (close grid), so it stays
	// inline and runs before the registry — registry's cmdFocusSidebar is
	// scoped to Global and wouldn't fire here anyway, but this makes the
	// grid-specific behavior explicit.
	if key.Matches(msg, m.keys.SidebarView) {
		return m.closeGrid()
	}
	// Palette opener is the registry's UI, not a registry entry.
	if key.Matches(msg, m.keys.Palette) {
		m.palette.Show(m.paletteItems())
		m.PushView(ViewPalette)
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

	// Grid mode toggles are a state machine (project-grid ↔ all-grid ↔ closed)
	// that doesn't fit the registry's "act on activeTarget" shape — kept inline.
	switch {
	case key.Matches(msg, m.keys.GridOverview):
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
			m.polling.Invalidate()
			return m.scheduleGridPoll()
		}
		// Already in project grid — close grid and return to main.
		return m.closeGrid()
	case key.Matches(msg, m.keys.ToggleAll):
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
		m.polling.Invalidate()
		return m.scheduleGridPoll()
	}

	// Delegate action keys (kill, rename, new session/worktree, color cycling,
	// help/settings/quit) to the command registry — same executors as sidebar
	// and palette, routed via activeTarget().
	if nm, cmd, ok := Model(*m).dispatchCommand(msg, ScopeGrid); ok {
		*m = nm.(Model)
		return cmd
	}
	prevSel := m.gridView.Selected()
	prevInputMode := m.gridView.InputMode()
	// Remaining keys (including h/l) are delegated to the grid component.
	// CollapseItem/ExpandItem (h/l) are intentionally not wired here — in grid
	// mode h/l navigate the cursor left/right, which is the expected behavior.
	cmd, _ := m.gridView.Update(msg)
	// gridView.Update may set Active=false (esc/q). Detect that and
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
	m.polling.Invalidate()
	return m.schedulePollPreview()
}

// popGridState pops the grid from the view stack and syncs the selected session.
func (m *Model) popGridState(sel *state.Session) {
	m.PopView()
	m.focusSession(sel.ID)
	m.polling.Invalidate()
}

// handleGlobalKey handles keys when no overlay or modal has focus. Action
// keybindings (new session, kill, attach, colors, quit, etc.) go through the
// command registry so the palette and direct-key paths stay in sync. Pure
// navigation and selection keys (nav, move, collapse, jump-to-project) remain
// inline — they operate on the sidebar rather than on a Target.
func (m Model) handleGlobalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if nm, cmd, ok := m.dispatchCommand(msg, ScopeGlobal); ok {
		return nm, cmd
	}

	// Palette opener is not a registry command — it's the registry's UI.
	if key.Matches(msg, m.keys.Palette) {
		m.palette.Show(m.paletteItems())
		m.PushView(ViewPalette)
		return m, nil
	}

	switch {
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

	// Jump to project by position in the configured JumpToProject keys.
	// The user's first key jumps to project 1, second to project 2, etc. —
	// so non-digit custom bindings work without silently requiring digits.
	case key.Matches(msg, m.keys.JumpToProject):
		idx := -1
		for i, k := range m.keys.JumpToProject.Keys() {
			if msg.String() == k {
				idx = i
				break
			}
		}
		if idx < 0 {
			return m, nil
		}
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
			m.polling.Invalidate()
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

	sw, _, _ := computeLayout(m.appState.TermWidth, m.appState.TermHeight, components.PreviewActivityPanelHeight)
	inSidebar := msg.X < sw

	switch msg.Button {
	case tea.MouseButtonLeft:
		if inSidebar {
			return m.handleSidebarClick(msg.Y)
		}
		// Click in preview area: attach the active session (same as pressing Enter).
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
				m.polling.Invalidate()
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
