package tui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

// CommandScope flags which dispatcher contexts a Command applies to.
// Two scopes cover today's surface area; no per-view bitmask needed.
type CommandScope uint8

const (
	ScopeGlobal CommandScope = 1 << iota // handleGlobalKey (sidebar / main view)
	ScopeGrid                            // handleGridKey (non-input mode)
)

// Command is a single action that can be invoked by either a keybinding or a
// command palette selection. The registry below is the single source of truth
// — adding or changing an action means editing one entry, and both call paths
// pick it up automatically.
type Command struct {
	ID      string                                // stable identifier (used by palette and tests)
	Label   string                                // palette display label
	Binding func(KeyMap) key.Binding              // nil → palette-only (no direct key)
	Enabled func(m *Model) bool                   // nil → always enabled
	Exec    func(m *Model) (tea.Model, tea.Cmd)   // mutates *m, returns the updated Model
	Scopes  CommandScope
}

// commands is the full command registry. Order matters only for the palette
// list rendering — dispatch matches by binding, not by position.
var commands = []Command{
	// --- Session actions ---
	{ID: "attach", Label: "Attach to session",
		Binding: func(km KeyMap) key.Binding { return km.Attach },
		Enabled: targetIsSession,
		Exec:    cmdAttach,
		Scopes:  ScopeGlobal | ScopeGrid},
	{ID: "new-session", Label: "New session",
		Binding: func(km KeyMap) key.Binding { return km.NewSession },
		Enabled: targetHasProjectID,
		Exec:    cmdNewSession,
		Scopes:  ScopeGlobal | ScopeGrid},
	{ID: "new-worktree", Label: "New worktree session",
		Binding: func(km KeyMap) key.Binding { return km.NewWorktreeSession },
		Enabled: targetHasProjectID,
		Exec:    cmdNewWorktree,
		Scopes:  ScopeGlobal | ScopeGrid},
	{ID: "kill-session", Label: "Kill session",
		Binding: func(km KeyMap) key.Binding { return km.KillSession },
		Enabled: targetIsKillable,
		Exec:    cmdKillSession, // branches to kill-project when target is a project
		Scopes:  ScopeGlobal | ScopeGrid},
	{ID: "rename", Label: "Rename session",
		Binding: func(km KeyMap) key.Binding { return km.Rename },
		Enabled: targetIsSession,
		Exec:    cmdRename,
		Scopes:  ScopeGlobal | ScopeGrid},

	// --- Project & team actions ---
	{ID: "new-project", Label: "New project",
		Binding: func(km KeyMap) key.Binding { return km.NewProject },
		Exec:    cmdNewProject,
		Scopes:  ScopeGlobal | ScopeGrid},
	{ID: "new-team", Label: "New team",
		Binding: func(km KeyMap) key.Binding { return km.NewTeam },
		Enabled: targetHasProjectID,
		Exec:    cmdNewTeam,
		Scopes:  ScopeGlobal | ScopeGrid},
	{ID: "kill-team", Label: "Kill team",
		Binding: func(km KeyMap) key.Binding { return km.KillTeam },
		Enabled: targetIsTeam,
		Exec:    cmdKillTeam,
		Scopes:  ScopeGlobal | ScopeGrid},

	// --- View actions ---
	//
	// grid/grid-all are ScopeGlobal only: they open the grid, which is
	// meaningless when already inside it. Grid's own GridOverview/ToggleAll
	// keys handle project↔all toggling inline (see handleGridKey).
	// sidebar is ScopeGlobal only: in grid, SidebarView is handled inline as
	// "close grid" (different semantics).
	// filter is ScopeGlobal only as a UX choice — filter narrows the sidebar
	// list, which isn't visible from grid.
	{ID: "grid", Label: "Grid view (project)",
		Binding: func(km KeyMap) key.Binding { return km.GridOverview },
		Exec:    cmdOpenGrid,
		Scopes:  ScopeGlobal},
	{ID: "grid-all", Label: "Grid view (all)",
		Binding: func(km KeyMap) key.Binding { return km.ToggleAll },
		Exec:    cmdOpenGridAll,
		Scopes:  ScopeGlobal},
	{ID: "sidebar", Label: "Sidebar view",
		Binding: func(km KeyMap) key.Binding { return km.SidebarView },
		Exec:    cmdFocusSidebar,
		Scopes:  ScopeGlobal},
	{ID: "filter", Label: "Filter sessions",
		Binding: func(km KeyMap) key.Binding { return km.Filter },
		Exec:    cmdFilter,
		Scopes:  ScopeGlobal},

	// --- Appearance ---
	{ID: "color-next", Label: "Next project color",
		Binding: func(km KeyMap) key.Binding { return km.ColorNext },
		Enabled: targetHasProjectID,
		Exec:    cmdColorNext,
		Scopes:  ScopeGlobal | ScopeGrid},
	{ID: "color-prev", Label: "Previous project color",
		Binding: func(km KeyMap) key.Binding { return km.ColorPrev },
		Enabled: targetHasProjectID,
		Exec:    cmdColorPrev,
		Scopes:  ScopeGlobal | ScopeGrid},
	{ID: "session-color-next", Label: "Next session color",
		Binding: func(km KeyMap) key.Binding { return km.SessionColorNext },
		Enabled: targetIsSession,
		Exec:    cmdSessionColorNext,
		Scopes:  ScopeGlobal | ScopeGrid},
	{ID: "session-color-prev", Label: "Previous session color",
		Binding: func(km KeyMap) key.Binding { return km.SessionColorPrev },
		Enabled: targetIsSession,
		Exec:    cmdSessionColorPrev,
		Scopes:  ScopeGlobal | ScopeGrid},

	// --- Help & settings ---
	{ID: "help", Label: "Help",
		Binding: func(km KeyMap) key.Binding { return km.Help },
		Exec:    cmdHelp,
		Scopes:  ScopeGlobal | ScopeGrid},
	{ID: "tmux-help", Label: "Tmux shortcuts",
		Binding: func(km KeyMap) key.Binding { return km.TmuxHelp },
		Exec:    cmdTmuxHelp,
		Scopes:  ScopeGlobal | ScopeGrid},
	{ID: "settings", Label: "Settings",
		Binding: func(km KeyMap) key.Binding { return km.Settings },
		Exec:    cmdSettings,
		Scopes:  ScopeGlobal | ScopeGrid},

	// --- Quit ---
	{ID: "quit", Label: "Quit",
		Binding: func(km KeyMap) key.Binding { return km.Quit },
		Exec:    cmdQuit,
		Scopes:  ScopeGlobal | ScopeGrid},
	{ID: "quit-kill", Label: "Quit and kill all",
		Binding: func(km KeyMap) key.Binding { return km.QuitKill },
		Exec:    cmdQuitKill,
		Scopes:  ScopeGlobal | ScopeGrid},
}

// findCommand returns the registry entry with the given ID, or nil.
// Used by the command palette dispatcher to look up picked actions.
func findCommand(id string) *Command {
	for i := range commands {
		if commands[i].ID == id {
			return &commands[i]
		}
	}
	return nil
}

// dispatchCommand tries each registry entry whose scope matches. First binding
// hit fires its executor; the bool result tells callers whether to skip the
// inline switch below.
func (m Model) dispatchCommand(msg tea.KeyMsg, scope CommandScope) (tea.Model, tea.Cmd, bool) {
	for i := range commands {
		c := &commands[i]
		if c.Scopes&scope == 0 || c.Binding == nil {
			continue
		}
		if !key.Matches(msg, c.Binding(m.keys)) {
			continue
		}
		if c.Enabled != nil && !c.Enabled(&m) {
			continue
		}
		nm, cmd := c.Exec(&m)
		return nm, cmd, true
	}
	return m, nil, false
}

// --- Enabled predicates (all read m.activeTarget) ---

// targetHasProjectID is true when the selection carries a project ID (session,
// team, or project itself). Not the same as Kind == TargetProject.
func targetHasProjectID(m *Model) bool { return m.activeTarget().ProjectID != "" }

func targetIsSession(m *Model) bool { return m.activeTarget().Kind == TargetSession }
func targetIsTeam(m *Model) bool    { return m.activeTarget().Kind == TargetTeam }

// targetIsKillable covers both "kill session" (Kind == Session) and "kill
// project" (Kind == Project) — kill-session dispatches to the right confirm.
func targetIsKillable(m *Model) bool {
	k := m.activeTarget().Kind
	return k == TargetSession || k == TargetProject
}

// --- Executors ---
// Each mutates *m and returns (m, cmd). Selection comes from m.activeTarget.

func cmdAttach(m *Model) (tea.Model, tea.Cmd) {
	// Grid context attaches via the same GridSessionSelectedMsg path as the
	// direct Enter key, so grid-restore state (RestoreGridMode, ActiveProjectID)
	// is updated correctly on return from the attached session.
	if m.TopView() == ViewGrid {
		if sess := m.gridView.Selected(); sess != nil {
			s := sess
			return *m, func() tea.Msg {
				return components.GridSessionSelectedMsg{TmuxSession: s.TmuxSession, TmuxWindow: s.TmuxWindow}
			}
		}
		return *m, nil
	}
	if !m.cfg.HideAttachHint {
		if attach := m.pendingAttachDetails(); attach != nil {
			m.pendingAttach = attach
			m.PushView(ViewAttachHint)
			return *m, nil
		}
	}
	return *m, m.attachActiveSession()
}

func cmdNewSession(m *Model) (tea.Model, tea.Cmd) {
	t := m.activeTarget()
	if t.ProjectID == "" {
		return *m, nil
	}
	m.pendingProjectID = t.ProjectID
	m.pendingWorktree = false
	m.inputMode = "new-session"
	m.agentPicker.Show(m.sortedAgentItems())
	m.PushView(ViewAgentPicker)
	return *m, nil
}

func cmdNewWorktree(m *Model) (tea.Model, tea.Cmd) {
	t := m.activeTarget()
	if t.ProjectID == "" {
		return *m, nil
	}
	if cmd := m.initWorktreeSession(t.ProjectID); cmd != nil {
		return *m, cmd
	}
	return *m, nil
}

func cmdKillSession(m *Model) (tea.Model, tea.Cmd) {
	t := m.activeTarget()
	switch t.Kind {
	case TargetProject:
		pid, label := t.ProjectID, t.Label
		return *m, func() tea.Msg {
			return ConfirmActionMsg{
				Message: fmt.Sprintf("Kill project %q and all its sessions?", label),
				Action:  "kill-project:" + pid,
			}
		}
	case TargetSession:
		if t.SessionID == "" {
			return *m, nil
		}
		sid, label := t.SessionID, t.Label
		return *m, func() tea.Msg {
			return ConfirmActionMsg{
				Message: fmt.Sprintf("Kill session %q?", label),
				Action:  "kill-session:" + sid,
			}
		}
	}
	return *m, nil
}

func cmdRename(m *Model) (tea.Model, tea.Cmd) {
	// startRename operates on the active selection, which the grid path
	// needs focused first so the rename dialog targets the right session.
	if m.TopView() == ViewGrid {
		if sess := m.gridView.Selected(); sess != nil {
			m.focusSession(sess.ID)
		}
	}
	return *m, m.startRename()
}

func cmdNewProject(m *Model) (tea.Model, tea.Cmd) {
	m.nameInput.Placeholder = "my-project"
	m.nameInput.Reset()
	m.PushView(ViewProjectName)
	return *m, m.nameInput.Focus()
}

func cmdNewTeam(m *Model) (tea.Model, tea.Cmd) {
	t := m.activeTarget()
	if t.ProjectID == "" {
		return *m, nil
	}
	workDir := ""
	if proj := state.FindProject(&m.appState, t.ProjectID); proj != nil {
		workDir = proj.Directory
	}
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	m.teamBuilder.Start(workDir)
	m.pendingProjectID = t.ProjectID
	m.PushView(ViewTeamBuilder)
	return *m, nil
}

func cmdKillTeam(m *Model) (tea.Model, tea.Cmd) {
	t := m.activeTarget()
	if t.TeamID == "" {
		return *m, nil
	}
	tid := t.TeamID
	teamName := m.teamNameByID(tid)
	return *m, func() tea.Msg {
		return ConfirmActionMsg{
			Message: fmt.Sprintf("Kill team %q and all its sessions?", teamName),
			Action:  "kill-team:" + tid,
		}
	}
}

func cmdOpenGrid(m *Model) (tea.Model, tea.Cmd) {
	m.openGrid(state.GridRestoreProject)
	m.polling.Invalidate()
	return *m, m.scheduleGridPoll()
}

func cmdOpenGridAll(m *Model) (tea.Model, tea.Cmd) {
	m.openGrid(state.GridRestoreAll)
	m.polling.Invalidate()
	return *m, m.scheduleGridPoll()
}

func cmdFocusSidebar(m *Model) (tea.Model, tea.Cmd) {
	m.appState.FocusedPane = state.PaneSidebar
	return *m, nil
}

func cmdFilter(m *Model) (tea.Model, tea.Cmd) {
	m.appState.FilterQuery = ""
	m.PushView(ViewFilter)
	return *m, nil
}

func cmdColorNext(m *Model) (tea.Model, tea.Cmd) { return cycleProjectColorCmd(m, +1) }
func cmdColorPrev(m *Model) (tea.Model, tea.Cmd) { return cycleProjectColorCmd(m, -1) }

func cycleProjectColorCmd(m *Model, dir int) (tea.Model, tea.Cmd) {
	t := m.activeTarget()
	if t.ProjectID == "" {
		return *m, nil
	}
	m.cycleProjectColor(t.ProjectID, dir)
	if m.TopView() == ViewGrid {
		m.gridView.SetProjectColors(m.gridProjectColors())
	}
	return *m, nil
}

func cmdSessionColorNext(m *Model) (tea.Model, tea.Cmd) { return cycleSessionColorCmd(m, +1) }
func cmdSessionColorPrev(m *Model) (tea.Model, tea.Cmd) { return cycleSessionColorCmd(m, -1) }

func cycleSessionColorCmd(m *Model, dir int) (tea.Model, tea.Cmd) {
	t := m.activeTarget()
	if t.SessionID == "" {
		return *m, nil
	}
	m.cycleSessionColor(t.SessionID, dir)
	if m.TopView() == ViewGrid {
		m.gridView.SetSessionColors(m.gridSessionColors())
	}
	return *m, nil
}

func cmdHelp(m *Model) (tea.Model, tea.Cmd) {
	m.helpPanel.Open(0)
	m.PushView(ViewHelp)
	return *m, nil
}

func cmdTmuxHelp(m *Model) (tea.Model, tea.Cmd) {
	m.helpPanel.Open(1)
	m.PushView(ViewHelp)
	return *m, nil
}

func cmdSettings(m *Model) (tea.Model, tea.Cmd) {
	m.settings.Width = m.appState.TermWidth
	m.settings.Height = m.appState.TermHeight
	m.settings.Open(m.cfg)
	m.PushView(ViewSettings)
	return *m, nil
}

func cmdQuit(m *Model) (tea.Model, tea.Cmd) {
	return *m, tea.Quit
}

func cmdQuitKill(m *Model) (tea.Model, tea.Cmd) {
	return *m, func() tea.Msg {
		return ConfirmActionMsg{
			Message: "Quit and kill ALL sessions?",
			Action:  "quit-kill",
		}
	}
}
