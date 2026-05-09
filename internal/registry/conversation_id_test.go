package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
)

func TestMetaFile_ConversationID_Roundtrip(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	a, err := r.Create(wire.CreateSpec{Name: "alpha", Cols: 80, Rows: 24, Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Manually set a conversation ID and persist.
	r.setConversationID(a.ID, "test-conv-id-123")

	if got := r.Get(a.ID).ConversationID; got != "test-conv-id-123" {
		t.Fatalf("after set: got %q, want test-conv-id-123", got)
	}
	_ = r.Close()

	// Reopen and confirm round-trip.
	r2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = r2.Close() })
	got := r2.Get(a.ID)
	if got == nil {
		t.Fatalf("entry %s missing after reopen", a.ID)
	}
	if got.ConversationID != "test-conv-id-123" {
		t.Errorf("after reopen: got %q, want test-conv-id-123", got.ConversationID)
	}
}

// TestRestart_FallsBackToResumeCmd_WhenNoConvID is the regression
// guard for Slice B touching Restart: an entry without
// ConversationID must still resume via def.ResumeCmd, not error out.
// We verify by checking that resumeArgsFor(agentID, "") returns the
// ResumeCmd argv — exercised here through the same path Restart uses.
func TestRestart_FallsBackToResumeCmd_WhenNoConvID(t *testing.T) {
	// Direct on the helper Restart calls.
	got := resumeArgsFor("claude", "")
	if len(got) == 0 || got[len(got)-1] != "--continue" {
		t.Errorf("claude no convID: got %v, want claude --continue", got)
	}
}

func TestSetConversationID_NoOpWhenAlreadySet(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	a, err := r.Create(wire.CreateSpec{Name: "alpha", Cols: 80, Rows: 24, Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	r.setConversationID(a.ID, "first")
	r.setConversationID(a.ID, "second") // should be ignored
	if got := r.Get(a.ID).ConversationID; got != "first" {
		t.Errorf("expected first to win: got %q", got)
	}
}

func TestJanitor_PicksUpIDForAliveSession(t *testing.T) {
	skipOnWindows(t)

	// Tighten cadences so the test runs in <1s.
	prevTick := janitorInterval
	janitorInterval = 25 * time.Millisecond
	t.Cleanup(func() { janitorInterval = prevTick })
	prevPoll := locatorPollDuration
	locatorPollDuration = 0 // skip the per-Create polling; let the janitor do the work
	t.Cleanup(func() { locatorPollDuration = prevPoll })

	tempRoot := t.TempDir()
	prevDataRoot := dataRootFor
	dataRootFor = func(id agent.ID) string {
		if id == agent.IDClaude {
			return tempRoot
		}
		return ""
	}
	t.Cleanup(func() { dataRootFor = prevDataRoot })

	r := freshRegistry(t)
	cwd := t.TempDir()
	p, err := r.CreateProject(wire.CreateProjectReq{Name: "proj", Color: "#abc", Cwd: cwd})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// Create a Claude session in that project. ConversationID will
	// initially be empty (no JSONL on disk yet).
	a, err := r.Create(wire.CreateSpec{
		Name:      "alpha",
		Cols:      80,
		Rows:      24,
		Shell:     "/bin/bash",
		Agent:     string(agent.IDClaude),
		ProjectID: p.ID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := r.Get(a.ID).ConversationID; got != "" {
		t.Fatalf("pre-locate: ConversationID = %q, want empty", got)
	}

	// Plant a Claude JSONL with an mtime after a.Created so the
	// locator's `since` filter accepts it.
	// claudeLocator encodes cwd by replacing "/" with "-".
	encoded := strings.ReplaceAll(cwd, "/", "-")
	dir := filepath.Join(tempRoot, "projects", encoded)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	jsonl := filepath.Join(dir, "found-conv-id.jsonl")
	if err := os.WriteFile(jsonl, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	post := time.Now().Add(1 * time.Second)
	if err := os.Chtimes(jsonl, post, post); err != nil {
		t.Fatal(err)
	}

	// Wait for the janitor to discover and persist.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := r.Get(a.ID).ConversationID; got == "found-conv-id" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("janitor never set ConversationID; have %q", r.Get(a.ID).ConversationID)
}

func TestSetConversationID_PersistsToDisk(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	a, err := r.Create(wire.CreateSpec{Name: "alpha", Cols: 80, Rows: 24, Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	r.setConversationID(a.ID, "persist-me")
	_ = r.Close()

	// Read raw MetaFile from disk to confirm it landed.
	var meta MetaFile
	path := filepath.Join(SessionsDir(dir), a.ID, "session.json")
	if err := readJSON(path, &meta); err != nil {
		t.Fatalf("readJSON: %v", err)
	}
	if meta.ConversationID != "persist-me" {
		t.Errorf("disk meta: got %q, want persist-me", meta.ConversationID)
	}
}

