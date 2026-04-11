package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/git"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
	"github.com/lucascaro/hive/internal/tui/styles"
)

func (m *Model) createProject(name, directory string) tea.Cmd {
	usedColors := make([]string, 0, len(m.appState.Projects))
	for _, p := range m.appState.Projects {
		usedColors = append(usedColors, p.Color)
	}
	_, proj := state.CreateProject(&m.appState, name, "", styles.NextFreeColor(usedColors), directory)
	m.commitState()
	m.fireHook(state.HookEvent{
		Name:        state.EventProjectCreate,
		ProjectID:   proj.ID,
		ProjectName: proj.Name,
		WorkDir:     proj.Directory,
	})
	return func() tea.Msg { return ProjectCreatedMsg{Project: proj} }
}

// cycleProjectColor changes a project's color to the next/prev free palette color.
func (m *Model) cycleProjectColor(projectID string, direction int) {
	proj := state.FindProject(&m.appState, projectID)
	if proj == nil {
		return
	}
	// Collect colors used by other projects.
	usedByOthers := make([]string, 0, len(m.appState.Projects))
	for _, p := range m.appState.Projects {
		if p.ID != projectID {
			usedByOthers = append(usedByOthers, p.Color)
		}
	}
	newColor := styles.CycleColor(proj.Color, direction, usedByOthers)
	state.SetProjectColor(&m.appState, projectID, newColor)
	m.commitState()
	m.sidebar.Rebuild(&m.appState)
}

// cycleSessionColor changes a session's color to the next/prev free palette color,
// skipping colors used by sibling sessions in the same project.
func (m *Model) cycleSessionColor(sessionID string, direction int) {
	sess := state.FindSession(&m.appState, sessionID)
	if sess == nil {
		return
	}
	proj := state.FindProject(&m.appState, sess.ProjectID)
	if proj == nil {
		return
	}
	usedByOthers := m.siblingSessionColors(sess.ProjectID, sessionID)
	// Also exclude the project color so cycling never lands on it.
	usedByOthers = append(usedByOthers, proj.Color)
	current := sess.Color
	if current == "" {
		current = proj.Color
	}
	newColor := styles.CycleColor(current, direction, usedByOthers)
	state.SetSessionColor(&m.appState, sessionID, newColor)
	m.commitState()
	m.sidebar.Rebuild(&m.appState)
}

// siblingSessionColors collects colors used by other sessions in the same project.
func (m *Model) siblingSessionColors(projectID, excludeSessionID string) []string {
	proj := state.FindProject(&m.appState, projectID)
	if proj == nil {
		return nil
	}
	var colors []string
	for _, s := range proj.Sessions {
		if s.ID != excludeSessionID && s.Color != "" {
			colors = append(colors, s.Color)
		}
	}
	for _, t := range proj.Teams {
		for _, s := range t.Sessions {
			if s.ID != excludeSessionID && s.Color != "" {
				colors = append(colors, s.Color)
			}
		}
	}
	return colors
}

// autoAssignSessionColor assigns a color to a newly created session.
func (m *Model) autoAssignSessionColor(sess *state.Session) {
	proj := state.FindProject(&m.appState, sess.ProjectID)
	if proj == nil {
		return
	}
	usedColors := m.siblingSessionColors(sess.ProjectID, sess.ID)
	sess.Color = styles.NextFreeSessionColor(proj.Color, usedColors)
}

func (m *Model) createSessionWithWorktree(projectID, agentTypeStr string, agentCmd []string, branch string) tea.Cmd {
	// Find project directory.
	proj := state.FindProject(&m.appState, projectID)
	if proj == nil {
		return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("project not found")} }
	}
	projDir := proj.Directory
	if projDir == "" {
		projDir, _ = os.Getwd()
	}

	// Resolve git root.
	gitRoot, err := git.Root(projDir)
	if err != nil {
		return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("not a git repository: %w", err)} }
	}

	worktreePath := git.WorktreePath(gitRoot, branch)

	// If .worktrees is not yet in .gitignore, ask the user first.
	if !git.IsInGitignore(gitRoot, ".worktrees") {
		// Stash branch/agent info so the confirm handler can retrieve them.
		m.pendingWorktreeAgentType = agentTypeStr
		m.pendingWorktreeAgentCmd = agentCmd
		return func() tea.Msg {
			return ConfirmActionMsg{
				Message: "Add \".worktrees\" to .gitignore?\n\n(Recommended to keep worktrees out of git history)",
				Action:  "gitignore-worktrees:" + projectID + ":" + branch,
			}
		}
	}

	return m.spawnWorktreeSession(proj, agentTypeStr, agentCmd, branch, gitRoot, worktreePath)
}

func (m *Model) spawnWorktreeSession(proj *state.Project, agentTypeStr string, agentCmd []string, branch, gitRoot, worktreePath string) tea.Cmd {
	agentType := state.AgentType(agentTypeStr)
	if len(agentCmd) == 0 {
		if profile, ok := m.cfg.Agents[agentTypeStr]; ok {
			agentCmd = profile.Cmd
		} else {
			agentCmd = []string{agentTypeStr}
		}
	}

	// Create the worktree first so the agent starts in an existing directory.
	if err := git.CreateWorktree(gitRoot, branch, worktreePath); err != nil {
		return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("worktree creation failed: %w", err)} }
	}

	muxSess := mux.SessionName(proj.ID)
	sessionTitle := branch

	winName := mux.WindowName(proj.Name, agentTypeStr, sessionTitle)
	windowIdx, err := ensureMuxWindow(muxSess, winName, worktreePath, agentCmd)
	if err != nil {
		_ = git.RemoveWorktree(gitRoot, worktreePath)
		return func() tea.Msg { return ErrorMsg{Err: err} }
	}

	_, sess := state.CreateSession(&m.appState, proj.ID, sessionTitle, agentType, agentCmd, worktreePath, muxSess, windowIdx)
	sess.WorktreePath = worktreePath
	sess.WorktreeBranch = branch
	m.autoAssignSessionColor(sess)
	state.RecordAgentUsage(&m.appState, agentTypeStr)
	m.commitState()

	m.fireHook(state.HookEvent{
		Name:         state.EventSessionCreate,
		ProjectID:    proj.ID,
		ProjectName:  proj.Name,
		SessionID:    sess.ID,
		SessionTitle: sess.Title,
		AgentType:    agentType,
		AgentCmd:     agentCmd,
		TmuxSession:  muxSess,
		TmuxWindow:   windowIdx,
		WorkDir:      worktreePath,
	})

	return func() tea.Msg { return SessionCreatedMsg{Session: sess} }
}

func (m *Model) createSession(projectID, agentTypeStr string, agentCmd []string) tea.Cmd {
	agentType := state.AgentType(agentTypeStr)
	if len(agentCmd) == 0 {
		if profile, ok := m.cfg.Agents[agentTypeStr]; ok {
			agentCmd = profile.Cmd
		} else {
			agentCmd = []string{agentTypeStr}
		}
	}

	// Find project to get tmux session name.
	proj := state.FindProject(&m.appState, projectID)
	if proj == nil {
		return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("project not found")} }
	}

	workDir := proj.Directory
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	muxSess := mux.SessionName(projectID)
	sessionTitle := git.RandomBranchName()
	for _, s := range proj.Sessions {
		if s.Title == sessionTitle {
			sessionTitle = git.RandomBranchName()
		}
	}

	winName := mux.WindowName(proj.Name, agentTypeStr, sessionTitle)
	windowIdx, err := ensureMuxWindow(muxSess, winName, workDir, agentCmd)
	if err != nil {
		return func() tea.Msg { return ErrorMsg{Err: err} }
	}

	_, sess := state.CreateSession(&m.appState, projectID, sessionTitle, agentType, agentCmd, workDir, muxSess, windowIdx)
	m.autoAssignSessionColor(sess)
	state.RecordAgentUsage(&m.appState, agentTypeStr)
	m.commitState()

	m.fireHook(state.HookEvent{
		Name:         state.EventSessionCreate,
		ProjectID:    projectID,
		ProjectName:  proj.Name,
		SessionID:    sess.ID,
		SessionTitle: sess.Title,
		AgentType:    agentType,
		AgentCmd:     agentCmd,
		TmuxSession:  muxSess,
		TmuxWindow:   windowIdx,
		WorkDir:      workDir,
	})
	return func() tea.Msg { return SessionCreatedMsg{Session: sess} }
}

func (m *Model) createTeam(spec components.TeamSpec) tea.Cmd {
	projectID := m.pendingProjectID
	proj := state.FindProject(&m.appState, projectID)
	if proj == nil {
		return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("project not found")} }
	}

	_, team := state.CreateTeam(&m.appState, projectID, spec.Name, spec.Goal, spec.SharedWorkDir)
	m.fireHook(state.HookEvent{
		Name:        state.EventTeamCreate,
		ProjectID:   projectID,
		ProjectName: proj.Name,
		TeamID:      team.ID,
		TeamName:    team.Name,
		WorkDir:     spec.SharedWorkDir,
	})

	muxSess := mux.SessionName(projectID)

	// Create orchestrator session.
	var cmds []tea.Cmd
	orchCmd := m.agentCmd(string(spec.OrchestratorAgent))
	cmds = append(cmds, m.addTeamSession(proj, team, state.RoleOrchestrator, "orchestrator", spec.OrchestratorAgent, orchCmd, spec.SharedWorkDir, muxSess))

	// Create worker sessions.
	for i, agentType := range spec.Workers {
		workerCmd := m.agentCmd(string(agentType))
		title := fmt.Sprintf("worker-%d", i+1)
		cmds = append(cmds, m.addTeamSession(proj, team, state.RoleWorker, title, agentType, workerCmd, spec.SharedWorkDir, muxSess))
	}

	m.commitState()
	return tea.Batch(append(cmds, func() tea.Msg { return TeamCreatedMsg{Team: team} })...)
}

func (m *Model) addTeamSession(proj *state.Project, team *state.Team, role state.TeamRole, title string, agentType state.AgentType, agentCmd []string, workDir, muxSess string) tea.Cmd {
	winName := mux.WindowName(proj.Name, string(agentType), title)
	windowIdx, err := ensureMuxWindow(muxSess, winName, workDir, agentCmd)
	if err != nil {
		return func() tea.Msg { return ErrorMsg{Err: err} }
	}

	_, sess := state.AddTeamSession(&m.appState, proj.ID, team.ID, role, title, agentType, agentCmd, workDir, muxSess, windowIdx)
	m.autoAssignSessionColor(sess)
	state.RecordAgentUsage(&m.appState, string(agentType))
	m.fireHook(state.HookEvent{
		Name:         state.EventTeamMemberAdd,
		ProjectID:    proj.ID,
		ProjectName:  proj.Name,
		SessionID:    sess.ID,
		SessionTitle: title,
		TeamID:       team.ID,
		TeamName:     team.Name,
		TeamRole:     role,
		AgentType:    agentType,
		AgentCmd:     agentCmd,
		TmuxSession:  muxSess,
		TmuxWindow:   windowIdx,
		WorkDir:      workDir,
	})
	return func() tea.Msg { return SessionCreatedMsg{Session: sess} }
}

func (m *Model) attachActiveSession() tea.Cmd {
	sel := m.sidebar.Selected()
	if sel == nil || sel.SessionID == "" {
		return nil
	}
	sess := m.activeSessionByID(sel.SessionID)
	if sess == nil {
		return nil
	}
	m.fireHook(state.HookEvent{
		Name:         state.EventSessionAttach,
		SessionID:    sess.ID,
		SessionTitle: sess.Title,
		AgentType:    sess.AgentType,
		TmuxSession:  sess.TmuxSession,
		TmuxWindow:   sess.TmuxWindow,
		WorkDir:      sess.WorkDir,
	})
	return func() tea.Msg {
		return SessionAttachMsg{
			TmuxSession:     sess.TmuxSession,
			TmuxWindow:      sess.TmuxWindow,
			RestoreGridMode: state.GridRestoreNone,
			SessionTitle:    sess.Title,
			AgentType:       sess.AgentType,
			ProjectName:     m.projectNameByID(sess.ProjectID),
			Status:          sess.Status,
			WorktreeBranch:  sess.WorktreeBranch,
			WorktreePath:    sess.WorktreePath,
		}
	}
}

func (m *Model) killSession(sessionID string) tea.Cmd {
	sess := m.activeSessionByID(sessionID)
	var tmuxSess string
	if sess != nil {
		tmuxSess = sess.TmuxSession
		target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
		_ = mux.KillWindow(target)
		m.fireHook(state.HookEvent{
			Name:         state.EventSessionKill,
			SessionID:    sess.ID,
			SessionTitle: sess.Title,
			AgentType:    sess.AgentType,
			TmuxSession:  sess.TmuxSession,
			TmuxWindow:   sess.TmuxWindow,
		})
		// Clean up git worktree if this session owns one.
		if sess.WorktreePath != "" {
			repoDir := sess.WorkDir
			if gitRoot, err := git.Root(repoDir); err == nil {
				_ = git.RemoveWorktree(gitRoot, sess.WorktreePath)
			}
		}
	}
	// State removal and focus fallback happen in handleSessionKilled to
	// avoid mutating shared Project pointers from a discarded Model copy.
	return func() tea.Msg { return SessionKilledMsg{SessionID: sessionID, TmuxSession: tmuxSess} }
}

func (m *Model) killTeam(teamID string) tea.Cmd {
	// Kill all sessions in the team.
	var teamTmuxSessions []string
	for _, p := range m.appState.Projects {
		for _, t := range p.Teams {
			if t.ID == teamID {
				for _, sess := range t.Sessions {
					teamTmuxSessions = append(teamTmuxSessions, sess.TmuxSession)
					target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
					_ = mux.KillWindow(target)
				}
				m.fireHook(state.HookEvent{
					Name:   state.EventTeamKill,
					TeamID: t.ID, TeamName: t.Name,
				})
			}
		}
	}
	m.appState = *state.RemoveTeam(&m.appState, teamID)
	// Clean up any now-empty tmux session containers.
	for _, s := range teamTmuxSessions {
		killTmuxSessionIfEmpty(&m.appState, s)
	}
	m.commitState()
	return func() tea.Msg { return TeamKilledMsg{TeamID: teamID} }
}

func (m *Model) killProject(projectID string) tea.Cmd {
	for _, p := range m.appState.Projects {
		if p.ID == projectID {
			for _, sess := range p.Sessions {
				target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
				_ = mux.KillWindow(target)
			}
			for _, t := range p.Teams {
				for _, sess := range t.Sessions {
					target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
					_ = mux.KillWindow(target)
				}
			}
			// Kill per-project session if it's not the shared container.
			if sess := mux.SessionName(projectID); sess != mux.HiveSession {
				_ = mux.KillSession(sess)
			}
			m.fireHook(state.HookEvent{
				Name:      state.EventProjectKill,
				ProjectID: p.ID, ProjectName: p.Name,
			})
		}
	}
	m.appState = *state.RemoveProject(&m.appState, projectID)
	m.commitState()
	return func() tea.Msg { return ProjectKilledMsg{ProjectID: projectID} }
}

func (m *Model) killAllSessions() {
	// Collect unique tmux session containers before we start killing windows.
	tmuxSessions := uniqueTmuxSessionNames(&m.appState)
	for _, sess := range state.AllSessions(&m.appState) {
		target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
		_ = mux.KillWindow(target)
	}
	// All windows are gone — kill the now-empty session containers.
	for _, s := range tmuxSessions {
		_ = mux.KillSession(s)
	}
}

func (m Model) handleConfirmedAction(action string) tea.Cmd {
	if strings.HasPrefix(action, "kill-session:") {
		sessionID := strings.TrimPrefix(action, "kill-session:")
		return m.killSession(sessionID)
	}
	if strings.HasPrefix(action, "kill-team:") {
		teamID := strings.TrimPrefix(action, "kill-team:")
		return m.killTeam(teamID)
	}
	if strings.HasPrefix(action, "kill-project:") {
		projectID := strings.TrimPrefix(action, "kill-project:")
		return m.killProject(projectID)
	}
	if strings.HasPrefix(action, "install-agent:") {
		agentType := strings.TrimPrefix(action, "install-agent:")
		installCmd := m.cfg.Agents[agentType].InstallCmd
		if len(installCmd) == 0 {
			return func() tea.Msg {
				return ErrorMsg{Err: fmt.Errorf("no install command configured for %q", agentType)}
			}
		}
		m.appState.LastError = ""
		m.appState.InstallingAgent = agentType
		return func() tea.Msg {
			if err := exec.Command(installCmd[0], installCmd[1:]...).Run(); err != nil {
				return ErrorMsg{Err: fmt.Errorf("install %s failed: %w", agentType, err)}
			}
			return AgentInstalledMsg{AgentType: agentType}
		}
	}
	if strings.HasPrefix(action, "gitignore-worktrees:") {
		// action format: "gitignore-worktrees:<projectID>:<branch>"
		rest := strings.TrimPrefix(action, "gitignore-worktrees:")
		colonIdx := strings.Index(rest, ":")
		if colonIdx < 0 {
			return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("malformed gitignore action")} }
		}
		projectID := rest[:colonIdx]
		branch := rest[colonIdx+1:]

		proj := state.FindProject(&m.appState, projectID)
		if proj == nil {
			return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("project not found")} }
		}
		projDir := proj.Directory
		if projDir == "" {
			projDir, _ = os.Getwd()
		}
		gitRoot, err := git.Root(projDir)
		if err != nil {
			return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("git root not found: %w", err)} }
		}
		_ = git.AddToGitignore(gitRoot, ".worktrees")
		worktreePath := git.WorktreePath(gitRoot, branch)
		agentTypeStr := m.pendingWorktreeAgentType
		agentCmd := m.pendingWorktreeAgentCmd
		m.pendingWorktreeAgentType = ""
		m.pendingWorktreeAgentCmd = nil
		return m.spawnWorktreeSession(proj, agentTypeStr, agentCmd, branch, gitRoot, worktreePath)
	}
	if action == "quit-kill" {
		m.killAllSessions()
		return tea.Quit
	}
	return nil
}

// recoverSessions creates a "Recovered Sessions" project (if it doesn't already
// exist) and adds each selected RecoverableSession as a state Session pointing
// at the existing tmux window.
func (m *Model) recoverSessions(sessions []state.RecoverableSession) {
	workDir := m.appState.RecoveryWorkDir

	// Find or create the recovery project.
	var proj *state.Project
	for _, p := range m.appState.Projects {
		if p.Name == "Recovered Sessions" {
			proj = p
			break
		}
	}
	if proj == nil {
		var newProj *state.Project
		_, newProj = state.CreateProject(&m.appState, "Recovered Sessions", "", "#6B7280", workDir)
		proj = newProj
	}

	for _, rs := range sessions {
		agentType := rs.DetectedAgentType
		if agentType == "" {
			agentType = state.AgentCustom
		}
		title := rs.WindowName
		if title == "" {
			title = fmt.Sprintf("%s:%d", rs.TmuxSession, rs.WindowIndex)
		}
		state.CreateSession(
			&m.appState,
			proj.ID,
			title,
			agentType,
			nil,
			workDir,
			rs.TmuxSession,
			rs.WindowIndex,
		)
	}

	m.commitState()
}
