package acp

import "github.com/lucascaro/hive/internal/wire"

// MaxTranscriptItems caps one session's in-memory transcript; the
// oldest items fall off first.
//
// ponytail: in-memory ring, no persistence. After a daemon restart the
// transcript is rebuilt from the agent's own session/load replay — the
// only copy that also holds turns typed in a PTY takeover. Persist a
// cache only if adapters prove to replay too little (tool and plan
// rows); the gate B run log records what each one replays.
const MaxTranscriptItems = 2000

// Transcript folds session/update notifications into wire.AcpItems:
// message chunks coalesce into one item per message, and tool call
// updates merge into the item that opened the call. Not safe for
// concurrent use; the registry guards it with r.mu.
type Transcript struct {
	epoch  int
	nextID int
	items  []wire.AcpItem
}

// Epoch identifies the current transcript generation.
func (t *Transcript) Epoch() int { return t.epoch }

// Reset empties the transcript and starts a new epoch, for a new
// adapter process whose session/load will replay the history.
func (t *Transcript) Reset() {
	t.epoch++
	t.items = nil
}

// Snapshot returns a copy of every item.
func (t *Transcript) Snapshot() []wire.AcpItem {
	out := make([]wire.AcpItem, len(t.items))
	for i, it := range t.items {
		out[i] = copyItem(it)
	}
	return out
}

// AddUser records a prompt Hive sent, with who sent it.
func (t *Transcript) AddUser(text, origin string) wire.AcpItem {
	return t.add(wire.AcpItem{Kind: wire.AcpItemUser, Text: text, Origin: origin})
}

// Apply folds in one update and returns the items it created or
// changed (copies), or nil when it changed nothing.
//
// user_message_chunk is honoured only while replaying a session/load:
// a live prompt is already recorded by AddUser, with its origin, and an
// adapter that echoes it back must not add it twice.
func (t *Transcript) Apply(u Update, replaying bool) []wire.AcpItem {
	switch u.SessionUpdate {
	case UpdateUserMessage:
		if !replaying {
			return nil
		}
		return t.chunk(wire.AcpItemUser, u, wire.OriginReplayed)
	case UpdateAgentMessage:
		return t.chunk(wire.AcpItemAgent, u, "")
	case UpdateAgentThought:
		return t.chunk(wire.AcpItemThought, u, "")
	case UpdateToolCall, UpdateToolCallPatch:
		if i := t.toolIndex(u.ToolCallID); i >= 0 {
			it := &t.items[i]
			if u.Title != "" {
				it.Title = u.Title
			}
			if u.Kind != "" {
				it.ToolKind = u.Kind
			}
			if u.Status != "" {
				it.Status = u.Status
			}
			return []wire.AcpItem{copyItem(*it)}
		}
		return []wire.AcpItem{t.add(wire.AcpItem{
			Kind: wire.AcpItemTool, ToolCallID: u.ToolCallID,
			Title: u.Title, ToolKind: u.Kind, Status: u.Status,
		})}
	case UpdatePlan:
		plan := make([]wire.AcpPlanEntry, len(u.Entries))
		for i, e := range u.Entries {
			plan[i] = wire.AcpPlanEntry{Content: e.Content, Priority: e.Priority, Status: e.Status}
		}
		// One plan item per turn: a plan update is the whole plan, so it
		// replaces this turn's plan rather than stacking a copy per step.
		for i := len(t.items) - 1; i >= 0 && t.items[i].Kind != wire.AcpItemUser; i-- {
			if t.items[i].Kind == wire.AcpItemPlan {
				t.items[i].Plan = plan
				return []wire.AcpItem{copyItem(t.items[i])}
			}
		}
		return []wire.AcpItem{t.add(wire.AcpItem{Kind: wire.AcpItemPlan, Plan: plan})}
	}
	return nil
}

func (t *Transcript) chunk(kind string, u Update, origin string) []wire.AcpItem {
	if u.Content == nil || u.Content.Type != "text" || u.Content.Text == "" {
		return nil
	}
	if n := len(t.items); n > 0 && t.items[n-1].Kind == kind {
		t.items[n-1].Text += u.Content.Text
		return []wire.AcpItem{copyItem(t.items[n-1])}
	}
	return []wire.AcpItem{t.add(wire.AcpItem{Kind: kind, Text: u.Content.Text, Origin: origin})}
}

func (t *Transcript) toolIndex(id string) int {
	if id == "" {
		return -1
	}
	for i := len(t.items) - 1; i >= 0; i-- {
		if t.items[i].Kind == wire.AcpItemTool && t.items[i].ToolCallID == id {
			return i
		}
	}
	return -1
}

func (t *Transcript) add(it wire.AcpItem) wire.AcpItem {
	t.nextID++
	it.ID = t.nextID
	t.items = append(t.items, it)
	if over := len(t.items) - MaxTranscriptItems; over > 0 {
		t.items = append([]wire.AcpItem(nil), t.items[over:]...)
	}
	return copyItem(it)
}

func copyItem(it wire.AcpItem) wire.AcpItem {
	if it.Plan != nil {
		it.Plan = append([]wire.AcpPlanEntry(nil), it.Plan...)
	}
	return it
}
