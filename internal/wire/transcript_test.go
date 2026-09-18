package wire

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTranscriptFramesRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	req := SearchTranscriptReq{SessionID: "s1", Query: "needle", MaxMatches: 10}
	if err := WriteJSON(&buf, FrameSearchTranscript, req); err != nil {
		t.Fatal(err)
	}
	var gotReq SearchTranscriptReq
	ft, err := ReadJSON(&buf, &gotReq)
	if err != nil {
		t.Fatal(err)
	}
	if ft != FrameSearchTranscript || gotReq != req {
		t.Fatalf("got %s %+v", ft, gotReq)
	}

	msg := TranscriptMatchesMsg{
		SessionID: "s1", Query: "needle", Available: true, Total: 2, TotalLines: 99,
		Matches: []TranscriptMatch{
			{Line: 3, Col: 4, Len: 6, Role: "user", Preview: "a needle here"},
			{Line: 9, Col: 0, Len: 6, Role: "assistant", Preview: "needle again"},
		},
	}
	buf.Reset()
	if err := WriteJSON(&buf, FrameTranscriptMatches, msg); err != nil {
		t.Fatal(err)
	}
	var gotMsg TranscriptMatchesMsg
	if ft, err = ReadJSON(&buf, &gotMsg); err != nil {
		t.Fatal(err)
	}
	if ft != FrameTranscriptMatches || len(gotMsg.Matches) != 2 || gotMsg.Matches[1].Line != 9 {
		t.Fatalf("got %s %+v", ft, gotMsg)
	}

	lines := TranscriptLinesMsg{
		SessionID: "s1", ReqID: 7, Start: 40, TotalLines: 99, Available: true,
		Lines: []TranscriptLine{{Line: 40, Role: "user", Text: "hello", Truncated: true}},
	}
	buf.Reset()
	if err := WriteJSON(&buf, FrameTranscriptLines, lines); err != nil {
		t.Fatal(err)
	}
	var gotLines TranscriptLinesMsg
	if ft, err = ReadJSON(&buf, &gotLines); err != nil {
		t.Fatal(err)
	}
	if ft != FrameTranscriptLines || gotLines.ReqID != 7 || !gotLines.Lines[0].Truncated {
		t.Fatalf("got %s %+v", ft, gotLines)
	}
}

func TestClampSearchReq(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, MaxTranscriptMatches},
		{-5, MaxTranscriptMatches},
		{100000, MaxTranscriptMatches},
		{10, 10},
	}
	for _, c := range cases {
		r := SearchTranscriptReq{MaxMatches: c.in}
		ClampSearchReq(&r)
		if r.MaxMatches != c.want {
			t.Errorf("MaxMatches %d -> %d, want %d", c.in, r.MaxMatches, c.want)
		}
	}
}

func TestClampLinesReq(t *testing.T) {
	r := GetTranscriptLinesReq{Count: 100000, Center: -50}
	ClampLinesReq(&r)
	if r.Count != MaxTranscriptWindow {
		t.Errorf("Count -> %d", r.Count)
	}
	if r.Center != 0 {
		t.Errorf("negative Center -> %d, want 0", r.Center)
	}
	r = GetTranscriptLinesReq{Count: 0}
	ClampLinesReq(&r)
	if r.Count != MaxTranscriptWindow {
		t.Errorf("zero Count -> %d", r.Count)
	}
}

// The byte caps bound text before encoding; MaxPayload is checked on the
// encoded frame. This builds the worst case those caps allow — every
// character a control byte, which encoding/json expands six-fold — and
// asserts the response still fits. It fails if the marshalled-size
// check is removed and only the byte caps are trusted.
func TestTranscriptResponseFitsMaxPayloadWithAllEscapes(t *testing.T) {
	esc := strings.Repeat("\x01", MaxTranscriptPreview)
	msg := TranscriptMatchesMsg{SessionID: "s1", Query: "q", Available: true}
	for i := 0; i < MaxTranscriptMatches; i++ {
		msg.Matches = append(msg.Matches, TranscriptMatch{
			Line: i, Col: 0, Len: 1, Role: "assistant", Preview: esc,
		})
	}
	msg.Total = len(msg.Matches)

	fitted := FitTranscriptMatches(msg)
	var buf bytes.Buffer
	if err := WriteJSON(&buf, FrameTranscriptMatches, fitted); err != nil {
		t.Fatalf("worst-case matches response did not fit: %v", err)
	}
	if fitted.Total != MaxTranscriptMatches {
		t.Errorf("Total = %d; the true hit count must survive trimming", fitted.Total)
	}

	// A window is the case the byte caps do NOT cover: 200 lines of
	// 2000 control bytes is ~400 KB raw but ~2.4 MB encoded, well past
	// MaxPayload. Without the marshalled-size check this write fails
	// with ErrFrameTooLarge and the client waits forever on a response
	// that was never sent.
	lineText := strings.Repeat("\x01", MaxTranscriptLineText)
	lines := TranscriptLinesMsg{SessionID: "s1", Available: true}
	for i := 0; i < MaxTranscriptWindow; i++ {
		lines.Lines = append(lines.Lines, TranscriptLine{Line: i, Role: "user", Text: lineText})
	}
	if raw, err := marshalLen(lines); err != nil || raw <= TranscriptPayloadBudget {
		t.Fatalf("fixture is not over budget (%d bytes, err=%v) — it would prove nothing", raw, err)
	}
	fittedLines := FitTranscriptLines(lines)
	if len(fittedLines.Lines) >= MaxTranscriptWindow {
		t.Errorf("over-budget window was not trimmed: %d lines", len(fittedLines.Lines))
	}
	buf.Reset()
	if err := WriteJSON(&buf, FrameTranscriptLines, fittedLines); err != nil {
		t.Fatalf("worst-case window response did not fit: %v", err)
	}
}

func marshalLen(v any) (int, error) {
	b, err := json.Marshal(v)
	return len(b), err
}

// A response already within budget must pass through untouched.
func TestFitLeavesSmallResponsesAlone(t *testing.T) {
	msg := TranscriptMatchesMsg{
		SessionID: "s1", Query: "q", Available: true, Total: 1,
		Matches: []TranscriptMatch{{Line: 0, Preview: "small"}},
	}
	got := FitTranscriptMatches(msg)
	if len(got.Matches) != 1 || got.Truncated {
		t.Fatalf("got %+v", got)
	}
}

func TestTranscriptFrameTypeStrings(t *testing.T) {
	for ft, want := range map[FrameType]string{
		FrameSearchTranscript:   "SEARCH_TRANSCRIPT",
		FrameTranscriptMatches:  "TRANSCRIPT_MATCHES",
		FrameGetTranscriptLines: "GET_TRANSCRIPT_LINES",
		FrameTranscriptLines:    "TRANSCRIPT_LINES",
	} {
		if got := ft.String(); got != want {
			t.Errorf("%#x -> %q, want %q", byte(ft), got, want)
		}
	}
}

// Both responses fan out through the shared dispatch table.
func TestTranscriptControlEventNames(t *testing.T) {
	for ft, want := range map[FrameType]string{
		FrameTranscriptMatches: "transcript:matches",
		FrameTranscriptLines:   "transcript:lines",
	} {
		got, ok := ControlEventName(ft)
		if !ok || got != want {
			t.Errorf("%s -> %q ok=%v, want %q", ft, got, ok, want)
		}
	}
}

func TestClampSearchReqBoundsQuery(t *testing.T) {
	r := SearchTranscriptReq{Query: "a" + strings.Repeat("é", MaxTranscriptQuery)} // the cut splits an é
	ClampSearchReq(&r)
	if len(r.Query) > MaxTranscriptQuery || !utf8.ValidString(r.Query) {
		t.Fatalf("query not clamped to a valid prefix: %d bytes", len(r.Query))
	}
	if len(r.Query) != MaxTranscriptQuery-1 {
		t.Fatalf("got %d bytes, want %d", len(r.Query), MaxTranscriptQuery-1)
	}
	short := SearchTranscriptReq{Query: "hello"}
	ClampSearchReq(&short)
	if short.Query != "hello" {
		t.Fatalf("short query changed: %q", short.Query)
	}
}
