package state

// Event name constants shared between the hook system and internal dispatching.
const (
	EventSessionCreate      = "session-create"
	EventSessionKill        = "session-kill"
	EventSessionAttach      = "session-attach"
	EventSessionDetach      = "session-detach"
	EventSessionTitleChange = "session-title-changed"
	EventProjectCreate      = "project-create"
	EventProjectKill        = "project-kill"
	EventTeamCreate         = "team-create"
	EventTeamKill           = "team-kill"
	EventTeamMemberAdd      = "team-member-add"
	EventTeamMemberRemove   = "team-member-remove"
)

// HookEvent carries all context for a hook invocation.
type HookEvent struct {
	Name        string
	ProjectID   string
	ProjectName string
	SessionID   string
	SessionTitle string
	TeamID      string
	TeamName    string
	TeamRole    TeamRole
	AgentType   AgentType
	AgentCmd    []string
	TmuxSession string
	TmuxWindow  int
	WorkDir     string
}
