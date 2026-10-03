package wire

// Session kinds, carried by CreateSpec.Kind and SessionInfo.Kind. Empty
// means KindPTY on both, so every session persisted or created before
// spec 496 is a terminal session with no migration.
const (
	// KindPTY is a session whose agent runs in a pseudo-terminal the
	// GUI attaches to — every session before spec 496.
	KindPTY = "pty"
	// KindACP is a session the daemon drives over the Agent Client
	// Protocol (JSON-RPC over the adapter's stdio). It has no terminal:
	// clients render its transcript (GET_ACP_TRANSCRIPT) instead, and an
	// attach is refused with ErrCodeACPSession.
	KindACP = "acp"
)

// Prompt origins recorded on AcpItem.Origin for user turns.
const (
	// OriginUser is a prompt typed by the user in a Hive client.
	OriginUser = "user"
	// OriginReplayed marks a user turn the agent replayed on
	// session/load (daemon restart, hand-back). Its original origin is
	// not known: the daemon keeps no transcript of its own.
	OriginReplayed = "replayed"
)

// Kinds of AcpItem.
const (
	AcpItemUser    = "user"
	AcpItemAgent   = "agent"
	AcpItemThought = "thought"
	AcpItemPlan    = "plan"
	AcpItemTool    = "tool"
)

// AcpItem is one row of an ACP session's transcript. Message chunks are
// coalesced by the daemon, so one agent reply is one item, and a tool
// call's later updates are merged into the item that opened it.
type AcpItem struct {
	// ID is stable for the item's life within one Epoch; a delta that
	// carries an existing ID replaces that item.
	ID   int    `json:"id"`
	Kind string `json:"kind"`
	// Text is the message text (user, agent, thought) — Markdown as the
	// agent wrote it, so clients must render it as untrusted.
	Text string `json:"text,omitempty"`
	// Origin is who sent a user turn: OriginUser, a principal such as
	// "plugin:<id>", or OriginReplayed.
	Origin     string `json:"origin,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Title      string `json:"title,omitempty"`
	ToolKind   string `json:"tool_kind,omitempty"`
	// Status is the tool call's status (pending, in_progress,
	// completed, failed) as the agent reports it.
	Status string         `json:"status,omitempty"`
	Plan   []AcpPlanEntry `json:"plan,omitempty"`
	// Append, on a delta only, means Text is a chunk to add to the end
	// of the item with this ID rather than its whole text. Streaming a
	// reply re-sending everything said so far would cost the square of
	// its length; a snapshot (Reset) never carries it.
	Append bool `json:"append,omitempty"`
}

// AcpPlanEntry is one step of an agent's plan.
type AcpPlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
}

// AcpPermission is a tool-permission request the agent is blocked on.
type AcpPermission struct {
	// RequestID identifies this request; ANSWER_PERMISSION must echo
	// it, so an answer meant for an earlier request cannot satisfy a
	// later one.
	RequestID  string                `json:"request_id"`
	ToolCallID string                `json:"tool_call_id,omitempty"`
	Title      string                `json:"title,omitempty"`
	Options    []AcpPermissionOption `json:"options"`
}

// AcpPermissionOption is one answer the agent offers, e.g. allow_once.
type AcpPermissionOption struct {
	OptionID string `json:"option_id"`
	Name     string `json:"name"`
	// Kind is ACP's option kind: allow_once, allow_always, reject_once
	// or reject_always.
	Kind string `json:"kind"`
}

// AcpTranscriptMsg is the ACP_TRANSCRIPT payload: a snapshot (Reset)
// in reply to GET_ACP_TRANSCRIPT, or a delta broadcast as the
// transcript changes.
type AcpTranscriptMsg struct {
	SessionID string `json:"session_id"`
	// Epoch changes whenever the transcript is rebuilt from scratch
	// (a new adapter process: create, revive, restart). A client drops
	// any delta whose Epoch is not the one its snapshot carried, and
	// re-fetches.
	Epoch int `json:"epoch"`
	// Reset means Items is the whole transcript, not a delta.
	Reset bool      `json:"reset,omitempty"`
	Items []AcpItem `json:"items,omitempty"`
	// Permission is the request currently pending, or nil when none is.
	// Every message carries it, so a client simply overwrites.
	Permission *AcpPermission `json:"permission,omitempty"`
}

// GetAcpTranscriptReq is the GET_ACP_TRANSCRIPT payload.
type GetAcpTranscriptReq struct {
	SessionID string `json:"session_id"`
}

// PromptAcpReq is the PROMPT_ACP payload: one user turn.
type PromptAcpReq struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

// AnswerPermissionReq is the ANSWER_PERMISSION payload.
type AnswerPermissionReq struct {
	SessionID string `json:"session_id"`
	RequestID string `json:"request_id"`
	OptionID  string `json:"option_id"`
}

// Error codes for ACP sessions.
const (
	// ErrCodeACPSession: an attach (or other terminal operation)
	// reached an ACP session, which has no terminal.
	ErrCodeACPSession = "acp_session"
	// ErrCodeACPBusy: PROMPT_ACP while a turn is still running. ACP
	// allows one prompt in flight per session; the daemon rejects
	// rather than queues so a result can never attach to the wrong turn.
	ErrCodeACPBusy = "acp_busy"
	// ErrCodeNotACP: an ACP operation reached a terminal session.
	ErrCodeNotACP = "not_acp_session"
	// ErrCodePermissionStale: ANSWER_PERMISSION for a request that is no
	// longer pending, or with an option it did not offer.
	ErrCodePermissionStale = "permission_stale"
)
