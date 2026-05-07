package wire

import (
	"encoding/json"
	"fmt"
	"io"
)

// Mode is the connection mode chosen by the client in HELLO. It
// determines the server-side dispatch and which frame types are
// allowed on the connection for the rest of its lifetime.
type Mode string

const (
	ModeControl Mode = "control" // session management; never streams DATA
	ModeAttach  Mode = "attach"  // attach to an existing session by ID
	ModeCreate  Mode = "create"  // create a new session, then behave as attach
)

// CreateSpec is the payload for ModeCreate's create field, and also
// the standalone CREATE_SESSION control frame.
type CreateSpec struct {
	Name      string `json:"name,omitempty"`
	Color     string `json:"color,omitempty"`
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
	Shell     string `json:"shell,omitempty"`
	Cwd       string `json:"cwd,omitempty"`        // working directory; falls back to project cwd
	Agent     string `json:"agent,omitempty"`      // canonical agent ID, e.g. "claude"; empty = generic shell
	ProjectID string `json:"project_id,omitempty"` // owning project; empty = default project
	// Cmd, when set, runs in place of the shell. Phase 3 uses this
	// for agent launchers when Agent is set, but the daemon also
	// accepts a raw Cmd from clients that don't speak agent IDs.
	Cmd []string `json:"cmd,omitempty"`

	// UseWorktree, when true and the resolved cwd is a git repo,
	// makes the daemon create a fresh git worktree under
	// <gitRoot>/.worktrees/ and run the session inside it.
	UseWorktree bool `json:"use_worktree,omitempty"`
	// Branch is an optional branch name for the worktree. When empty,
	// a random adjective-noun is generated.
	Branch string `json:"branch,omitempty"`
}

// Hello is the first frame the client sends after connecting.
type Hello struct {
	Version int    `json:"version"`
	Client  string `json:"client"` // free-form, e.g. "hive/0.2.0"
	// BuildID is the client's link-time build identity (see
	// internal/buildinfo). Omitempty so an older client talking to a
	// newer daemon still parses cleanly; "" means "unknown".
	BuildID string `json:"build_id,omitempty"`

	// v1 fields:
	Mode      Mode        `json:"mode,omitempty"`
	SessionID string      `json:"session_id,omitempty"` // ModeAttach
	Create    *CreateSpec `json:"create,omitempty"`     // ModeCreate
}

// Welcome is the server's response to Hello. For attach/create modes
// it announces the active session and PTY dimensions so the client
// can size its terminal widget before live data flows. For control
// mode SessionID is empty.
type Welcome struct {
	Version int `json:"version"`
	// BuildID is the daemon's link-time build identity. Same shape
	// and semantics as Hello.BuildID — clients compare to detect a
	// stale daemon that survived a GUI rebuild.
	BuildID   string `json:"build_id,omitempty"`
	Mode      Mode   `json:"mode,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
}

// SessionInfo is the public-facing description of one daemon session.
// It is what the client sees in SESSIONS and SESSION_EVENT payloads.
type SessionInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Color          string `json:"color"`
	Order          int    `json:"order"`
	Created        string `json:"created"` // RFC 3339
	Alive          bool   `json:"alive"`
	Agent          string `json:"agent,omitempty"`           // canonical agent ID, "" = generic shell
	ProjectID      string `json:"project_id,omitempty"`      // owning project; "" = unassigned/legacy
	WorktreePath   string `json:"worktree_path,omitempty"`   // absolute path; "" = no worktree
	WorktreeBranch string `json:"worktree_branch,omitempty"` // branch backing the worktree
	LastError      string `json:"last_error,omitempty"`      // human-readable error from last failed Start/Revive
}

// ListSessionsReq is the LIST_SESSIONS payload (currently empty).
type ListSessionsReq struct{}

// SessionsResp is the SESSIONS payload returned in response to LIST_SESSIONS.
type SessionsResp struct {
	Sessions []SessionInfo `json:"sessions"`
}

// KillSessionReq is the KILL_SESSION payload. Force=true tells the
// daemon to skip the dirty-worktree safety check and discard
// uncommitted changes.
type KillSessionReq struct {
	SessionID string `json:"session_id"`
	Force     bool   `json:"force,omitempty"`
}

// RestartSessionReq is the RESTART_SESSION payload. The daemon
// terminates the agent process in place (preserving the session
// entry, its name/color/order/worktree) and respawns it using the
// agent's ResumeCmd if defined, otherwise its Cmd.
type RestartSessionReq struct {
	SessionID string `json:"session_id"`
}

// UpdateSessionReq mutates session metadata. Pointer fields are
// "omit if not setting". Order is *int because 0 is a valid value
// and we need to distinguish "no change" from "set to zero".
type UpdateSessionReq struct {
	SessionID string  `json:"session_id"`
	Name      *string `json:"name,omitempty"`
	Color     *string `json:"color,omitempty"`
	Order     *int    `json:"order,omitempty"`
	ProjectID *string `json:"project_id,omitempty"` // reassign session
}

// SessionEventKind enumerates the kinds carried by SESSION_EVENT.
const (
	SessionEventAdded   = "added"
	SessionEventRemoved = "removed"
	SessionEventUpdated = "updated"
)

// SessionEvent is the SESSION_EVENT payload, broadcast to every
// control connection on any registry change.
type SessionEvent struct {
	Kind    string      `json:"kind"`
	Session SessionInfo `json:"session"`
}

// --- Phase 4: projects ---

// ProjectInfo is the public-facing description of one project.
type ProjectInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Color   string `json:"color"`
	Cwd     string `json:"cwd,omitempty"`
	Order   int    `json:"order"`
	Created string `json:"created"` // RFC 3339
}

// ListProjectsReq is the LIST_PROJECTS payload (currently empty).
type ListProjectsReq struct{}

// ProjectsResp is the PROJECTS payload returned in response to
// LIST_PROJECTS or pushed unsolicited as the initial control snapshot.
type ProjectsResp struct {
	Projects []ProjectInfo `json:"projects"`
}

// CreateProjectReq is the CREATE_PROJECT payload.
type CreateProjectReq struct {
	Name  string `json:"name,omitempty"`
	Color string `json:"color,omitempty"`
	Cwd   string `json:"cwd,omitempty"`
}

// KillProjectReq is the KILL_PROJECT payload. KillSessions=true kills
// all sessions in the project; otherwise they are reassigned to the
// default project (and thus survive the project removal).
type KillProjectReq struct {
	ProjectID    string `json:"project_id"`
	KillSessions bool   `json:"kill_sessions,omitempty"`
}

// UpdateProjectReq mutates project metadata. Pointer fields opt in.
type UpdateProjectReq struct {
	ProjectID string  `json:"project_id"`
	Name      *string `json:"name,omitempty"`
	Color     *string `json:"color,omitempty"`
	Cwd       *string `json:"cwd,omitempty"`
	Order     *int    `json:"order,omitempty"`
}

// ProjectEventKind enumerates the kinds carried by PROJECT_EVENT.
const (
	ProjectEventAdded   = "added"
	ProjectEventRemoved = "removed"
	ProjectEventUpdated = "updated"
)

// ProjectEvent is the PROJECT_EVENT payload, broadcast to every
// control connection on any project change.
type ProjectEvent struct {
	Kind    string      `json:"kind"`
	Project ProjectInfo `json:"project"`
}

// Resize is sent by the client whenever its terminal widget changes
// size. The server forwards the new dimensions to the PTY.
type Resize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// Event covers asynchronous server-to-client notifications.
type Event struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Well-known event kinds.
const (
	EventScrollbackReplayDone = "scrollback_replay_done"
	EventSessionExit          = "session_exit"
)

// Error is sent by the server when something goes wrong.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// SessionID, when non-empty, links the error to a specific
	// session — used by clients (e.g. dirty-worktree confirm) to
	// know which session to retry.
	SessionID string `json:"session_id,omitempty"`
}

// Well-known error codes.
const (
	ErrCodeWorktreeDirty = "worktree_dirty"
)

// WriteJSON marshals v and writes it as a frame of type t.
func WriteJSON(w io.Writer, t FrameType, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("wire: marshal %s: %w", t, err)
	}
	return WriteFrame(w, t, b)
}

// ReadJSON reads the next frame and unmarshals its payload into v.
// Returns the actual frame type so the caller can distinguish unexpected
// messages without re-reading.
func ReadJSON(r io.Reader, v any) (FrameType, error) {
	t, payload, err := ReadFrame(r)
	if err != nil {
		return 0, err
	}
	if v != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, v); err != nil {
			return t, fmt.Errorf("wire: unmarshal %s: %w", t, err)
		}
	}
	return t, nil
}
