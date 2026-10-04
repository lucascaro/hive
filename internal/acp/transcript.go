package acp

import (
	"strings"
	"unicode/utf8"

	"github.com/lucascaro/hive/internal/wire"
)

// Transcript bounds. A snapshot travels as one ACP_TRANSCRIPT frame, and
// frames are capped at wire.MaxPayload (1 MiB), so the text it holds is
// budgeted by its JSON-ESCAPED size — a quote or newline costs two bytes
// on the wire and an ESC or '<' six — well under that; the oldest items
// fall off first. A single message longer than maxItemText raw bytes
// keeps its beginning.
//
// Worst case, by construction: the text budget (512 KiB) or one item at
// six times maxItemText (384 KiB), whichever is larger, plus per-item JSON
// framing for MaxTranscriptItems items (~100 bytes each) — under 1 MiB.
// TestSnapshotFitsFrameLimitWorstCase holds this.
//
// ponytail: in-memory, no persistence. After a daemon restart the
// transcript is rebuilt from the agent's own session/load replay — the
// only copy that also holds turns typed in a PTY takeover. Persist a
// cache only if adapters prove to replay too little (tool and plan
// rows); the gate B run log records what each one replays.
const (
	MaxTranscriptItems = 2000
	maxTranscriptText  = 512 << 10 // escaped bytes
	maxItemText        = 64 << 10  // raw bytes
)

// truncatedMark ends a message cut at maxItemText.
const truncatedMark = "\n\n[… truncated by Hive]"

// Transcript folds session/update notifications into wire.AcpItems:
// message chunks coalesce into one item per message, and tool call
// updates merge into the item that opened the call. Not safe for
// concurrent use; the registry guards it with r.mu.
type Transcript struct {
	epoch  int
	nextID int
	items  []wire.AcpItem
	text   int // total itemText (escaped bytes) over items
	// open accumulates the text of the message being streamed, the item
	// whose ID is openID, so each chunk costs its own length rather than
	// a copy of everything said so far. Item.Text views its buffer
	// (Builder.String does not copy, and never rewrites written bytes).
	open   strings.Builder
	openID int
}

// Epoch identifies the current transcript generation.
func (t *Transcript) Epoch() int { return t.epoch }

// Reset empties the transcript and starts a new epoch, for a new
// adapter process whose session/load will replay the history.
func (t *Transcript) Reset() {
	t.epoch++
	t.items = nil
	t.text = 0
	t.open, t.openID = strings.Builder{}, 0
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
	return t.add(wire.AcpItem{Kind: wire.AcpItemUser, Text: clip(text), Origin: origin})
}

// Apply folds in one update and returns the deltas to broadcast — items
// created or changed, as copies, with a streamed chunk marked Append —
// or nil when it changed nothing.
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
				title := clip(u.Title)
				t.text += escLen(title) - escLen(it.Title)
				it.Title = title
			}
			if u.Kind != "" {
				it.ToolKind = u.Kind
			}
			if u.Status != "" {
				it.Status = u.Status
			}
			out := copyItem(*it)
			t.trim()
			return []wire.AcpItem{out}
		}
		return []wire.AcpItem{t.add(wire.AcpItem{
			Kind: wire.AcpItemTool, ToolCallID: u.ToolCallID,
			Title: clip(u.Title), ToolKind: u.Kind, Status: u.Status,
		})}
	case UpdatePlan:
		plan := make([]wire.AcpPlanEntry, len(u.Entries))
		for i, e := range u.Entries {
			plan[i] = wire.AcpPlanEntry{Content: clip(e.Content), Priority: e.Priority, Status: e.Status}
		}
		// One plan item per turn: a plan update is the whole plan, so it
		// replaces this turn's plan rather than stacking a copy per step.
		for i := len(t.items) - 1; i >= 0 && t.items[i].Kind != wire.AcpItemUser; i-- {
			if t.items[i].Kind == wire.AcpItemPlan {
				t.text += planText(plan) - planText(t.items[i].Plan)
				t.items[i].Plan = plan
				out := copyItem(t.items[i])
				t.trim()
				return []wire.AcpItem{out}
			}
		}
		return []wire.AcpItem{t.add(wire.AcpItem{Kind: wire.AcpItemPlan, Plan: plan})}
	}
	return nil
}

// chunk appends streamed text to the open message of this kind, or
// opens a new one. An append is broadcast as just the new text.
func (t *Transcript) chunk(kind string, u Update, origin string) []wire.AcpItem {
	text, ok := u.TextChunk()
	if !ok {
		return nil
	}
	if n := len(t.items); n > 0 && t.items[n-1].Kind == kind {
		it := &t.items[n-1]
		room := maxItemText - len(it.Text)
		if room <= 0 {
			return nil // already cut; the rest of this message is dropped
		}
		add := text
		if len(add) > room {
			add = add[:room] + truncatedMark
		}
		if t.openID != it.ID {
			t.open = strings.Builder{}
			t.open.WriteString(it.Text)
			t.openID = it.ID
		}
		t.open.WriteString(add)
		it.Text = t.open.String()
		t.text += escLen(add)
		delta := wire.AcpItem{ID: it.ID, Kind: it.Kind, Origin: it.Origin, Text: add, Append: true}
		t.trim()
		return []wire.AcpItem{delta}
	}
	return []wire.AcpItem{t.add(wire.AcpItem{Kind: kind, Text: clip(text), Origin: origin})}
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
	t.text += itemText(it)
	out := copyItem(it)
	t.trim()
	return out
}

// trim drops the oldest items until both budgets hold. The newest item
// always stays, whatever its size (clip bounds it).
func (t *Transcript) trim() {
	drop := 0
	for len(t.items)-drop > 1 && (len(t.items)-drop > MaxTranscriptItems || t.text > maxTranscriptText) {
		t.text -= itemText(t.items[drop])
		drop++
	}
	if drop > 0 {
		t.items = append([]wire.AcpItem(nil), t.items[drop:]...)
	}
}

// clip bounds one string at maxItemText, keeping its beginning. It may
// cut inside a UTF-8 sequence; encoding/json then writes U+FFFD there.
func clip(s string) string {
	if len(s) > maxItemText {
		return s[:maxItemText] + truncatedMark
	}
	return s
}

func itemText(it wire.AcpItem) int { return escLen(it.Text) + escLen(it.Title) + planText(it.Plan) }

func planText(p []wire.AcpPlanEntry) int {
	n := 0
	for _, e := range p {
		n += escLen(e.Content)
	}
	return n
}

// escLen is the length of s once encoding/json has escaped it (without
// the quotes): its HTML-safe default escapes <, > and & to \u003c-style
// six-byte sequences, as it does control characters other than \n \r
// \t and U+2028/U+2029; each byte of invalid UTF-8 becomes U+FFFD.
func escLen(s string) int {
	n := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch {
			case c == '"' || c == '\\' || c == '\n' || c == '\r' || c == '\t':
				n += 2
			case c < 0x20 || c == '<' || c == '>' || c == '&':
				n += 6
			default:
				n++
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			n += 3 // written as the raw UTF-8 of U+FFFD
		case r == '\u2028', r == '\u2029':
			n += 6
		default:
			n += size
		}
		i += size
	}
	return n
}

func copyItem(it wire.AcpItem) wire.AcpItem {
	if it.Plan != nil {
		it.Plan = append([]wire.AcpPlanEntry(nil), it.Plan...)
	}
	return it
}
