package acp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

func text(kind, s string) Update {
	b, _ := json.Marshal(ContentBlock{Type: "text", Text: s})
	return Update{SessionUpdate: kind, Content: b}
}

func TestChunksCoalesce(t *testing.T) {
	var tr Transcript
	tr.AddUser("hi", wire.OriginUser)
	tr.Apply(text(UpdateAgentMessage, "hel"), false)
	tr.Apply(text(UpdateAgentMessage, "lo"), false)
	tr.Apply(text(UpdateAgentThought, "hmm"), false)
	tr.Apply(text(UpdateAgentMessage, "!"), false)
	got := tr.Snapshot()
	want := []struct{ kind, text string }{{"user", "hi"}, {"agent", "hello"}, {"thought", "hmm"}, {"agent", "!"}}
	if len(got) != len(want) {
		t.Fatalf("items = %+v", got)
	}
	for i, w := range want {
		if got[i].Kind != w.kind || got[i].Text != w.text {
			t.Errorf("item %d = %s %q, want %s %q", i, got[i].Kind, got[i].Text, w.kind, w.text)
		}
	}
	if got[0].Origin != wire.OriginUser {
		t.Errorf("user origin = %q", got[0].Origin)
	}
}

// A live prompt is recorded by AddUser with its origin; an adapter
// echoing it back must not add it again. On load the echo IS the record.
func TestUserChunksOnlyCountWhileReplaying(t *testing.T) {
	var tr Transcript
	tr.AddUser("hi", wire.OriginUser)
	if tr.Apply(text(UpdateUserMessage, "hi"), false) != nil {
		t.Error("live user echo changed the transcript")
	}
	tr.Reset()
	tr.Apply(text(UpdateUserMessage, "old"), true)
	got := tr.Snapshot()
	if len(got) != 1 || got[0].Kind != wire.AcpItemUser || got[0].Origin != wire.OriginReplayed {
		t.Errorf("replayed = %+v, want one replayed user item", got)
	}
}

func TestToolCallUpdateMergesByID(t *testing.T) {
	var tr Transcript
	tr.Apply(Update{SessionUpdate: UpdateToolCall, ToolCallID: "t1", Title: "Read", Kind: "read", Status: "pending"}, false)
	ch := tr.Apply(Update{SessionUpdate: UpdateToolCallPatch, ToolCallID: "t1", Status: "completed"}, false)
	got := tr.Snapshot()
	if len(got) != 1 || got[0].Status != "completed" || got[0].Title != "Read" || got[0].ToolKind != "read" {
		t.Errorf("items = %+v, want one merged completed tool call", got)
	}
	if len(ch) != 1 || ch[0].ID != got[0].ID {
		t.Errorf("changed = %+v, want the merged item", ch)
	}
}

func TestPlanReplacesWithinTurn(t *testing.T) {
	var tr Transcript
	tr.AddUser("go", wire.OriginUser)
	tr.Apply(Update{SessionUpdate: UpdatePlan, Entries: []PlanEntry{{Content: "a"}}}, false)
	tr.Apply(Update{SessionUpdate: UpdatePlan, Entries: []PlanEntry{{Content: "a", Status: "completed"}, {Content: "b"}}}, false)
	tr.AddUser("again", wire.OriginUser)
	tr.Apply(Update{SessionUpdate: UpdatePlan, Entries: []PlanEntry{{Content: "c"}}}, false)
	plans := 0
	for _, it := range tr.Snapshot() {
		if it.Kind == wire.AcpItemPlan {
			plans++
		}
	}
	if plans != 2 {
		t.Errorf("plan items = %d, want one per turn (2)", plans)
	}
	if got := tr.Snapshot()[1].Plan; len(got) != 2 || got[0].Status != "completed" {
		t.Errorf("first turn's plan = %+v, want the latest update", got)
	}
}

func TestResetBumpsEpoch(t *testing.T) {
	var tr Transcript
	tr.AddUser("x", wire.OriginUser)
	e := tr.Epoch()
	tr.Reset()
	if tr.Epoch() == e || len(tr.Snapshot()) != 0 {
		t.Errorf("after Reset: epoch %d (was %d), items %d", tr.Epoch(), e, len(tr.Snapshot()))
	}
}

func TestCapDropsOldest(t *testing.T) {
	var tr Transcript
	for i := 0; i < MaxTranscriptItems+5; i++ {
		tr.AddUser("x", wire.OriginUser)
	}
	got := tr.Snapshot()
	if len(got) != MaxTranscriptItems || got[0].ID != 6 {
		t.Errorf("len %d first id %d, want %d and 6", len(got), got[0].ID, MaxTranscriptItems)
	}
}

func TestSnapshotIsACopy(t *testing.T) {
	var tr Transcript
	tr.Apply(Update{SessionUpdate: UpdatePlan, Entries: []PlanEntry{{Content: "a"}}}, false)
	s := tr.Snapshot()
	s[0].Plan[0].Content = "mutated"
	if tr.Snapshot()[0].Plan[0].Content != "a" {
		t.Error("Snapshot shares plan storage with the transcript")
	}
}

// A streamed chunk is broadcast as just that chunk, marked Append, so a
// long reply costs its length, not its length squared.
func TestChunkDeltaIsAppendOnly(t *testing.T) {
	var tr Transcript
	first := tr.Apply(text(UpdateAgentMessage, "hel"), false)
	second := tr.Apply(text(UpdateAgentMessage, "lo"), false)
	if len(first) != 1 || first[0].Append || first[0].Text != "hel" {
		t.Errorf("first delta = %+v, want the whole new item", first)
	}
	if len(second) != 1 || !second[0].Append || second[0].Text != "lo" || second[0].ID != first[0].ID {
		t.Errorf("second delta = %+v, want an append of \"lo\" to item %d", second, first[0].ID)
	}
	for _, it := range tr.Snapshot() {
		if it.Append {
			t.Error("a snapshot item is marked Append")
		}
	}
}

// However much the agent says, a snapshot must fit in one frame.
func TestSnapshotFitsFrameLimit(t *testing.T) {
	var tr Transcript
	big := strings.Repeat("x", 200<<10)
	for i := 0; i < 40; i++ {
		tr.AddUser("go", wire.OriginUser)
		tr.Apply(text(UpdateAgentMessage, big), false)
		tr.Apply(text(UpdateAgentMessage, big), false) // past maxItemText
		tr.Apply(Update{SessionUpdate: UpdateToolCall, ToolCallID: "t", Title: big}, false)
	}
	b, err := json.Marshal(wire.AcpTranscriptMsg{SessionID: "s", Reset: true, Items: tr.Snapshot()})
	if err != nil {
		t.Fatal(err)
	}
	if len(b) >= wire.MaxPayload {
		t.Errorf("snapshot is %d bytes, want < %d", len(b), wire.MaxPayload)
	}
	last := tr.Snapshot()[len(tr.Snapshot())-1]
	if last.Kind != wire.AcpItemTool {
		t.Errorf("newest item = %s, want the newest kept", last.Kind)
	}
}

func TestLongMessageIsTruncatedOnce(t *testing.T) {
	var tr Transcript
	tr.Apply(text(UpdateAgentMessage, strings.Repeat("a", maxItemText-1)), false)
	tr.Apply(text(UpdateAgentMessage, "bbb"), false)
	if d := tr.Apply(text(UpdateAgentMessage, "ccc"), false); d != nil {
		t.Errorf("chunk after the cut = %+v, want dropped", d)
	}
	got := tr.Snapshot()[0].Text
	if !strings.HasSuffix(got, truncatedMark) || strings.Count(got, truncatedMark) != 1 {
		t.Errorf("text tail = %q, want one truncation mark", got[len(got)-40:])
	}
}

// ACP sends tool_call content as an array of ToolCallContent, unlike a
// message chunk's single block. Decoding must not drop the update.
func TestToolCallWithArrayContentDecodes(t *testing.T) {
	raw := `{"sessionUpdate":"tool_call","toolCallId":"t1","title":"Read","kind":"read","status":"pending",` +
		`"content":[{"type":"content","content":{"type":"text","text":"x"}}]}`
	var u Update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("decode tool_call with array content: %v", err)
	}
	var tr Transcript
	got := tr.Apply(u, false)
	if len(got) != 1 || got[0].Kind != wire.AcpItemTool || got[0].Title != "Read" {
		t.Errorf("Apply = %+v, want one tool item", got)
	}
	if _, ok := u.TextChunk(); ok {
		t.Error("a tool call's array content read as a text chunk")
	}
}
