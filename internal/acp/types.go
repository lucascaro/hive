package acp

import "encoding/json"

// ProtocolVersion is the ACP major version this client speaks.
const ProtocolVersion = 1

// ACP method names.
const (
	MethodInitialize        = "initialize"
	MethodSessionNew        = "session/new"
	MethodSessionLoad       = "session/load"
	MethodSessionPrompt     = "session/prompt"
	MethodSessionUpdate     = "session/update"
	MethodRequestPermission = "session/request_permission"
	MethodSessionSetMode    = "session/set_mode"
)

type fsCapability struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

type clientCapabilities struct {
	FS       fsCapability `json:"fs"`
	Terminal bool         `json:"terminal"`
}

type initializeParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities clientCapabilities `json:"clientCapabilities"`
}

// AgentCapabilities is the part of initialize's result Hive reads.
type AgentCapabilities struct {
	LoadSession bool `json:"loadSession"`
}

// InitializeResult is initialize's result.
type InitializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
}

// EnvVar is one environment variable for an MCP server.
type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// MCPServer is a stdio MCP server the agent should start for the
// session. Sent on session/new and again on every session/load — an
// adapter does not remember them across processes.
type MCPServer struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Env     []EnvVar `json:"env"`
}

type newSessionParams struct {
	Cwd        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers"`
}

// Mode is one of the session's permission/behaviour modes.
type Mode struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Modes is the session's mode state.
type Modes struct {
	CurrentModeID  string `json:"currentModeId"`
	AvailableModes []Mode `json:"availableModes"`
}

// NewSessionResult is session/new's result.
type NewSessionResult struct {
	SessionID string `json:"sessionId"`
	Modes     *Modes `json:"modes,omitempty"`
}

type loadSessionParams struct {
	SessionID  string      `json:"sessionId"`
	Cwd        string      `json:"cwd"`
	MCPServers []MCPServer `json:"mcpServers"`
}

// LoadSessionResult is session/load's result.
type LoadSessionResult struct {
	Modes *Modes `json:"modes,omitempty"`
}

type setModeParams struct {
	SessionID string `json:"sessionId"`
	ModeID    string `json:"modeId"`
}

// ContentBlock is a prompt or message content block. Hive sends and
// renders text only (images are a spec 496 non-goal).
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type promptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

type promptResult struct {
	StopReason string `json:"stopReason"`
}

// SessionNotification is the session/update notification's params.
type SessionNotification struct {
	SessionID string          `json:"sessionId"`
	Update    json.RawMessage `json:"update"`
}

// Update is one decoded session/update payload. Fields are the union
// of the kinds Hive renders; SessionUpdate says which apply.
type Update struct {
	SessionUpdate string `json:"sessionUpdate"`
	// Content is raw because its shape depends on SessionUpdate: one
	// ContentBlock on a message or thought chunk, an array of
	// ToolCallContent on tool_call / tool_call_update. Read it with
	// TextChunk.
	Content json.RawMessage `json:"content,omitempty"`
	// tool_call / tool_call_update
	ToolCallID string `json:"toolCallId,omitempty"`
	Title      string `json:"title,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Status     string `json:"status,omitempty"`
	// Name, ToolName and Meta are a tool_call's identity, where the
	// adapter reports it; a later permission request for the same
	// ToolCallID is matched against it (see ToolIdentity).
	Name     string          `json:"name,omitempty"`
	ToolName string          `json:"toolName,omitempty"`
	Meta     json.RawMessage `json:"_meta,omitempty"`
	// plan
	Entries []PlanEntry `json:"entries,omitempty"`
	// current_mode_update
	CurrentModeID string `json:"currentModeId,omitempty"`
}

// TextChunk returns the text of a message or thought chunk's content
// block, and false when the update carries no text block.
func (u Update) TextChunk() (string, bool) {
	var b ContentBlock
	if len(u.Content) == 0 || json.Unmarshal(u.Content, &b) != nil || b.Type != "text" || b.Text == "" {
		return "", false
	}
	return b.Text, true
}

// PlanEntry is one step of a plan update.
type PlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority,omitempty"`
	Status   string `json:"status,omitempty"`
}

// session/update kinds Hive renders. Others (usage_update,
// available_commands_update, …) are ignored.
const (
	UpdateUserMessage   = "user_message_chunk"
	UpdateAgentMessage  = "agent_message_chunk"
	UpdateAgentThought  = "agent_thought_chunk"
	UpdateToolCall      = "tool_call"
	UpdateToolCallPatch = "tool_call_update"
	UpdatePlan          = "plan"
	UpdateCurrentMode   = "current_mode_update"
)

// PermissionOption is one answer the agent offers.
type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// PermissionToolCall is the tool call a permission request is about.
// Name, ToolName and Meta carry the tool's identity where an adapter
// reports it (Claude: name = mcp__<server>__<tool>); ToolIdentity
// matches on them. RawInput is the tool's arguments — model-written,
// kept for display only, never an identity.
type PermissionToolCall struct {
	ToolCallID string          `json:"toolCallId"`
	Title      string          `json:"title,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Meta       json.RawMessage `json:"_meta,omitempty"`
	RawInput   json.RawMessage `json:"rawInput,omitempty"`
}

// PermissionRequest is session/request_permission's params.
type PermissionRequest struct {
	SessionID string             `json:"sessionId"`
	ToolCall  PermissionToolCall `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

type permissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}

type permissionResponse struct {
	Outcome permissionOutcome `json:"outcome"`
}

// Selected answers a permission request with optionID.
func Selected(optionID string) any {
	return permissionResponse{Outcome: permissionOutcome{Outcome: "selected", OptionID: optionID}}
}

// Cancelled answers a permission request as cancelled — the session is
// going away, or the answer can no longer be given.
func Cancelled() any {
	return permissionResponse{Outcome: permissionOutcome{Outcome: "cancelled"}}
}
