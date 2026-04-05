package tui

import "github.com/lucascaro/hive/internal/state"

// ViewID identifies a view layer in the view stack.
type ViewID string

const (
	ViewMain           ViewID = "main"
	ViewSettings       ViewID = "settings"
	ViewGrid           ViewID = "grid"
	ViewHelp           ViewID = "help"
	ViewTmuxHelp       ViewID = "tmux-help"
	ViewAttachHint     ViewID = "attach-hint"
	ViewConfirm        ViewID = "confirm"
	ViewRecovery       ViewID = "recovery"
	ViewOrphan         ViewID = "orphan"
	ViewAgentPicker    ViewID = "agent-picker"
	ViewTeamBuilder    ViewID = "team-builder"
	ViewRename         ViewID = "rename"
	ViewProjectName    ViewID = "project-name"
	ViewDirPicker      ViewID = "dir-picker"
	ViewDirConfirm     ViewID = "dir-confirm"
	ViewCustomCmd      ViewID = "custom-command"
	ViewWorktreeBranch ViewID = "worktree-branch"
	ViewFilter         ViewID = "filter"
)

// PushView pushes a view onto the stack and syncs legacy flags.
func (m *Model) PushView(id ViewID) {
	if debugLog != nil {
		for _, v := range m.viewStack {
			if v == id {
				debugLog.Printf("WARNING: PushView(%s) duplicates existing stack entry", id)
				break
			}
		}
	}
	m.viewStack = append(m.viewStack, id)
	m.syncLegacyFlags(id, true)
}

// PopView removes the top view from the stack, clears its legacy flags,
// and returns the popped ViewID. Never pops below ViewMain.
func (m *Model) PopView() ViewID {
	if len(m.viewStack) <= 1 {
		return ViewMain
	}
	top := m.viewStack[len(m.viewStack)-1]
	m.viewStack = m.viewStack[:len(m.viewStack)-1]
	m.syncLegacyFlags(top, false)
	return top
}

// TopView returns the view at the top of the stack.
func (m *Model) TopView() ViewID {
	if len(m.viewStack) == 0 {
		return ViewMain
	}
	return m.viewStack[len(m.viewStack)-1]
}

// HasView returns true if the given view is anywhere in the stack.
func (m *Model) HasView(id ViewID) bool {
	for _, v := range m.viewStack {
		if v == id {
			return true
		}
	}
	return false
}

// ReplaceTop replaces the top view with a new one, clearing the old
// view's flags and setting the new view's flags. If the stack only
// has ViewMain, it pushes instead.
func (m *Model) ReplaceTop(id ViewID) {
	if len(m.viewStack) <= 1 {
		m.PushView(id)
		return
	}
	old := m.viewStack[len(m.viewStack)-1]
	m.syncLegacyFlags(old, false)
	m.viewStack[len(m.viewStack)-1] = id
	m.syncLegacyFlags(id, true)
}

// syncLegacyFlags sets or clears the legacy boolean flags and Active fields
// to keep backward compat with component code that reads them directly.
func (m *Model) syncLegacyFlags(id ViewID, active bool) {
	switch id {
	case ViewSettings:
		m.settings.Active = active
	case ViewGrid:
		// Grid data (sessions, cursor) is managed by Show/Hide; Active is
		// the only field we need to sync here. The caller handles Show/Hide.
		m.gridView.Active = active
	case ViewHelp:
		m.appState.ShowHelp = active
	case ViewTmuxHelp:
		m.appState.ShowTmuxHelp = active
	case ViewAttachHint:
		m.showAttachHint = active
	case ViewConfirm:
		m.appState.ShowConfirm = active
	case ViewRecovery:
		m.recoveryPicker.Active = active
	case ViewOrphan:
		m.orphanPicker.Active = active
	case ViewAgentPicker:
		m.agentPicker.Active = active
		if !active {
			m.agentPicker.Hide()
		}
	case ViewTeamBuilder:
		m.teamBuilder.Active = active
		if !active {
			m.teamBuilder.Hide()
		}
	case ViewRename:
		m.appState.EditingTitle = active
	case ViewProjectName:
		if active {
			m.inputMode = "project-name"
		} else {
			m.inputMode = ""
		}
	case ViewDirPicker:
		m.dirPicker.Active = active
	case ViewDirConfirm:
		if active {
			m.inputMode = "project-dir-confirm"
		} else {
			m.inputMode = ""
		}
	case ViewCustomCmd:
		if active {
			m.inputMode = "custom-command"
		} else {
			m.inputMode = ""
		}
	case ViewWorktreeBranch:
		if active {
			m.inputMode = "worktree-branch"
		} else {
			m.inputMode = ""
		}
	case ViewFilter:
		m.appState.FilterActive = active
	}
}

// refreshGrid refreshes the grid's session list and project names if the
// grid is in the view stack. Preserves cursor position.
func (m *Model) refreshGrid() {
	if !m.HasView(ViewGrid) {
		return
	}
	prevID := ""
	if s := m.gridView.Selected(); s != nil {
		prevID = s.ID
	}
	m.gridView.Show(m.gridSessions(m.gridView.Mode), m.gridView.Mode)
	m.gridView.SetProjectNames(m.gridProjectNames())
	if prevID != "" {
		m.gridView.SyncCursor(prevID)
	}
}

// openGrid is a helper that pushes the grid view and sets up grid state.
func (m *Model) openGrid(mode state.GridRestoreMode) {
	sessions := m.gridSessions(mode)
	m.gridView.Show(sessions, mode)
	m.gridView.SetProjectNames(m.gridProjectNames())
	m.gridView.SyncCursor(m.appState.ActiveSessionID)
	m.PushView(ViewGrid)
}
