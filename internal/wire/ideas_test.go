package wire

import (
	"encoding/json"
	"strings"
	"testing"
)

// The frame bytes are the protocol's identity — reassigning one
// silently reinterprets an old client's frames as a different command.
// A missing String() case also turns the daemon's "unexpected control
// frame" log into a bare number.
func TestIdeaFrameStrings(t *testing.T) {
	cases := map[FrameType]struct {
		b    byte
		name string
	}{
		FrameListIdeas:  {0x23, "LIST_IDEAS"},
		FrameIdeas:      {0x24, "IDEAS"},
		FrameAddIdea:    {0x25, "ADD_IDEA"},
		FrameUpdateIdea: {0x26, "UPDATE_IDEA"},
		FrameRemoveIdea: {0x27, "REMOVE_IDEA"},
		FrameIdeaEvent:  {0x28, "IDEA_EVENT"},
	}
	for ft, want := range cases {
		if byte(ft) != want.b {
			t.Errorf("%s = 0x%02x, want 0x%02x", want.name, byte(ft), want.b)
		}
		if ft.String() != want.name {
			t.Errorf("String() = %q, want %q", ft.String(), want.name)
		}
	}
}

// The GUI fans control frames out by event name; a frame with no
// mapping is silently dropped and the inbox never hears about it.
func TestIdeaControlEventNames(t *testing.T) {
	for ft, want := range map[FrameType]string{
		FrameIdeas:     "idea:list",
		FrameIdeaEvent: "idea:event",
	} {
		name, ok := ControlEventName(ft)
		if !ok {
			t.Fatalf("%s has no control event name", ft)
		}
		if name != want {
			t.Errorf("ControlEventName(%s) = %q, want %q", ft, name, want)
		}
	}
	// Request frames are client → server; they must NOT be fanned out.
	for _, ft := range []FrameType{
		FrameListIdeas,
		FrameAddIdea,
		FrameUpdateIdea,
		FrameRemoveIdea,
	} {
		if _, ok := ControlEventName(ft); ok {
			t.Errorf("%s is client→server but has a control event name", ft)
		}
	}
}

func TestIdeaJSONTags(t *testing.T) {
	cases := []struct {
		v    any
		want []string
	}{
		{IdeaInfo{ID: "i", ProjectID: "p", Kind: IdeaKindBug, Text: "t",
			Status: IdeaStatusStarted, Created: "c", Updated: "u",
			SourceSessionID: "s1", SessionID: "s2"},
			[]string{`"project_id"`, `"source_session_id"`, `"session_id"`, `"created"`, `"updated"`}},
		{AddIdeaReq{SessionID: "s", Text: "t"}, []string{`"session_id"`, `"text"`}},
		{RemoveIdeaReq{ID: "i"}, []string{`"id"`}},
		{ListIdeasReq{ProjectID: "p"}, []string{`"project_id"`}},
		{IdeaEvent{Kind: IdeaEventAdded, Idea: IdeaInfo{ID: "i"}}, []string{`"kind"`, `"idea"`}},
		{KillProjectReq{ProjectID: "p", DeleteIdeas: true}, []string{`"delete_ideas"`}},
		{Error{Code: ErrCodeProjectHasIdeas, ProjectID: "p"}, []string{`"project_id"`}},
	}
	for _, tc := range cases {
		b, err := json.Marshal(tc.v)
		if err != nil {
			t.Fatalf("marshal %T: %v", tc.v, err)
		}
		for _, want := range tc.want {
			if !strings.Contains(string(b), want) {
				t.Errorf("%T encoded as %s, missing %s", tc.v, b, want)
			}
		}
	}
}

// Pointer-per-field is what lets "clear the text" and "don't touch the
// text" be different requests. An omitted field must decode to nil,
// not to the zero value.
func TestUpdateIdeaReqPointerSemantics(t *testing.T) {
	var req UpdateIdeaReq
	if err := json.Unmarshal([]byte(`{"id":"i","status":"done"}`), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Text != nil {
		t.Errorf("omitted text decoded to %v, want nil", *req.Text)
	}
	if req.SessionID != nil {
		t.Errorf("omitted session_id decoded to %v, want nil", *req.SessionID)
	}
	if req.Status == nil || *req.Status != IdeaStatusDone {
		t.Errorf("status = %v, want done", req.Status)
	}
	// A round trip of the omitted fields must not invent them.
	b, err := json.Marshal(UpdateIdeaReq{ID: "i"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "text") || strings.Contains(string(b), "status") {
		t.Errorf("empty patch encoded as %s", b)
	}
}

// Error carries both ids so one client-side confirm-and-retry branch
// can serve the session-scoped and project-scoped refusals.
func TestErrorOmitsUnsetIDs(t *testing.T) {
	b, err := json.Marshal(Error{Code: ErrCodeWorktreeDirty, SessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "project_id") {
		t.Errorf("session-scoped error carried a project_id: %s", b)
	}
}
