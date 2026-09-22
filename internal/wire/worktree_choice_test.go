package wire

import (
	"encoding/json"
	"strings"
	"testing"
)

// The wire is snake_case and the Go side is CamelCase (DESIGN.md), and
// the JS readers key off the snake_case names. A tag typo here is
// invisible in Go and silently empties the dialog in the GUI.
func TestPendingWorktreeChoiceRoundTrip(t *testing.T) {
	in := SessionInfo{
		ID: "s1",
		PendingWorktreeChoice: &PendingWorktreeChoice{
			Kind:             WorktreeChoiceFetchFailed,
			Message:          "ssh: Could not resolve hostname example.invalid",
			Branch:           "feature-x",
			CachedRef:        "origin/main",
			CachedTip:        "deadbeef",
			CachedTipAgeSecs: 259200,
		},
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{
		`"pending_worktree_choice"`, `"cached_ref"`, `"cached_tip"`,
		`"cached_tip_age_secs"`, `"kind"`, `"message"`,
	} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("wire JSON is missing %s; the GUI reads these names: %s", key, raw)
		}
	}

	var out SessionInfo
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.PendingWorktreeChoice == nil {
		t.Fatal("pending choice lost in round trip")
	}
	if *out.PendingWorktreeChoice != *in.PendingWorktreeChoice {
		t.Errorf("round trip changed the payload:\n got %+v\nwant %+v",
			*out.PendingWorktreeChoice, *in.PendingWorktreeChoice)
	}
}

// A session with nothing pending must not carry the key at all: every
// SessionInfo is broadcast on every event, and an always-present null
// is bytes on the wire for nothing.
func TestPendingWorktreeChoiceOmittedWhenAbsent(t *testing.T) {
	raw, err := json.Marshal(SessionInfo{ID: "s1"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "pending_worktree_choice") {
		t.Errorf("field must be omitempty; got %s", raw)
	}
}

func TestResolveWorktreeChoiceReqRoundTrip(t *testing.T) {
	raw, err := json.Marshal(ResolveWorktreeChoiceReq{
		SessionID: "s1", Choice: WorktreeChoiceRetry,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"session_id"`) || !strings.Contains(string(raw), `"choice"`) {
		t.Errorf("payload keys must be snake_case; got %s", raw)
	}
	if got := FrameResolveWorktreeChoice.String(); got != "RESOLVE_WORKTREE_CHOICE" {
		t.Errorf("frame name = %q; logs and the bridge switch on it", got)
	}
}
