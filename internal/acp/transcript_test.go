package acp

import (
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

func text(kind, s string) Update {
	return Update{SessionUpdate: kind, Content: &ContentBlock{Type: "text", Text: s}}
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
