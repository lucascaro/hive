// Package transcript reads an agent's on-disk JSONL conversation
// transcript and answers substring searches over it.
//
// It exists because an agent running on the terminal's alternate screen
// buffer keeps no scrollback: the GUI holds exactly one screenful, while
// the agent's full history sits in its own transcript file. Searching
// that file is the only way to find text the user saw five minutes ago.
//
// The package deliberately does its work daemon-side. A wire frame is
// capped at 1 MiB and real transcripts reach tens of MB, so shipping the
// file to the GUI is not an option; the GUI receives capped match
// anchors and a bounded window of lines instead.
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// Line is one displayable line of a projected transcript. Index is its
// position in the projection and is the only addressing scheme callers
// use — both search results and window requests speak in Index.
type Line struct {
	Index int
	Role  string
	Text  string
}

// Match is one substring hit. Col is a byte offset into the Line's Text
// as the caller will receive it, so a caller can highlight without any
// arithmetic of its own.
type Match struct {
	Line int
	Col  int
	Len  int
	Role string
}

// MaxFileBytes bounds how much of a transcript is read. The largest
// real transcript observed was 21.9 MB; this leaves headroom without
// letting a pathological file (or one with no newlines at all, which
// ReadBytes would pull entirely into memory) become unbounded work.
// Reading stops at the last complete record before the limit.
const MaxFileBytes = 64 << 20

// projectFile reads r and appends display lines to dst, returning the
// extended slice and the number of bytes consumed.
//
// The byte count advances only past records that ended in a newline. A
// transcript observed mid-write has a half-flushed final record; if its
// bytes were consumed, the completed record would never be re-read and
// the most recent line — the one the user is most likely looking for —
// would be invisible forever.
func projectFile(r io.Reader, dst []Line) ([]Line, int64, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	var consumed int64
	for {
		raw, err := br.ReadBytes('\n')
		if len(raw) > 0 && raw[len(raw)-1] == '\n' {
			consumed += int64(len(raw))
			dst = appendRecord(dst, bytes.TrimRight(raw, "\r\n"))
		}
		if err != nil {
			if err == io.EOF {
				return dst, consumed, nil
			}
			return dst, consumed, err
		}
	}
}

// appendRecord projects one JSONL record into zero or more display
// lines. A record that fails to parse is skipped rather than aborting
// the file: transcripts are written by other programs and one bad line
// must not cost the user the other fifty thousand.
func appendRecord(dst []Line, raw []byte) []Line {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return dst
	}
	var rec record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return dst
	}
	role := rec.Type
	if rec.Message != nil && rec.Message.Role != "" {
		role = rec.Message.Role
	}
	for _, s := range rec.texts() {
		for _, part := range strings.Split(s, "\n") {
			part = sanitize(part)
			if part == "" {
				continue
			}
			dst = append(dst, Line{Index: len(dst), Role: role, Text: part})
		}
	}
	return dst
}

// record is the subset of a transcript record this package reads. Both
// Claude and pi write a "type" per line and put message text under
// "message"; fields either does not use stay zero.
type record struct {
	Type    string   `json:"type"`
	Message *message `json:"message"`
	Content *content `json:"content"`
}

type message struct {
	Role    string   `json:"role"`
	Content *content `json:"content"`
}

// content is the string-or-array shape both agents use. Claude's
// .message.content is a plain string for a typed user message and an
// array of typed blocks for everything else, and a tool_result block's
// own .content repeats the same ambiguity one level deeper.
type content struct {
	Text   string
	Blocks []block
}

func (c *content) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return nil
	}
	if b[0] == '"' {
		return json.Unmarshal(b, &c.Text)
	}
	if b[0] == '[' {
		return json.Unmarshal(b, &c.Blocks)
	}
	// An object shape neither agent documents; ignore rather than fail
	// the whole record.
	return nil
}

type block struct {
	Type    string   `json:"type"`
	Text    string   `json:"text"`
	Content *content `json:"content"`
}

// texts returns the displayable strings of a record, in order.
//
// Deliberately excluded: assistant "thinking" blocks and the JSON of
// "tool_use" inputs. The user is re-finding what they saw on screen,
// and searching raw argument JSON adds noise the spec rules out.
func (r *record) texts() []string {
	var out []string
	add := func(c *content) {
		if c == nil {
			return
		}
		if c.Text != "" {
			out = append(out, c.Text)
			return
		}
		for _, b := range c.Blocks {
			switch b.Type {
			case "text", "":
				if b.Text != "" {
					out = append(out, b.Text)
				}
			case "tool_result":
				if b.Content != nil {
					if b.Content.Text != "" {
						out = append(out, b.Content.Text)
					}
					for _, inner := range b.Content.Blocks {
						if inner.Text != "" {
							out = append(out, inner.Text)
						}
					}
				}
			}
		}
	}
	if r.Message != nil {
		add(r.Message.Content)
	}
	add(r.Content)
	return out
}

// sanitize strips control characters, keeping tab.
//
// Two reasons, and the second is the load-bearing one. Raw escapes must
// not reach the DOM; and a match column counted over escape bytes would
// index text the caller never sees, highlighting the wrong characters.
// Stripping here means Col indexes exactly what the caller renders.
//
// It also removes the worst case for frame size: encoding/json expands
// each control byte to a six-byte \uXXXX escape, so escape-heavy
// terminal output would inflate 6x on the wire.
func sanitize(s string) string {
	if strings.IndexFunc(s, isCtrl) < 0 {
		return strings.TrimRight(s, " ")
	}
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := false
	for _, r := range s {
		if isCtrl(r) {
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	return strings.TrimRight(b.String(), " ")
}

func isCtrl(r rune) bool {
	if r == '\t' {
		return false
	}
	return r < 0x20 || r == 0x7f
}

// Search returns case-insensitive substring matches of query across
// lines, MOST RECENT FIRST, stopping at limit. The bool reports whether
// the results were truncated.
//
// Newest-first because the feature exists to answer "what did it just
// say": the first match shown is the one nearest the bottom, and
// stepping moves back through history. The order also decides what
// truncation drops — scanning from the end means a capped search keeps
// the newest matches, which are the ones the user wants, rather than
// the oldest.
//
// Within a line, later columns are newer, so they come first too.
//
// Plain substring by design: regex, whole-word and case-sensitive
// toggles are explicit non-goals for this pass.
func Search(lines []Line, query string, limit int) ([]Match, bool) {
	if query == "" || limit <= 0 {
		return nil, false
	}
	q := strings.ToLower(query)
	var out []Match
	for li := len(lines) - 1; li >= 0; li-- {
		ln := lines[li]
		hay := strings.ToLower(ln.Text)
		var cols []int
		for off := 0; off <= len(hay); {
			i := strings.Index(hay[off:], q)
			if i < 0 {
				break
			}
			cols = append(cols, off+i)
			off += i + len(q)
		}
		for ci := len(cols) - 1; ci >= 0; ci-- {
			if len(out) >= limit {
				return out, true
			}
			out = append(out, Match{Line: ln.Index, Col: cols[ci], Len: len(query), Role: ln.Role})
		}
	}
	return out, false
}

// CapText truncates s to at most max bytes without splitting a rune,
// reporting whether it cut. Mirrors daemon.capBytes, which exists for
// the same reason on a different payload.
func CapText(s string, max int) (string, bool) {
	if max <= 0 || len(s) <= max {
		return s, false
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// Window returns at most count lines centered on center, clamped to the
// bounds of lines. The returned int is the index of the first line, so
// a caller can address the slice without recomputing the clamp.
//
// Centering is what makes a match readable in context; the clamp is
// what stops a match on line 2 asking for a negative start.
func Window(lines []Line, center, count int) ([]Line, int) {
	if count <= 0 || len(lines) == 0 {
		return nil, 0
	}
	if count > len(lines) {
		count = len(lines)
	}
	start := center - count/2
	if start < 0 {
		start = 0
	}
	if start > len(lines)-count {
		start = len(lines) - count
	}
	return lines[start : start+count], start
}

// ReadAll projects every path in order into one line list. Multiple
// paths concatenate: nothing guarantees an agent writes exactly one
// file per session.
func ReadAll(paths []string) ([]Line, error) {
	var out []Line
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return out, err
		}
		out, _, err = projectFile(io.LimitReader(f, MaxFileBytes), out)
		f.Close()
		if err != nil {
			return out, err
		}
	}
	return out, nil
}
