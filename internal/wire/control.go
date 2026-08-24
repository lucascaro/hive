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

	// ContinueConversation asks the agent to pick up its most recent
	// conversation in the resolved cwd instead of starting a fresh one
	// (claude --continue, codex resume --last). Path-scoped by nature,
	// so it is only meaningful together with WorktreePath — "open a
	// session in this worktree and carry on where I left off".
	ContinueConversation bool `json:"continue_conversation,omitempty"`

	// WorktreePath, when set, names an EXISTING worktree the session
	// should run in — the "resume this work" path from the worktree
	// browser. The daemon adopts that worktree's branch onto the new
	// entry, which is what keeps it claimed (an unclaimed worktree is
	// eligible for the startup reclaim). Mutually exclusive with
	// UseWorktree: this path never runs `git worktree add`.
	WorktreePath string `json:"worktree_path,omitempty"`

	// InsertAfterSessionID, when it names an existing session in the
	// same project as the new one, places the new session immediately
	// after it in the display order instead of appending it last.
	// Ignored (append) when empty, unknown, or owned by a different
	// project.
	InsertAfterSessionID string `json:"insert_after_session_id,omitempty"`
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
	BuildID string `json:"build_id,omitempty"`
	// Release is the daemon's human-readable release version (see
	// internal/buildinfo.Version) — e.g. "v0.4.2", or "dev" for an
	// unstamped build. Distinct from Version above, which is the
	// integer protocol version. Omitempty so a daemon predating this
	// field still parses; "" means "unknown".
	Release   string `json:"release,omitempty"`
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
	// Phase is the session's lifecycle phase. Empty means ready (the
	// steady state), which keeps the field omitempty on the wire and
	// makes every entry loaded from disk ready by default. See the
	// Phase* constants below.
	Phase string `json:"phase,omitempty"`
}

// Session lifecycle phases, carried by SessionInfo.Phase. The daemon
// owns them; clients render them. They are in-memory only — nothing
// persists a phase, so a daemon restart can never strand a session in
// a transient one.
//
//	create:  starting → fetching → worktree → spawning → ready
//	kill:    ready → checking → closing → (removed)
//	restart: ready → restarting → ready
//
// A session is attachable only when Alive is true AND Phase is ready.
// SESSION_EVENT(added) means "the entry exists", not "you may attach".
const (
	PhaseReady      = ""           // steady state; attachable when Alive
	PhaseStarting   = "starting"   // entry registered, nothing spawned yet
	PhaseFetching   = "fetching"   // git fetch origin, ahead of the worktree add
	PhaseWorktree   = "worktree"   // git worktree add + agent-config linking
	PhaseSpawning   = "spawning"   // forking the PTY / shell
	PhaseChecking   = "checking"   // kill: checking the worktree for uncommitted changes
	PhaseClosing    = "closing"    // kill: PTY teardown + worktree removal
	PhaseRestarting = "restarting" // restart: PTY recycled in place
)

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
	// RemoveWorktree deletes the session's worktree as part of the
	// close, instead of leaving it behind for the worktree browser.
	// Done daemon-side rather than as a follow-up REMOVE_WORKTREE from
	// the client: the worktree is occupied until this very session is
	// gone, so a second round trip would race its own teardown and be
	// refused as in-use.
	RemoveWorktree bool `json:"remove_worktree,omitempty"`
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
	// EventScrollbackReplayBegin precedes a replay (initial attach or a
	// client-requested replay). The client should reset its terminal
	// buffer on receipt so the incoming bytes paint a clean slate
	// instead of overlaying whatever's already on screen.
	EventScrollbackReplayBegin = "scrollback_replay_begin"
	EventScrollbackReplayDone  = "scrollback_replay_done"
	EventSessionExit           = "session_exit"
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
	// ErrCodeWorktreeInUse is returned when a worktree operation is
	// refused because a live session is running inside it. Unlike the
	// other two worktree codes this one is NOT overridable by force —
	// the client must close those sessions first.
	ErrCodeWorktreeInUse = "worktree_in_use"
	// ErrCodeWorktreeUnpushed is returned when a worktree holds
	// committed work that is not reachable from its upstream (or when
	// no comparison base resolved at all, which is treated the same
	// way). Overridable by force after the user confirms.
	ErrCodeWorktreeUnpushed = "worktree_unpushed"
	// ErrCodeBranchUnmerged is returned when deleting a branch would
	// discard commits that are not merged into the default ref.
	// Overridable by force after the user confirms.
	ErrCodeBranchUnmerged = "branch_unmerged"
	// ErrCodeSessionStarting is returned by an attach that arrives
	// while the session is still being created. The entry exists but
	// has no PTY yet; the client should wait for the SESSION_EVENT
	// that moves it to PhaseReady rather than treating it as dead.
	ErrCodeSessionStarting = "session_starting"
)

// ---------- worktree management ----------

// ListWorktreesReq asks for the worktree inventory of one project.
type ListWorktreesReq struct {
	ProjectID string `json:"project_id"`
}

// WorktreeInfo is one row of the worktree browser. Uncommitted /
// Unpushed / Unknown are the safety verdict: a worktree is disposable
// only when all three are zero-valued.
type WorktreeInfo struct {
	Path     string `json:"path"`
	Branch   string `json:"branch,omitempty"` // "" = detached HEAD
	Detached bool   `json:"detached,omitempty"`
	// IsMain marks the project's own checkout, which is listed for
	// context but can never be removed.
	IsMain      bool `json:"is_main,omitempty"`
	Uncommitted bool `json:"uncommitted,omitempty"`
	Unpushed    int  `json:"unpushed,omitempty"`
	// Unknown means the unpushed count could not be determined (no
	// upstream and no default ref). Clients must render it as
	// "unsafe to delete", never as clean.
	Unknown bool `json:"unknown,omitempty"`
	// SessionIDs are the live sessions whose cwd is this worktree.
	// Non-empty ⇒ removal and rename are refused.
	SessionIDs []string `json:"session_ids,omitempty"`
}

// BranchInfo describes a local branch with no worktree — the
// "orphaned branch" list, where a worktree can be re-created to pick
// the work back up.
type BranchInfo struct {
	Name     string `json:"name"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead,omitempty"`
	Merged   bool   `json:"merged,omitempty"`
}

// WorktreesResp is the daemon's answer to LIST_WORKTREES and to every
// successful worktree mutation.
type WorktreesResp struct {
	ProjectID string `json:"project_id"`
	// RepoRoot is the git root backing the project, "" when the
	// project cwd is not a git repository (in which case both lists
	// are empty and the client should say so rather than show an
	// empty browser).
	RepoRoot       string         `json:"repo_root,omitempty"`
	Worktrees      []WorktreeInfo `json:"worktrees,omitempty"`
	OrphanBranches []BranchInfo   `json:"orphan_branches,omitempty"`
}

// RemoveWorktreeReq deletes a worktree directory. Force overrides the
// dirty and unpushed refusals (never the in-use one). DeleteBranch
// additionally removes the branch the worktree was on — off by
// default, because `git worktree remove` leaves the ref behind and
// that ref is the user's last handle on the work.
type RemoveWorktreeReq struct {
	ProjectID    string `json:"project_id"`
	Path         string `json:"path"`
	Force        bool   `json:"force,omitempty"`
	DeleteBranch bool   `json:"delete_branch,omitempty"`
}

// CreateWorktreeReq materializes a worktree for a branch that already
// exists (the orphaned-branch case). A branch that does not exist yet
// is created from the upstream default ref, same as session creation.
type CreateWorktreeReq struct {
	ProjectID string `json:"project_id"`
	Branch    string `json:"branch"`
}

// RenameWorktreeReq renames both the branch and the directory, which
// stay coupled (the path is derived from the branch). Refused while a
// session lives in the worktree — moving the directory would leave
// that session's shell in a cwd that no longer exists.
type RenameWorktreeReq struct {
	ProjectID string `json:"project_id"`
	Path      string `json:"path"`
	NewBranch string `json:"new_branch"`
}

// DeleteBranchReq removes a local branch that has no worktree. Force
// is required for a branch holding commits that are not merged into
// the default ref — git refuses those outright, and the client asks
// before overriding.
type DeleteBranchReq struct {
	ProjectID string `json:"project_id"`
	Branch    string `json:"branch"`
	Force     bool   `json:"force,omitempty"`
}

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
