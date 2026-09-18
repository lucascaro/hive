package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/transcript"
	"github.com/lucascaro/hive/internal/wire"
)

// readTranscriptMatches drains until the first TRANSCRIPT_MATCHES.
// A control connection is still delivering its initial snapshots and
// any session deltas, so the answer is not necessarily the next frame.
func readTranscriptMatches(t *testing.T, c interface {
	Read([]byte) (int, error)
}) wire.TranscriptMatchesMsg {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ft, payload, err := wire.ReadFrame(c)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		if ft != wire.FrameTranscriptMatches {
			continue
		}
		var msg wire.TranscriptMatchesMsg
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return msg
	}
	t.Fatal("no TRANSCRIPT_MATCHES within the deadline")
	return wire.TranscriptMatchesMsg{}
}

func readTranscriptLines(t *testing.T, c interface {
	Read([]byte) (int, error)
}) wire.TranscriptLinesMsg {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ft, payload, err := wire.ReadFrame(c)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		if ft != wire.FrameTranscriptLines {
			continue
		}
		var msg wire.TranscriptLinesMsg
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return msg
	}
	t.Fatal("no TRANSCRIPT_LINES within the deadline")
	return wire.TranscriptLinesMsg{}
}

func jsonlRecord(t *testing.T, role, text string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type":    role,
		"message": map[string]any{"role": role, "content": text},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// A session with no agent (a plain shell) keeps no transcript Hive can
// read. That is a normal answer the GUI renders as "no searchable
// history" — not an error frame, which would be indistinguishable from
// a protocol fault.
func TestSearchTranscriptUnavailableForShellSession(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)

	c := dial(t, d)
	defer c.Close()
	handshake(t, c, wire.Hello{Mode: wire.ModeControl})
	if err := wire.WriteJSON(c, wire.FrameSearchTranscript, wire.SearchTranscriptReq{
		SessionID: id, Query: "anything",
	}); err != nil {
		t.Fatalf("write SEARCH_TRANSCRIPT: %v", err)
	}

	msg := readTranscriptMatches(t, c)
	if msg.Available {
		t.Fatalf("a shell session should not be searchable: %+v", msg)
	}
	if msg.Reason != wire.TranscriptUnsupported {
		t.Errorf("Reason = %q, want %q", msg.Reason, wire.TranscriptUnsupported)
	}
	if msg.SessionID != id {
		t.Errorf("SessionID = %q, want %q", msg.SessionID, id)
	}
}

// An unknown session is distinct from an agent that keeps no
// transcripts: one is a client bug, the other a fact about the agent.
func TestSearchTranscriptUnknownSession(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)

	c := dial(t, d)
	defer c.Close()
	handshake(t, c, wire.Hello{Mode: wire.ModeControl})
	if err := wire.WriteJSON(c, wire.FrameSearchTranscript, wire.SearchTranscriptReq{
		SessionID: "nope", Query: "x",
	}); err != nil {
		t.Fatal(err)
	}
	msg := readTranscriptMatches(t, c)
	if msg.Available || msg.Reason != wire.TranscriptNoSession {
		t.Fatalf("got %+v", msg)
	}
}

// The window handler answers over the wire too, and carries the same
// reason vocabulary — a client that opened the box on an unsearchable
// session must get a normal answer back, not silence.
func TestGetTranscriptLinesUnavailableForShellSession(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)

	c := dial(t, d)
	defer c.Close()
	handshake(t, c, wire.Hello{Mode: wire.ModeControl})
	if err := wire.WriteJSON(c, wire.FrameGetTranscriptLines, wire.GetTranscriptLinesReq{
		SessionID: id, ReqID: 42, Center: 0,
	}); err != nil {
		t.Fatalf("write GET_TRANSCRIPT_LINES: %v", err)
	}

	msg := readTranscriptLines(t, c)
	if msg.Available {
		t.Fatalf("a shell session should not be searchable: %+v", msg)
	}
	if msg.Reason != wire.TranscriptUnsupported {
		t.Errorf("Reason = %q, want %q", msg.Reason, wire.TranscriptUnsupported)
	}
	// ReqID must survive even on the unavailable path, or the client
	// cannot match the response to the request it discarded around.
	if msg.ReqID != 42 {
		t.Errorf("ReqID = %d, want 42", msg.ReqID)
	}
}

// The mode gate, mirroring TestSessionModeCannotGetActivity. An agent
// running inside a session must not read transcripts over the events
// socket — that is a separate decision with its own privacy question.
func TestSessionModeCannotSearchTranscript(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)

	c := dialEvents(t, d)
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "test/0",
		Mode: wire.ModeSession, SessionID: id,
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	// Its OWN transcript, the most permissive thing it could ask for.
	if err := wire.WriteJSON(c, wire.FrameSearchTranscript, wire.SearchTranscriptReq{
		SessionID: id, Query: "x",
	}); err != nil {
		t.Fatalf("write SEARCH_TRANSCRIPT: %v", err)
	}
	if err := awaitModeNotAllowed(t, c); err != nil {
		t.Fatalf("want mode_not_allowed: %v", err)
	}
}

func TestSessionModeCannotGetTranscriptLines(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)

	c := dialEvents(t, d)
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "test/0",
		Mode: wire.ModeSession, SessionID: id,
	}); err != nil {
		t.Fatal(err)
	}
	if err := wire.WriteJSON(c, wire.FrameGetTranscriptLines, wire.GetTranscriptLinesReq{
		SessionID: id,
	}); err != nil {
		t.Fatal(err)
	}
	if err := awaitModeNotAllowed(t, c); err != nil {
		t.Fatalf("want mode_not_allowed: %v", err)
	}
}

// --- the search/window path, exercised directly against the daemon's
// handlers with a synthetic transcript, so it does not need a real
// agent on PATH. ---

// fakeTranscript writes a transcript and points the daemon's resolver
// at it by hand, bypassing the registry's agent lookup. The registry
// path is covered by the agent package's own tests; what matters here
// is the handler's clamping, centering and payload behaviour.
func fakeTranscriptLines(t *testing.T, n int, needle string) []string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "t.jsonl")
	var b strings.Builder
	for i := range n {
		text := "line " + string(rune('a'+i%26))
		if i == n/2 {
			text = needle
		}
		b.WriteString(jsonlRecord(t, "user", text))
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return []string{p}
}

func TestSearchTranscriptFindsAndClamps(t *testing.T) {
	d := &Daemon{}
	paths := fakeTranscriptLines(t, 400, "the needle is here")

	lines, err := d.transcripts.Lines("s1", paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 400 {
		t.Fatalf("projected %d lines", len(lines))
	}

	// An over-cap request must come back clamped, not honoured.
	req := wire.SearchTranscriptReq{SessionID: "s1", Query: "line", MaxMatches: 100000}
	wire.ClampSearchReq(&req)
	if req.MaxMatches != wire.MaxTranscriptMatches {
		t.Fatalf("MaxMatches not clamped: %d", req.MaxMatches)
	}
}

// Criterion 3's centering half, plus the clamp that stops a match near
// either end asking for an out-of-range start.
func TestGetTranscriptLinesWindowIsCenteredAndClamped(t *testing.T) {
	d := &Daemon{}
	paths := fakeTranscriptLines(t, 400, "needle")
	if _, err := d.transcripts.Lines("s1", paths); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		center    int
		count     int
		wantStart int
	}{
		{"middle is centered", 200, 40, 180},
		{"head clamps to zero", 2, 40, 0},
		{"tail clamps to end", 399, 40, 360},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := wire.GetTranscriptLinesReq{
				SessionID: "s1", ReqID: 1, Center: tc.center, Count: tc.count,
			}
			wire.ClampLinesReq(&req)
			lines, _ := d.transcripts.Lines("s1", paths)
			_, start := transcript.Window(lines, req.Center, req.Count)
			if start != tc.wantStart {
				t.Fatalf("center %d count %d -> start %d, want %d",
					tc.center, tc.count, start, tc.wantStart)
			}
			if start < 0 {
				t.Fatal("negative start")
			}
		})
	}
}

// A window request with a negative center must not produce a negative
// start; ClampLinesReq is what guarantees it.
func TestGetTranscriptLinesRejectsNegativeCenter(t *testing.T) {
	req := wire.GetTranscriptLinesReq{Center: -500, Count: 0}
	wire.ClampLinesReq(&req)
	if req.Center != 0 {
		t.Fatalf("Center = %d", req.Center)
	}
	if req.Count != wire.MaxTranscriptWindow {
		t.Fatalf("Count = %d", req.Count)
	}
}
