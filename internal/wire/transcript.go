package wire

import "encoding/json"

// Transcript search payloads.
//
// The daemon searches an agent's on-disk transcript and answers with
// match anchors; the client asks separately for a window of lines
// around whichever match is active. Neither response ever carries the
// file: a frame is capped at MaxPayload (1 MiB) and real transcripts
// reach tens of MB.
//
// Every cap below is enforced by the daemon. A client's request is a
// suggestion — see ClampSearchReq / ClampLinesReq.

const (
	// MaxTranscriptMatches bounds a search response's anchor list.
	MaxTranscriptMatches = 500
	// MaxTranscriptPreview bounds the context string carried per match.
	MaxTranscriptPreview = 300
	// MaxTranscriptWindow bounds a window response's line count.
	MaxTranscriptWindow = 200
	// MaxTranscriptLineText bounds one line's text. A single Claude
	// tool_result line routinely exceeds MaxPayload on its own.
	MaxTranscriptLineText = 2000

	// TranscriptPayloadBudget is the marshalled-size ceiling the daemon
	// shrinks a response to fit.
	//
	// This is the real guarantee; the byte caps above are only a fast
	// path. They bound text BEFORE encoding, while MaxPayload is
	// checked on the encoded frame, and encoding/json expands each
	// control byte to a six-byte \uXXXX escape. Transcript text is
	// terminal output, so an escape-heavy response could inflate far
	// past the caps' apparent arithmetic. Projection strips control
	// characters, and this budget catches whatever still surprises us.
	TranscriptPayloadBudget = MaxPayload - (64 << 10)
)

// TranscriptUnavailableReason explains why a session has no searchable
// transcript. The two "no transcript" cases are deliberately distinct:
// an agent that keeps none is a permanent fact about that agent, while
// a missing file for an agent that should have one is a transient state
// (or a resolution bug), and telling a real Claude session it has no
// history would be a lie.
const (
	TranscriptOK = "" // searchable
	// TranscriptNoSession means the session id is unknown.
	TranscriptNoSession = "no_such_session"
	// TranscriptUnsupported means this agent keeps no transcript Hive
	// can read (codex, gemini, copilot, a plain shell).
	TranscriptUnsupported = "unsupported_agent"
	// TranscriptMissing means the agent should have a transcript and
	// none was found — not yet written, or the session was created in a
	// way that left no id to resolve by.
	TranscriptMissing = "no_transcript_file"
)

// SearchTranscriptReq asks the daemon to search a session's transcript.
type SearchTranscriptReq struct {
	SessionID string `json:"session_id"`
	Query     string `json:"query"`
	// MaxMatches is clamped to MaxTranscriptMatches. Zero means the cap.
	MaxMatches int `json:"max_matches,omitempty"`
}

// TranscriptMatch is one hit. Col is a byte offset into the line's text
// as the client receives it, so highlighting needs no arithmetic.
type TranscriptMatch struct {
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Len     int    `json:"len"`
	Role    string `json:"role,omitempty"`
	Preview string `json:"preview,omitempty"`
}

// TranscriptMatchesMsg answers SearchTranscriptReq.
//
// Query is echoed so a client can discard a stale response: searches
// run per keystroke and are re-issued on session output, so two can be
// in flight and land out of order.
type TranscriptMatchesMsg struct {
	SessionID  string            `json:"session_id"`
	Query      string            `json:"query"`
	Available  bool              `json:"available"`
	Reason     string            `json:"reason,omitempty"`
	Total      int               `json:"total"`
	Truncated  bool              `json:"truncated,omitempty"`
	TotalLines int               `json:"total_lines"`
	Matches    []TranscriptMatch `json:"matches,omitempty"`
}

// GetTranscriptLinesReq asks for a window of lines.
//
// ReqID is a client-side monotonic counter echoed on the response.
// Window requests cannot be discriminated by query (they carry none)
// nor by revision (two requests differing only in Start share one), so
// without it two in-flight windows can land out of order and paint the
// wrong context around the active match.
type GetTranscriptLinesReq struct {
	SessionID string `json:"session_id"`
	ReqID     int    `json:"req_id"`
	// Center is the line the window is centered on. The daemon clamps
	// the resulting start to the bounds of the transcript.
	Center int `json:"center"`
	// Count is clamped to MaxTranscriptWindow. Zero means the cap.
	Count int `json:"count,omitempty"`
}

// TranscriptLine is one displayable line.
type TranscriptLine struct {
	Line      int    `json:"line"`
	Role      string `json:"role,omitempty"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

// TranscriptLinesMsg answers GetTranscriptLinesReq.
type TranscriptLinesMsg struct {
	SessionID  string           `json:"session_id"`
	ReqID      int              `json:"req_id"`
	Start      int              `json:"start"`
	TotalLines int              `json:"total_lines"`
	Available  bool             `json:"available"`
	Reason     string           `json:"reason,omitempty"`
	Lines      []TranscriptLine `json:"lines,omitempty"`
}

// ClampSearchReq applies the server-side limit to a client's request.
func ClampSearchReq(r *SearchTranscriptReq) {
	if r.MaxMatches <= 0 || r.MaxMatches > MaxTranscriptMatches {
		r.MaxMatches = MaxTranscriptMatches
	}
}

// ClampLinesReq applies the server-side limit to a client's request.
func ClampLinesReq(r *GetTranscriptLinesReq) {
	if r.Count <= 0 || r.Count > MaxTranscriptWindow {
		r.Count = MaxTranscriptWindow
	}
	if r.Center < 0 {
		r.Center = 0
	}
}

// FitTranscriptMatches drops matches from the tail until the marshalled
// message fits TranscriptPayloadBudget, returning the trimmed message.
//
// A loop with a strictly decreasing bound that terminates at an empty
// list, so it cannot fail to produce a sendable frame. Total is left
// reporting the true count — the user should still see how many hits
// exist even when not all anchors fit.
func FitTranscriptMatches(m TranscriptMatchesMsg) TranscriptMatchesMsg {
	for {
		b, err := json.Marshal(m)
		if err == nil && len(b) <= TranscriptPayloadBudget {
			return m
		}
		if len(m.Matches) == 0 {
			// Nothing left to drop; the fixed fields alone are small.
			m.Matches = nil
			return m
		}
		drop := len(m.Matches) / 4
		if drop < 1 {
			drop = 1
		}
		m.Matches = m.Matches[:len(m.Matches)-drop]
		m.Truncated = true
	}
}

// FitTranscriptLines is FitTranscriptMatches for a window response.
func FitTranscriptLines(m TranscriptLinesMsg) TranscriptLinesMsg {
	for {
		b, err := json.Marshal(m)
		if err == nil && len(b) <= TranscriptPayloadBudget {
			return m
		}
		if len(m.Lines) == 0 {
			m.Lines = nil
			return m
		}
		drop := len(m.Lines) / 4
		if drop < 1 {
			drop = 1
		}
		m.Lines = m.Lines[:len(m.Lines)-drop]
	}
}
