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

// --- helpers ---

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

// SessionLabel returns a short display string for a session.
func SessionLabel(s *Session) string {
	role := ""
	if s.TeamRole == RoleOrchestrator {
		role = "★ "
	}
	return fmt.Sprintf("%s%s [%s]", role, s.Title, s.AgentType)
}
