package state

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Store holds the persisted state and provides reducer-style mutation methods.
// All mutation methods return a new state snapshot; callers replace the old state.

// CreateProject adds a new project.
func CreateProject(state *AppState, name, description, color, directory string) (*AppState, *Project) {
	p := &Project{
		ID:          uuid.New().String(),
		Name:        name,
		Description: description,
		Color:       color,
		Directory:   directory,
		Teams:       []*Team{},
		Sessions:    []*Session{},
		CreatedAt:   time.Now(),
		Meta:        map[string]string{},
	}
	state.Projects = append(state.Projects, p)
	state.ActiveProjectID = p.ID
	return state, p
}

// RemoveProject removes a project by ID.
func RemoveProject(state *AppState, projectID string) *AppState {
	out := make([]*Project, 0, len(state.Projects))
	for _, p := range state.Projects {
		if p.ID != projectID {
			out = append(out, p)
		}
	}
	state.Projects = out
	if state.ActiveProjectID == projectID {
		state.ActiveProjectID = ""
		state.ActiveSessionID = ""
		state.ActiveTeamID = ""
	}
	return state
}

// CreateSession adds a new standalone session to a project.
func CreateSession(state *AppState, projectID string, title string, agentType AgentType, agentCmd []string, workDir, tmuxSession string, tmuxWindow int) (*AppState, *Session) {
	sess := newSession(projectID, "", RoleStandalone, title, agentType, agentCmd, workDir, tmuxSession, tmuxWindow)
	for _, p := range state.Projects {
		if p.ID == projectID {
			p.SessionCounter++
			p.Sessions = append(p.Sessions, sess)
			break
		}
	}
	state.ActiveSessionID = sess.ID
	return state, sess
}

// CreateTeam adds a new team to a project.
func CreateTeam(state *AppState, projectID, name, goal, sharedWorkDir string) (*AppState, *Team) {
	t := &Team{
		ID:            uuid.New().String(),
		ProjectID:     projectID,
		Name:          name,
		Goal:          goal,
		Sessions:      []*Session{},
		SharedWorkDir: sharedWorkDir,
		CreatedAt:     time.Now(),
		Meta:          map[string]string{},
	}
	for _, p := range state.Projects {
		if p.ID == projectID {
			p.Teams = append(p.Teams, t)
			break
		}
	}
	state.ActiveTeamID = t.ID
	return state, t
}

// AddTeamSession adds a session to an existing team.
func AddTeamSession(state *AppState, projectID, teamID string, role TeamRole, title string, agentType AgentType, agentCmd []string, workDir, tmuxSession string, tmuxWindow int) (*AppState, *Session) {
	sess := newSession(projectID, teamID, role, title, agentType, agentCmd, workDir, tmuxSession, tmuxWindow)
	for _, p := range state.Projects {
		if p.ID != projectID {
			continue
		}
		p.SessionCounter++
		for _, t := range p.Teams {
			if t.ID != teamID {
				continue
			}
			t.Sessions = append(t.Sessions, sess)
			if role == RoleOrchestrator {
				t.OrchestratorID = sess.ID
			}
			break
		}
		break
	}
	state.ActiveSessionID = sess.ID
	return state, sess
}

// RemoveSession removes a session from wherever it lives.
func RemoveSession(state *AppState, sessionID string) *AppState {
	for _, p := range state.Projects {
		p.Sessions = filterSessions(p.Sessions, sessionID)
		for _, t := range p.Teams {
			t.Sessions = filterSessions(t.Sessions, sessionID)
		}
	}
	if state.ActiveSessionID == sessionID {
		state.ActiveSessionID = ""
	}
	return state
}

// RemoveTeam removes a team and all its sessions.
func RemoveTeam(state *AppState, teamID string) *AppState {
	for _, p := range state.Projects {
		out := make([]*Team, 0, len(p.Teams))
		for _, t := range p.Teams {
			if t.ID != teamID {
				out = append(out, t)
			}
		}
		p.Teams = out
	}
	if state.ActiveTeamID == teamID {
		state.ActiveTeamID = ""
		state.ActiveSessionID = ""
	}
	return state
}

// UpdateProjectName updates a project's name.
func UpdateProjectName(state *AppState, projectID, name string) *AppState {
	for _, p := range state.Projects {
		if p.ID == projectID {
			p.Name = name
			break
		}
	}
	return state
}

// UpdateTeamName updates a team's name.
func UpdateTeamName(state *AppState, teamID, name string) *AppState {
	for _, p := range state.Projects {
		for _, t := range p.Teams {
			if t.ID == teamID {
				t.Name = name
				return state
			}
		}
	}
	return state
}

// UpdateSessionTitle updates a session's title and title source.
func UpdateSessionTitle(state *AppState, sessionID, title string, src TitleSource) *AppState {
	sess := findSession(state, sessionID)
	if sess != nil {
		sess.Title = title
		sess.TitleSource = src
	}
	return state
}

// UpdateSessionStatus updates the status of a session.
func UpdateSessionStatus(state *AppState, sessionID string, status SessionStatus) *AppState {
	sess := findSession(state, sessionID)
	if sess != nil {
		sess.Status = status
		sess.LastActiveAt = time.Now()
	}
	return state
}

// SetProjectColor sets the color of a project.
func SetProjectColor(state *AppState, projectID, color string) *AppState {
	for _, p := range state.Projects {
		if p.ID == projectID {
			p.Color = color
			break
		}
	}
	return state
}

// ToggleProjectCollapsed toggles the collapsed state of a project in the sidebar.
func ToggleProjectCollapsed(state *AppState, projectID string) *AppState {
	for _, p := range state.Projects {
		if p.ID == projectID {
			p.Collapsed = !p.Collapsed
			break
		}
	}
	return state
}

// ToggleTeamCollapsed toggles the collapsed state of a team in the sidebar.
func ToggleTeamCollapsed(state *AppState, teamID string) *AppState {
	for _, p := range state.Projects {
		for _, t := range p.Teams {
			if t.ID == teamID {
				t.Collapsed = !t.Collapsed
				break
			}
		}
	}
	return state
}

// NextSessionAfterRemoval returns the session ID that should receive focus
// after sessionID is removed. Must be called BEFORE RemoveSession.
// Priority: next in group → prev in group → next overall → prev overall → "".
func NextSessionAfterRemoval(state *AppState, sessionID string) string {
	// Find the group (slice) the session belongs to and its index.
	var group []*Session
	var idx int
	found := false
	for _, p := range state.Projects {
		for ti, t := range p.Teams {
			for si, s := range t.Sessions {
				if s.ID == sessionID {
					group = p.Teams[ti].Sessions
					idx = si
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if found {
			break
		}
		for si, s := range p.Sessions {
			if s.ID == sessionID {
				group = p.Sessions
				idx = si
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return ""
	}

	// Next in group.
	if idx+1 < len(group) {
		return group[idx+1].ID
	}
	// Prev in group.
	if idx-1 >= 0 {
		return group[idx-1].ID
	}

	// Fallback: next/prev overall, in sidebar/UI traversal order (teams
	// before standalone sessions within a project) — so the fallback
	// matches what the user sees.
	all := allSessionsUIOrder(state)
	for i, s := range all {
		if s.ID == sessionID {
			if i+1 < len(all) {
				return all[i+1].ID
			}
			if i-1 >= 0 {
				return all[i-1].ID
			}
			break
		}
	}
	return ""
}

// allSessionsUIOrder flattens all sessions in the order the sidebar renders
// them: per project, teams (with their sessions) first, then standalone
// sessions. Used by focus-fallback logic so "next overall" matches the UI.
func allSessionsUIOrder(state *AppState) []*Session {
	var out []*Session
	for _, p := range state.Projects {
		for _, t := range p.Teams {
			out = append(out, t.Sessions...)
		}
		out = append(out, p.Sessions...)
	}
	return out
}

// MoveSessionUp swaps a session with the one before it in its containing slice.
// No-op if the session is already first or not found.
func MoveSessionUp(state *AppState, sessionID string) *AppState {
	for _, p := range state.Projects {
		if i := sessionIndex(p.Sessions, sessionID); i > 0 {
			p.Sessions[i], p.Sessions[i-1] = p.Sessions[i-1], p.Sessions[i]
			return state
		}
		for _, t := range p.Teams {
			if i := sessionIndex(t.Sessions, sessionID); i > 0 {
				t.Sessions[i], t.Sessions[i-1] = t.Sessions[i-1], t.Sessions[i]
				return state
			}
		}
	}
	return state
}

// MoveSessionDown swaps a session with the one after it in its containing slice.
// No-op if the session is already last or not found.
func MoveSessionDown(state *AppState, sessionID string) *AppState {
	for _, p := range state.Projects {
		if i := sessionIndex(p.Sessions, sessionID); i >= 0 && i < len(p.Sessions)-1 {
			p.Sessions[i], p.Sessions[i+1] = p.Sessions[i+1], p.Sessions[i]
			return state
		}
		for _, t := range p.Teams {
			if i := sessionIndex(t.Sessions, sessionID); i >= 0 && i < len(t.Sessions)-1 {
				t.Sessions[i], t.Sessions[i+1] = t.Sessions[i+1], t.Sessions[i]
				return state
			}
		}
	}
	return state
}

// MoveTeamUp swaps a team with the one before it in its project's Teams slice.
// No-op if the team is already first or not found.
func MoveTeamUp(state *AppState, teamID string) *AppState {
	for _, p := range state.Projects {
		for i, t := range p.Teams {
			if t.ID == teamID {
				if i > 0 {
					p.Teams[i], p.Teams[i-1] = p.Teams[i-1], p.Teams[i]
				}
				return state
			}
		}
	}
	return state
}

// MoveTeamDown swaps a team with the one after it in its project's Teams slice.
// No-op if the team is already last or not found.
func MoveTeamDown(state *AppState, teamID string) *AppState {
	for _, p := range state.Projects {
		for i, t := range p.Teams {
			if t.ID == teamID {
				if i < len(p.Teams)-1 {
					p.Teams[i], p.Teams[i+1] = p.Teams[i+1], p.Teams[i]
				}
				return state
			}
		}
	}
	return state
}

// MoveProjectUp swaps a project with the one before it in the Projects slice.
// No-op if the project is already first or not found.
func MoveProjectUp(state *AppState, projectID string) *AppState {
	for i, p := range state.Projects {
		if p.ID == projectID {
			if i > 0 {
				state.Projects[i], state.Projects[i-1] = state.Projects[i-1], state.Projects[i]
			}
			return state
		}
	}
	return state
}

// MoveProjectDown swaps a project with the one after it in the Projects slice.
// No-op if the project is already last or not found.
func MoveProjectDown(state *AppState, projectID string) *AppState {
	for i, p := range state.Projects {
		if p.ID == projectID {
			if i < len(state.Projects)-1 {
				state.Projects[i], state.Projects[i+1] = state.Projects[i+1], state.Projects[i]
			}
			return state
		}
	}
	return state
}

// --- helpers ---

func sessionIndex(sessions []*Session, id string) int {
	for i, s := range sessions {
		if s.ID == id {
			return i
		}
	}
	return -1
}

func newSession(projectID, teamID string, role TeamRole, title string, agentType AgentType, agentCmd []string, workDir, tmuxSession string, tmuxWindow int) *Session {
	return &Session{
		ID:           uuid.New().String(),
		ProjectID:    projectID,
		TeamID:       teamID,
		TeamRole:     role,
		Title:        title,
		TmuxSession:  tmuxSession,
		TmuxWindow:   tmuxWindow,
		Status:       StatusRunning,
		TitleSource:  TitleSourceAuto,
		AgentType:    agentType,
		AgentCmd:     agentCmd,
		WorkDir:      workDir,
		CreatedAt:    time.Now(),
		LastActiveAt: time.Now(),
		Meta:         map[string]string{},
	}
}

func filterSessions(sessions []*Session, excludeID string) []*Session {
	out := make([]*Session, 0, len(sessions))
	for _, s := range sessions {
		if s.ID != excludeID {
			out = append(out, s)
		}
	}
	return out
}

func findSession(state *AppState, sessionID string) *Session {
	for _, p := range state.Projects {
		for _, s := range p.Sessions {
			if s.ID == sessionID {
				return s
			}
		}
		for _, t := range p.Teams {
			for _, s := range t.Sessions {
				if s.ID == sessionID {
					return s
				}
			}
		}
	}
	return nil
}

// RecordAgentUsage increments usage count and updates last-used time for an agent.
func RecordAgentUsage(s *AppState, agentType string) {
	if s.AgentUsage == nil {
		s.AgentUsage = make(map[string]AgentUsageRecord)
	}
	rec := s.AgentUsage[agentType]
	rec.Count++
	rec.LastUsed = time.Now()
	s.AgentUsage[agentType] = rec
}

// AllSessions returns every session in the state flattened.
func AllSessions(state *AppState) []*Session {
	var out []*Session
	for _, p := range state.Projects {
		out = append(out, p.Sessions...)
		for _, t := range p.Teams {
			out = append(out, t.Sessions...)
		}
	}
	return out
}

// FindSession returns the session with the given ID, or nil if not found.
func FindSession(state *AppState, sessionID string) *Session {
	return findSession(state, sessionID)
}

// FindProject returns the project with the given ID, or nil if not found.
func FindProject(state *AppState, projectID string) *Project {
	for _, p := range state.Projects {
		if p.ID == projectID {
			return p
		}
	}
	return nil
}

// FindTeam returns the team with the given ID, or nil if not found.
func FindTeam(state *AppState, teamID string) *Team {
	for _, p := range state.Projects {
		for _, t := range p.Teams {
			if t.ID == teamID {
				return t
			}
		}
	}
	return nil
}

// FindSessionByTmux returns the session matching the given tmux session and window index, or nil.
func FindSessionByTmux(state *AppState, tmuxSession string, tmuxWindow int) *Session {
	for _, p := range state.Projects {
		for _, s := range p.Sessions {
			if s.TmuxSession == tmuxSession && s.TmuxWindow == tmuxWindow {
				return s
			}
		}
		for _, t := range p.Teams {
			for _, s := range t.Sessions {
				if s.TmuxSession == tmuxSession && s.TmuxWindow == tmuxWindow {
					return s
				}
			}
		}
	}
	return nil
}

// SessionLabel returns a short display string for a session.
func SessionLabel(s *Session) string {
	role := ""
	if s.TeamRole == RoleOrchestrator {
		role = "★ "
	}
	return fmt.Sprintf("%s%s [%s]", role, s.Title, s.AgentType)
}
