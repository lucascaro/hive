package wire

import (
	"encoding/json"
	"errors"
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
	// ModeEvent is a one-shot connection used by an agent's hook or
	// extension tier to report a state observation (`hived hook`, or
	// the Pi extension). Exactly one FrameAgentEvent frame is read and
	// applied, then the connection is closed — there is no Welcome and
	// the connection never streams DATA.
	ModeEvent Mode = "event"
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
	Release string `json:"release,omitempty"`
	// DaemonContract is the daemon's compatibility generation (see
	// internal/buildinfo.DaemonContract). A client compares it to its
	// own to decide whether a cheap GUI-only reload is enough or a
	// full daemon restart is required. Omitempty so a daemon
	// predating this field still parses; 0 means "unknown", which
	// clients must treat as "restart required" — never as a match.
	DaemonContract int    `json:"daemon_contract,omitempty"`
	Mode           Mode   `json:"mode,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	Cols           int    `json:"cols,omitempty"`
	Rows           int    `json:"rows,omitempty"`
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
	// Title is the window title the running program most recently set
	// via OSC 0 / OSC 2 — what a shell or agent TUI publishes to say
	// what it is doing right now. Daemon-owned and in-memory only, like
	// Phase: it is read back off the session's VT mirror, never
	// persisted, so a daemon restart simply starts it empty again and a
	// session with no live process reports "". Capped at
	// MaxTitleLen bytes, since the content comes from the child process.
	Title string `json:"title,omitempty"`
	// NeedsAttention is true when the program on this session rang the
	// terminal bell and nobody has looked since. Daemon-owned and
	// in-memory like Phase and Title, so a daemon restart starts every
	// session quiet.
	//
	// It lives on the wire, rather than in each client's head, because
	// more than one client needs it and only one of them holds an
	// attach connection. A GUI window learns about a bell from the PTY
	// stream it is already reading; hivebar has no such stream, and a
	// second GUI window has its own. Deriving it per client meant three
	// answers to one question and no way for the menu bar to have any
	// answer at all.
	NeedsAttention bool `json:"needs_attention,omitempty"`

	// State is what the session is doing right now — see the State*
	// constants. Daemon-owned and in-memory like Phase, Title and
	// NeedsAttention, so a daemon restart starts every session idle.
	//
	// Empty means StateIdle. That is deliberate: it keeps the field
	// omitempty, and it means a client built against this generation
	// reads a daemon built before it as "everything idle" rather than
	// as "everything in an unknown state it must render specially".
	State string `json:"state,omitempty"`
	// StateSource names the tier that produced State — see the
	// StateSource* constants. Clients render the difference, because
	// "the agent told us it is waiting for permission" and "no bytes
	// arrived for two seconds" are not the same claim and should not
	// look the same. Empty means StateSourceHeuristic.
	StateSource string `json:"state_source,omitempty"`
	// LastPrompt is the first thing the user asked this session to do,
	// as reported by the agent. It answers "what is this one for" in a
	// list of ten. Empty when the agent reports nothing (every session
	// on the heuristic tier).
	//
	// Capped at MaxSummaryLen bytes at the boundary, like Title: the
	// content is whatever was typed at a prompt.
	LastPrompt string `json:"last_prompt,omitempty"`
	// LastSummary is what the agent said as it finished its most
	// recent turn, or the error it reported. Same capping rules and
	// same in-memory lifetime as LastPrompt.
	LastSummary string `json:"last_summary,omitempty"`
}

// MaxTitleLen bounds SessionInfo.Title. The title is attacker-influenced
// in the ordinary sense — any program on the PTY can set it to anything —
// and it is rebroadcast to every connected client on change, so it is
// truncated at the boundary rather than trusted. 256 bytes is far more
// than any sane title and far less than a problem.
const MaxTitleLen = 256

// MaxSummaryLen bounds SessionInfo.LastPrompt and LastSummary. Same
// reasoning as MaxTitleLen — the content is supplied by the child
// process or typed by the user and is rebroadcast to every connected
// client — with a larger budget, because a one-line summary of a turn
// is genuinely longer than a window title and is rendered in a
// tooltip rather than a row.
const MaxSummaryLen = 512

// Session states, carried by SessionInfo.State. The daemon owns them;
// clients render them.
//
// The set is deliberately small and answers exactly one question: does
// this session need me, and if not, is it still going? Anything finer
// belongs in LastSummary, which is text, not a state a client has to
// have an icon for.
const (
	// StateIdle is the steady state: alive, nothing running, nobody
	// waiting. It is the empty string so it is also what a client
	// reads from a daemon that predates the field.
	StateIdle = ""
	// StateWorking means the session is producing output or the agent
	// reported it is mid-turn.
	StateWorking = "working"
	// StateWaitingInput means the program wants something typed. On
	// the heuristic tier this is what a terminal bell means.
	StateWaitingInput = "waiting_input"
	// StateWaitingPermission means the agent is blocked on an explicit
	// yes/no. Only the hook and extension tiers can tell this apart
	// from StateWaitingInput; the distinction is the whole reason the
	// tiers exist.
	StateWaitingPermission = "waiting_permission"
	// StateExited means the child process is gone, whatever its exit
	// code. A shell that exits 1 is exited, not StateError.
	StateExited = "exited"
	// StateError is reserved for failures the agent itself reported.
	// Keeping it apart from StateExited is what stops a red dot from
	// meaning "a command returned non-zero once".
	StateError = "error"
)

// State tiers, carried by SessionInfo.StateSource, in increasing order
// of trust.
const (
	// StateSourceHeuristic is derived from the PTY alone: bytes
	// arrived, bytes stopped, a bell rang. Available for every
	// session including plain shells, and never more than a guess —
	// clients say so in the state icon's tooltip ("guessed from
	// terminal output"), which is the only place the tiers look
	// different. The empty string, so it is also what a pre-field
	// daemon reads as.
	StateSourceHeuristic = ""
	// StateSourceHook is reported by the agent's own hook mechanism
	// (Claude Code hooks calling `hived hook`).
	StateSourceHook = "hook"
	// StateSourceExtension is reported by an in-process agent
	// extension (the Hive-shipped Pi extension).
	StateSourceExtension = "extension"
)

// Session lifecycle phases, carried by SessionInfo.Phase. The daemon
// owns them; clients render them. They are in-memory only — nothing
// persists a phase, so a daemon restart can never strand a session in
// a transient one.
//
//	create:  starting → fetching → worktree → spawning → ready
//	kill:    ready → checking → closing → (removed)
//	restart: ready → restarting → ready
//	boot:    reviving → spawning → ready (sessions restored from disk)
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
	PhaseReviving   = "reviving"   // daemon boot: restored session waiting its turn to respawn
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

// RestoreSessionReq is the RESTORE_SESSION payload. SessionID is the
// id of a session closed earlier; empty means "the most recently
// closed one", which is what the reopen-last affordance sends so the
// client does not have to LIST_CLOSED first and then race a prune.
type RestoreSessionReq struct {
	SessionID string `json:"session_id,omitempty"`
}

// ListClosedReq is the LIST_CLOSED payload.
type ListClosedReq struct{}

// ClosedResp is the CLOSED payload: the sessions that can still be
// reopened, most recently closed first.
type ClosedResp struct {
	Closed []ClosedSessionInfo `json:"closed"`
}

// ClosedSessionInfo describes one restorable session. Deliberately
// thinner than SessionInfo — nothing here is live, so there is no
// phase, no alive flag and no title to report.
type ClosedSessionInfo struct {
	SessionID      string `json:"session_id"`
	Name           string `json:"name"`
	Color          string `json:"color,omitempty"`
	Agent          string `json:"agent,omitempty"`
	ProjectID      string `json:"project_id,omitempty"`
	WorktreeBranch string `json:"worktree_branch,omitempty"`
	ClosedAt       string `json:"closed_at"`
	// HasPatch reports that a recovery patch of the worktree's
	// uncommitted state was saved when this session was closed.
	HasPatch bool `json:"has_patch,omitempty"`
}

// RestoredResp is the SESSION_RESTORED payload: what the restore could
// NOT bring back. Sent after the session's "added" event so a client
// can render the tile first and then explain the gaps.
//
// Every field is a degradation, so an all-false payload means a clean
// undo. Scrollback is absent from this list because it is never
// restorable — the UI says so unconditionally rather than per-restore.
type RestoredResp struct {
	SessionID string `json:"session_id"`
	// ProjectReassigned: the original project was deleted, so the
	// session came back in the default project.
	ProjectReassigned bool `json:"project_reassigned,omitempty"`
	// WorktreeRecreated: the directory was gone and was rebuilt from
	// the surviving branch. Committed work is back; uncommitted is not.
	WorktreeRecreated bool `json:"worktree_recreated,omitempty"`
	// WorktreeLost: no worktree could be restored; the session runs in
	// the project directory instead.
	WorktreeLost bool `json:"worktree_lost,omitempty"`
	// ConversationLost: the agent had no pinned conversation id, so it
	// came back as a fresh conversation.
	ConversationLost bool `json:"conversation_lost,omitempty"`
	// AgentFellBack: the session's agent is no longer in the catalog
	// (a custom agent deleted since), so it came back as a shell.
	AgentFellBack bool `json:"agent_fell_back,omitempty"`
	// PatchPath is where the recovery patch for a deleted worktree was
	// saved, "" when there is none.
	PatchPath string `json:"patch_path,omitempty"`
	// PatchSkipped: there WAS uncommitted work at close time but it
	// exceeded the patch cap and was not saved. Distinct from an empty
	// PatchPath, which means nothing was at stake.
	PatchSkipped bool `json:"patch_skipped,omitempty"`
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
	// NeedsAttention clears (or, in principle, sets) the bell flag.
	// The client that focuses a session is the only thing that knows
	// the user has now looked at it, so clearing is a client-driven
	// update rather than something the daemon can infer.
	NeedsAttention *bool `json:"needs_attention,omitempty"`
}

// SessionEventKind enumerates the kinds carried by SESSION_EVENT.
const (
	SessionEventAdded   = "added"
	SessionEventRemoved = "removed"
	SessionEventUpdated = "updated"
	// SessionEventTitle reports that the program running on the session
	// changed its window title (SessionInfo.Title). Deliberately NOT an
	// "updated": that kind means the daemon's own view of the session
	// changed — a rename, a reorder, a phase transition, a death — and
	// clients treat it as authoritative state worth a full re-render. A
	// title change is neither authoritative nor rare: it is the child
	// process redrawing, at whatever rate it likes. Sharing a kind would
	// make every existing consumer tolerate that churn, and make the
	// event stream nondeterministic for anything asserting on it.
	//
	// Clients that do not know this kind ignore it and simply show no
	// titles, so the field and the kind are both additive.
	SessionEventTitle = "title"
	// SessionEventAttention reports that SessionInfo.NeedsAttention
	// changed. Kept apart from "updated" for the same reason "title"
	// is: it is driven by the child process, not by the daemon's own
	// view of the session, and consumers that re-render everything on
	// "updated" should not be made to do so on every bell.
	SessionEventAttention = "attention"
	// SessionEventState reports that SessionInfo.State (or any of the
	// text that travels with it) changed. Kept apart from "updated"
	// for the same reason "title" and "attention" are: it is driven by
	// the child process rather than by the daemon's own view of the
	// session, and it fires as often as an agent changes what it is
	// doing. Consumers that re-render the world on "updated" must not
	// be made to do so on every turn.
	SessionEventState = "state"
)

// AgentEvent is the payload of FrameAgentEvent — the sole frame of a
// ModeEvent connection. It carries one observation from an agent's
// hook or extension tier (`hived hook`, or the Pi extension) about
// what the session it is running is doing right now.
type AgentEvent struct {
	SessionID string `json:"session_id"`
	// Kind is one of AgentEventKinds; see agentstate.Machine.Apply for
	// what each does to the session's state.
	Kind string `json:"kind"`
	// Source names the reporting tier: StateSourceHook or
	// StateSourceExtension. Anything else is refused.
	Source string `json:"source"`
	// Text is prompt/summary/error text, capped at MaxSummaryLen by the
	// daemon (truncated, not rejected — same reasoning as Title).
	Text string `json:"text,omitempty"`
	// At is when the reporter observed this, RFC3339Nano. Empty or
	// unparseable falls back to the daemon's own time.Now() rather than
	// being refused — a clock the daemon does not control should not be
	// able to drop an otherwise-valid event.
	At string `json:"at,omitempty"`
}

// AgentEvent kinds. These are the wire spelling of agentstate.Kind*;
// kept as separate string constants (rather than importing agentstate,
// which would make the domain package depend on the wire format it is
// deliberately ignorant of) but must stay byte-identical to it.
const (
	AgentEventPrompt             = "prompt"
	AgentEventTurnEnd            = "turn_end"
	AgentEventWaitingInput       = "waiting_input"
	AgentEventWaitingPermission  = "waiting_permission"
	AgentEventPing               = "ping"
	AgentEventPermissionResolved = "permission_resolved"
	AgentEventError              = "error"
	AgentEventSessionEnd         = "session_end"
)

// AgentEventKinds is the validation allowlist for AgentEvent.Kind, the
// same pattern as ClientCommands: an unrecognised kind is refused
// rather than applied, since Apply's tolerant-parsing fallback (ping)
// is for a hook the machine already trusts, not for anything on the
// wire.
var AgentEventKinds = map[string]bool{
	AgentEventPrompt:             true,
	AgentEventTurnEnd:            true,
	AgentEventWaitingInput:       true,
	AgentEventWaitingPermission:  true,
	AgentEventPing:               true,
	AgentEventPermissionResolved: true,
	AgentEventError:              true,
	AgentEventSessionEnd:         true,
}

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
	// DeleteIdeas is the force flag for the project_has_ideas
	// refusal. Deleting a project always deletes its ideas — nothing
	// else can reach them once the project card is gone — so the
	// daemon refuses first when any are still open and the client
	// retries with this set after confirming.
	DeleteIdeas bool `json:"delete_ideas,omitempty"`
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

// --- Ideas ---

// MaxIdeaText bounds one idea's text. Generous for a note typed into
// a one-line sheet, small enough that the flat ideas/ directory stays
// cheap to load at boot.
const MaxIdeaText = 4 << 10 // 4 KiB

// Idea kinds. Validated daemon-side; an unknown kind is refused.
const (
	IdeaKindIdea     = "idea"
	IdeaKindBug      = "bug"
	IdeaKindFeedback = "feedback"
)

// IdeaKinds is the closed set IdeaKind* enumerates.
var IdeaKinds = map[string]bool{
	IdeaKindIdea:     true,
	IdeaKindBug:      true,
	IdeaKindFeedback: true,
}

// Idea statuses. "started" means a session was started from the idea;
// "done" is the user marking it handled. An idea outlives the session
// started from it — closing that session does not move it to done.
const (
	IdeaStatusOpen    = "open"
	IdeaStatusStarted = "started"
	IdeaStatusDone    = "done"
)

// IdeaStatuses is the closed set IdeaStatus* enumerates.
var IdeaStatuses = map[string]bool{
	IdeaStatusOpen:    true,
	IdeaStatusStarted: true,
	IdeaStatusDone:    true,
}

// IdeaInfo is the public-facing description of one idea. It is
// deliberately not the whole persisted record: registry.IdeaFile also
// carries external_ref, which nothing renders yet.
type IdeaInfo struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	Status    string `json:"status"`
	Created   string `json:"created"` // RFC 3339
	Updated   string `json:"updated"` // RFC 3339
	// SourceSessionID is the session the idea was filed from, when it
	// was filed from one. Only a provenance breadcrumb — the idea is
	// owned by the project and outlives that session.
	SourceSessionID string `json:"source_session_id,omitempty"`
	// SessionID is the session started from this idea, set together
	// with Status=started.
	SessionID string `json:"session_id,omitempty"`
}

// ListIdeasReq is the LIST_IDEAS payload. An empty ProjectID asks for
// every project's ideas.
type ListIdeasReq struct {
	ProjectID string `json:"project_id,omitempty"`
}

// IdeasResp is the IDEAS payload, newest first.
type IdeasResp struct {
	Ideas []IdeaInfo `json:"ideas"`
}

// AddIdeaReq is the ADD_IDEA payload.
//
// ProjectID is optional: when empty the daemon resolves it from the
// live registry entry for SessionID, so an idea filed after a session
// was reassigned lands in the project the session is in NOW rather
// than the one it spawned in. Neither field set is an error.
type AddIdeaReq struct {
	SessionID string `json:"session_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Kind      string `json:"kind,omitempty"` // empty = IdeaKindIdea
	Text      string `json:"text"`
}

// UpdateIdeaReq mutates one idea. Pointer fields opt in, matching
// UpdateProjectReq. There is no Kind (nothing re-kinds an idea) and
// SessionID rides along with a Status change to "started".
type UpdateIdeaReq struct {
	ID        string  `json:"id"`
	Text      *string `json:"text,omitempty"`
	Status    *string `json:"status,omitempty"`
	SessionID *string `json:"session_id,omitempty"`
}

// RemoveIdeaReq is the REMOVE_IDEA payload.
type RemoveIdeaReq struct {
	ID string `json:"id"`
}

// IdeaEventKind enumerates the kinds carried by IDEA_EVENT.
const (
	IdeaEventAdded   = "added"
	IdeaEventRemoved = "removed"
	IdeaEventUpdated = "updated"
)

// IdeaEvent is the IDEA_EVENT payload, broadcast to every control
// connection on any idea change.
type IdeaEvent struct {
	Kind string   `json:"kind"`
	Idea IdeaInfo `json:"idea"`
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
	// ProjectID is the same affordance one level up: it names the
	// project a refusal is about, so the single client-side
	// confirm-and-retry branch can serve both worktree_dirty (session
	// scoped) and project_has_ideas (project scoped).
	ProjectID string `json:"project_id,omitempty"`
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
	// ErrCodeProtocolVersionMismatch is returned INSTEAD of a WELCOME
	// when the client's HELLO names a different PROTOCOL_VERSION. The
	// daemon refuses the connection outright, so this is the client's
	// only signal — Handshake turns it into ErrProtocolMismatch.
	ErrCodeProtocolVersionMismatch = "protocol_version_mismatch"
	// ErrCodeIdeaTooLong is returned when an idea's text exceeds
	// MaxIdeaText. Rejected rather than truncated: a silently
	// half-saved note is worse than one the user is told to shorten.
	ErrCodeIdeaTooLong = "idea_too_long"
	// ErrCodeProjectHasIdeas is returned when deleting a project would
	// destroy ideas that are still open. Overridable by force
	// (KillProjectReq.DeleteIdeas) after the user confirms.
	ErrCodeProjectHasIdeas = "project_has_ideas"
)

// ErrProtocolMismatch wraps a handshake refused for speaking a
// different PROTOCOL_VERSION. Callers match it with errors.Is to tell
// "this daemon is too old/new to talk to" apart from an ordinary
// connection failure — the two need opposite remedies, and only the
// former is fixed by restarting the daemon.
var ErrProtocolMismatch = errors.New("wire: protocol version mismatch")

// ---------- client commands ----------

// ClientCommand is the payload of both CLIENT_COMMAND (one client
// asking) and CLIENT_BROADCAST (the daemon relaying to every control
// client, sender included).
//
// The daemon never acts on one of these. It validates Cmd against
// ClientCommands and forwards it verbatim — the semantics live
// entirely in the clients. That is deliberate: these verbs are about
// client-side UI state ("relaunch yourself", "bring this session
// forward"), which the daemon has no business knowing about.
type ClientCommand struct {
	Cmd string `json:"cmd"`
	// SessionID scopes a command to one session. Only meaningful for
	// CmdFocusSession; ignored otherwise.
	SessionID string `json:"session_id,omitempty"`
}

// Client command verbs.
const (
	// CmdReloadGUI asks every GUI window to relaunch its own process,
	// leaving the daemon and every running session untouched. This is
	// the cheap half of what used to be a single "Restart Hive" —
	// picking up new GUI code no longer has to kill the user's PTYs.
	CmdReloadGUI = "reload_gui"
	// CmdFocusSession asks the GUI to bring one session forward. Sent
	// by hivebar, which has no window of its own.
	CmdFocusSession = "focus_session"
	// CmdCheckUpdate asks the GUI to run an update check. Also
	// hivebar's: the GUI owns staging, verification and the bundle
	// swap, and is the thing being replaced, so the menu bar delegates
	// rather than duplicating any of it.
	CmdCheckUpdate = "check_update"
)

// ClientCommands is the allowlist the daemon validates against. An
// unrecognised verb is refused rather than relayed: the daemon is the
// only thing standing between one client and every other, so a typo
// must not become a frame that every window has to guess at.
var ClientCommands = map[string]bool{
	CmdReloadGUI:    true,
	CmdFocusSession: true,
	CmdCheckUpdate:  true,
}

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
	// Upstream is the branch's tracking ref ("origin/foo"), empty when
	// it tracks nothing. Clients use it to know whether there is a
	// remote branch that could also be deleted.
	Upstream string `json:"upstream,omitempty"`
	// Merged means the branch's work is already in the default ref,
	// including via a squash merge. Unpushed commits on a merged
	// branch are not lost work, so clients may offer removal without
	// the destructive confirm.
	Merged bool `json:"merged,omitempty"`
	// SessionIDs are the live sessions whose cwd is this worktree.
	// Non-empty ⇒ removal and rename are refused.
	SessionIDs []string `json:"session_ids,omitempty"`
	// Subject is the first line of the branch tip's commit message —
	// what the branch name alone never says. Empty for a detached
	// worktree, and for anything the branch listing did not cover.
	Subject string `json:"subject,omitempty"`
}

// BranchInfo describes a local branch with no worktree — the
// "orphaned branch" list, where a worktree can be re-created to pick
// the work back up.
type BranchInfo struct {
	Name     string `json:"name"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead,omitempty"`
	// Merged: reachable from the default ref, or merged into it by a
	// squash (detected by patch id, or by GitHub PR state).
	Merged bool `json:"merged,omitempty"`
	// Subject is the first line of the branch tip's commit message.
	Subject string `json:"subject,omitempty"`
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
	// DeleteRemote also deletes the branch on its remote. Requires
	// DeleteBranch — the remote copy is the last handle on the work
	// once the local ref is gone, so the two go together or not at all.
	DeleteRemote bool `json:"delete_remote,omitempty"`
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
	// DeleteRemote also deletes the branch on its remote (a push, so
	// it needs the network). Ignored for a branch that tracks nothing.
	DeleteRemote bool `json:"delete_remote,omitempty"`
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
