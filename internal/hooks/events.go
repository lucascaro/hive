package hooks

// Re-export event name constants from state for convenience.
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
