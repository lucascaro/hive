package daemon

import (
	"github.com/lucascaro/hive/internal/transcript"
	"github.com/lucascaro/hive/internal/wire"
)

// searchTranscript answers SEARCH_TRANSCRIPT.
//
// The daemon searches rather than shipping the file: a frame is capped
// at wire.MaxPayload and real transcripts reach tens of MB. The client
// gets capped match anchors and asks separately for a window of lines.
//
// An unavailable session is a normal answer, not an error frame — the
// GUI renders "no searchable history" from it, and an error frame would
// be indistinguishable from a protocol fault.
func (d *Daemon) searchTranscript(req wire.SearchTranscriptReq) wire.TranscriptMatchesMsg {
	wire.ClampSearchReq(&req)
	msg := wire.TranscriptMatchesMsg{SessionID: req.SessionID, Query: req.Query}

	paths, reason := d.reg.TranscriptPaths(req.SessionID)
	if reason != wire.TranscriptOK {
		msg.Reason = reason
		return msg
	}

	lines, err := d.transcripts.Lines(req.SessionID, paths)
	if err != nil && len(lines) == 0 {
		// A transcript that vanished between resolution and read.
		msg.Reason = wire.TranscriptMissing
		return msg
	}
	msg.Available = true
	msg.TotalLines = len(lines)

	if req.Query == "" {
		return msg
	}
	hits, truncated := transcript.Search(lines, req.Query, req.MaxMatches)
	msg.Truncated = truncated
	msg.Total = len(hits)
	for _, h := range hits {
		preview, _ := transcript.CapText(lines[h.Line].Text, wire.MaxTranscriptPreview)
		msg.Matches = append(msg.Matches, wire.TranscriptMatch{
			Line: h.Line, Col: h.Col, Len: h.Len, Role: h.Role, Preview: preview,
		})
	}
	return wire.FitTranscriptMatches(msg)
}

// transcriptLines answers GET_TRANSCRIPT_LINES with a window centered on
// req.Center, clamped to the transcript's bounds.
//
// Centering is what makes a match readable in its surrounding
// conversation; the clamp is what stops a match near either end asking
// for an out-of-range start.
func (d *Daemon) transcriptLines(req wire.GetTranscriptLinesReq) wire.TranscriptLinesMsg {
	wire.ClampLinesReq(&req)
	msg := wire.TranscriptLinesMsg{SessionID: req.SessionID, ReqID: req.ReqID}

	paths, reason := d.reg.TranscriptPaths(req.SessionID)
	if reason != wire.TranscriptOK {
		msg.Reason = reason
		return msg
	}

	lines, err := d.transcripts.Lines(req.SessionID, paths)
	if err != nil && len(lines) == 0 {
		msg.Reason = wire.TranscriptMissing
		return msg
	}
	msg.Available = true
	msg.TotalLines = len(lines)

	window, start := transcript.Window(lines, req.Center, req.Count)
	msg.Start = start
	for _, ln := range window {
		text, cut := transcript.CapText(ln.Text, wire.MaxTranscriptLineText)
		msg.Lines = append(msg.Lines, wire.TranscriptLine{
			Line: ln.Index, Role: ln.Role, Text: text, Truncated: cut,
		})
	}
	return wire.FitTranscriptLines(msg)
}
