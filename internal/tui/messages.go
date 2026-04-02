package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/state"
)

// --- Session lifecycle ---

// SessionCreatedMsg is sent after a new session is spawned in tmux.
type SessionCreatedMsg struct {
	Session *state.Session
}

// SessionKilledMsg is sent after a session's tmux window is removed.
type SessionKilledMsg struct {
	SessionID string
}

// SessionAttachMsg triggers attaching to a session (suspends TUI).
type SessionAttachMsg struct {
	TmuxSession     string
	TmuxWindow      int
	RestoreGridMode state.GridRestoreMode
	// Display metadata used to set the terminal window title while attached.
	SessionTitle string
	AgentType    state.AgentType
	ProjectName  string
}

// SessionDetachedMsg is sent when the user returns from a tmux session.
type SessionDetachedMsg struct{}

// SessionTitleChangedMsg carries a new title for a session.
type SessionTitleChangedMsg struct {
	SessionID string
	Title     string
	Source    state.TitleSource
}

// SessionStatusChangedMsg carries a new status for a session.
type SessionStatusChangedMsg struct {
	SessionID string
	Status    state.SessionStatus
}

// --- Team lifecycle ---

// TeamCreatedMsg is sent after a new team (and its sessions) are created.
type TeamCreatedMsg struct {
	Team *state.Team
}

// TeamKilledMsg is sent after a team is removed.
type TeamKilledMsg struct {
	TeamID string
}

// --- Project lifecycle ---

// ProjectCreatedMsg is sent after a new project is created.
type ProjectCreatedMsg struct {
	Project *state.Project
}

// ProjectKilledMsg is sent after a project is removed.
type ProjectKilledMsg struct {
	ProjectID string
}

// --- UI ---

// ErrorMsg carries a non-fatal error message to display in the status bar.
type ErrorMsg struct{ Err error }

func (e ErrorMsg) Error() string { return e.Err.Error() }

// ConfirmActionMsg requests a yes/no confirmation.
type ConfirmActionMsg struct {
	Message string
	Action  string // opaque identifier handled in Update
}

// ConfirmedMsg is sent when the user confirms an action.
type ConfirmedMsg struct{ Action string }

// PersistMsg triggers writing state to disk.
type PersistMsg struct{}

// QuitAndKillMsg signals quit + kill all managed sessions.
type QuitAndKillMsg struct{}

// AgentInstalledMsg is sent after a successful agent installation.
type AgentInstalledMsg struct{ AgentType string }

// CleanOrphansMsg is sent when the user confirms orphaned tmux session cleanup.
// Sessions holds the tmux session names (e.g. "hive-abc12345") to kill.
type CleanOrphansMsg struct {
	Sessions []string
}

// ConfigSavedMsg is sent after config changes are successfully written to disk.
type ConfigSavedMsg struct {
	Config config.Config
}

// Ensure tea.Msg interface satisfaction (compile-time checks).
var _ tea.Msg = SessionCreatedMsg{}
