package state

import (
	"math"
	"time"
)

// AgentUsageRecord tracks how often and how recently an agent was used.
type AgentUsageRecord struct {
	Count    int       `json:"count"`
	LastUsed time.Time `json:"last_used"`
}

// Score returns a combined recency+frequency score (higher = prefer first).
func (r AgentUsageRecord) Score() float64 {
	hoursSince := time.Since(r.LastUsed).Hours()
	if hoursSince < 0 {
		hoursSince = 0 // guard against future timestamps (clock skew)
	}
	return math.Log(float64(r.Count)+1) + 1.0/math.Log(hoursSince+math.E)
}

// AgentType identifies which AI agent runs in a session.
type AgentType string

const (
	AgentClaude   AgentType = "claude"
	AgentCodex    AgentType = "codex"
	AgentGemini   AgentType = "gemini"
	AgentCopilot  AgentType = "copilot"
	AgentAider    AgentType = "aider"
	AgentOpenCode AgentType = "opencode"
	AgentCustom   AgentType = "custom"
)

// TeamRole describes a session's role within an agent team.
type TeamRole string

const (
	RoleOrchestrator TeamRole = "orchestrator"
	RoleWorker       TeamRole = "worker"
	RoleStandalone   TeamRole = "standalone"
)

// SessionStatus reflects the perceived activity of a session.
type SessionStatus string

const (
	StatusRunning SessionStatus = "running"
	StatusIdle    SessionStatus = "idle"
	StatusWaiting SessionStatus = "waiting"
	StatusDead    SessionStatus = "dead"
)

// TitleSource tracks how a session's title was last set.
type TitleSource string

const (
	TitleSourceAuto  TitleSource = "auto"
	TitleSourceUser  TitleSource = "user"
	TitleSourceAgent TitleSource = "agent"
)

// Pane identifies which TUI pane is focused.
type Pane int

const (
	PaneSidebar Pane = iota
	PanePreview
)

// GridRestoreMode identifies which grid scope should be restored after attach.
type GridRestoreMode string

const (
	GridRestoreNone    GridRestoreMode = ""
	GridRestoreProject GridRestoreMode = "project"
	GridRestoreAll     GridRestoreMode = "all"
)

// RecoverableSession is a tmux window found in an orphaned hive-* session that
// has live content. It is transient — set by cmd/start.go and consumed by the TUI.
type RecoverableSession struct {
	TmuxSession       string    // e.g. "hive-a1b2c3d4"
	WindowIndex       int       // tmux window index
	WindowName        string    // tmux window name
	DetectedAgentType AgentType // empty = unknown / not detected
	PanePreview       string    // last ~10 lines of visible pane content
}

// Project groups related AI agent sessions.
type Project struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Color          string            `json:"color"`
	Directory      string            `json:"directory,omitempty"` // working directory for all sessions in this project
	Teams          []*Team           `json:"teams"`
	Sessions       []*Session        `json:"sessions"` // standalone (no team)
	Collapsed      bool              `json:"-"`               // per-instance view state; not persisted so two hive instances can collapse independently
	SessionCounter int               `json:"session_counter"` // monotonically increasing; never reset on delete
	CreatedAt      time.Time         `json:"created_at"`
	Meta           map[string]string `json:"meta,omitempty"`
}

// Team is a coordinated group of agents (orchestrator + workers).
type Team struct {
	ID             string            `json:"id"`
	ProjectID      string            `json:"project_id"`
	Name           string            `json:"name"`
	Goal           string            `json:"goal"`
	OrchestratorID string            `json:"orchestrator_id"`
	Sessions       []*Session        `json:"sessions"`
	SharedWorkDir  string            `json:"shared_work_dir"`
	Collapsed      bool              `json:"-"` // per-instance view state; not persisted (see Project.Collapsed)
	CreatedAt      time.Time         `json:"created_at"`
	Meta           map[string]string `json:"meta,omitempty"`
}

// Session maps 1:1 to a tmux window.
type Session struct {
	ID             string            `json:"id"`
	ProjectID      string            `json:"project_id"`
	TeamID         string            `json:"team_id,omitempty"`
	TeamRole       TeamRole          `json:"team_role"`
	Title          string            `json:"title"`
	TmuxSession    string            `json:"tmux_session"`
	TmuxWindow     int               `json:"tmux_window"`
	Status         SessionStatus     `json:"status"`
	TitleSource    TitleSource       `json:"title_source"`
	AgentType      AgentType         `json:"agent_type"`
	AgentCmd       []string          `json:"agent_cmd"`
	Color          string            `json:"color,omitempty"`
	WorkDir        string            `json:"work_dir"`
	WorktreePath   string            `json:"worktree_path,omitempty"`   // non-empty = session runs in a git worktree
	WorktreeBranch string            `json:"worktree_branch,omitempty"` // branch name for the worktree
	CreatedAt      time.Time         `json:"created_at"`
	LastActiveAt   time.Time         `json:"last_active_at"`
	Meta           map[string]string `json:"meta,omitempty"`
}

// AppState is the single source of truth for the TUI.
// Only mutated inside Bubble Tea's Update() — no external locking needed.
type AppState struct {
	Projects        []*Project
	ActiveProjectID string
	ActiveTeamID    string
	ActiveSessionID string
	FocusedPane     Pane
	PreviewContent  string
	EditingTitle    bool
	TitleDraft      string
	TermWidth       int
	TermHeight      int
	LastError       string
	// UI overlay states
	ShowHelp      bool
	ShowConfirm   bool
	ConfirmMsg    string
	ConfirmAction string // opaque action identifier
	FilterQuery   string
	FilterActive  bool
	// Agent usage tracking (persisted separately in usage.json)
	AgentUsage map[string]AgentUsageRecord
	// InstallingAgent holds the agent type currently being installed (empty = none).
	InstallingAgent string
	// RestoreGridMode is transient: when non-empty, New() opens the grid view on
	// startup using the matching scope. Set by cmd/start.go after the user
	// detaches from a grid-initiated session.
	RestoreGridMode GridRestoreMode
	// OrphanSessions is transient: when non-empty, the TUI shows an orphan-cleanup
	// overlay on startup listing hive-* tmux sessions with no matching project.
	// Set by cmd/start.go after reconcileState detects orphaned containers.
	OrphanSessions []string
	// RecoverableSessions is transient: windows in orphaned hive-* sessions that
	// have live content and can be re-adopted into state.
	// Set by cmd/start.go; cleared after the TUI recovery picker consumes it.
	RecoverableSessions []RecoverableSession
	// RecoveryWorkDir is the working directory to assign to the "Recovered Sessions"
	// project. Set to cwd at startup; never persisted.
	RecoveryWorkDir string
}

// TeamStatus derives an aggregate status from all team member statuses.
func (t *Team) TeamStatus() SessionStatus {
	hasWaiting := false
	hasRunning := false
	allDead := true
	for _, s := range t.Sessions {
		switch s.Status {
		case StatusWaiting:
			hasWaiting = true
			allDead = false
		case StatusRunning:
			hasRunning = true
			allDead = false
		case StatusIdle:
			allDead = false
		}
	}
	if allDead {
		return StatusDead
	}
	if hasWaiting {
		return StatusWaiting
	}
	if hasRunning {
		return StatusRunning
	}
	return StatusIdle
}

func (s *AppState) ActiveSession() *Session {
	for _, p := range s.Projects {
		for _, sess := range p.Sessions {
			if sess.ID == s.ActiveSessionID {
				return sess
			}
		}
		for _, t := range p.Teams {
			for _, sess := range t.Sessions {
				if sess.ID == s.ActiveSessionID {
					return sess
				}
			}
		}
	}
	return nil
}

func (s *AppState) ActiveProject() *Project {
	for _, p := range s.Projects {
		if p.ID == s.ActiveProjectID {
			return p
		}
	}
	return nil
}
