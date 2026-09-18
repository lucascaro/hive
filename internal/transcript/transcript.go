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
	// Msg groups lines into messages: every line of one prompt, one
	// assistant text block or one tool result shares a Msg, and it rises
	// monotonically through the file. The GUI renders a message as a unit
	// — one header, tight line spacing, space between messages — which is
	// what makes the transcript read like an agent session rather than a
	// log.
	Msg int
	// Kind is what the message is, for presentation: "user" (a typed
	// prompt), "assistant", "tool" (a tool's output) or "meta" (the agent
	// harness talking — slash-command echoes, injected reminders). Role
	// alone cannot tell these apart: Claude records tool results, and its
	// own command echoes, as role "user".
	Kind string
	// Tool names the tool whose output this is, for Kind "tool". Display
	// only — it is not part of Text, so it is never searched.
	Tool string
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

// projector carries the state projection needs across records — and
// across calls, since the cache re-projects only a transcript's new tail.
type projector struct {
	msg int
	// tools maps a tool call's id to its name. Claude's tool_result only
	// names its call by tool_use_id; the name lives on the assistant's
	// earlier tool_use block.
	tools map[string]string
}

func newProjector() *projector {
	return &projector{tools: map[string]string{}}
}

// projectFile projects r with a fresh projector. See (*projector).project.
func projectFile(r io.Reader, dst []Line) ([]Line, int64, error) {
	return newProjector().project(r, dst)
}

// project reads r and appends display lines to dst, returning the
// extended slice and the number of bytes consumed.
//
// The byte count advances only past records that ended in a newline. A
// transcript observed mid-write has a half-flushed final record; if its
// bytes were consumed, the completed record would never be re-read and
// the most recent line — the one the user is most likely looking for —
// would be invisible forever.
func (p *projector) project(r io.Reader, dst []Line) ([]Line, int64, error) {
	br := bufio.NewReaderSize(r, 64<<10)
	var consumed int64
	for {
		raw, err := br.ReadBytes('\n')
		if len(raw) > 0 && raw[len(raw)-1] == '\n' {
			consumed += int64(len(raw))
			dst = p.appendRecord(dst, bytes.TrimRight(raw, "\r\n"))
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
func (p *projector) appendRecord(dst []Line, raw []byte) []Line {
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
	for _, pc := range rec.pieces(p.tools) {
		var emitted bool
		for _, part := range strings.Split(pc.text, "\n") {
			part = sanitize(part)
			if part == "" {
				continue
			}
			dst = append(dst, Line{
				Index: len(dst), Role: role, Text: part,
				Msg: p.msg, Kind: pc.kind, Tool: pc.tool,
			})
			emitted = true
		}
		if emitted {
			p.msg++
		}
	}
	return dst
}

// record is the subset of a transcript record this package reads. Both
// Claude and pi write a "type" per line and put message text under
// "message"; fields either does not use stay zero.
type record struct {
	Type    string   `json:"type"`
	IsMeta  bool     `json:"isMeta"`
	Message *message `json:"message"`
	Content *content `json:"content"`
}

type message struct {
	Role    string   `json:"role"`
	Content *content `json:"content"`
	// ToolName is pi's: its toolResult messages name their tool directly.
	ToolName string `json:"toolName"`
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
	// A tool call's id and name (Claude "tool_use", pi "toolCall"), and
	// the id a Claude tool_result answers.
	ID        string `json:"id"`
	Name      string `json:"name"`
	ToolUseID string `json:"tool_use_id"`
}

// piece is one displayable run of a record: its text and what it is.
type piece struct {
	kind string
	tool string
	text string
}

// pieces returns the displayable parts of a record, in order, recording
// any tool calls it makes in tools so later results can be named.
//
// Deliberately excluded: assistant "thinking" blocks and the JSON of
// tool-call inputs. The user is re-finding what they saw on screen, and
// searching raw argument JSON adds noise the spec rules out.
func (r *record) pieces(tools map[string]string) []piece {
	var out []piece
	role := r.Type
	if r.Message != nil && r.Message.Role != "" {
		role = r.Message.Role
	}
	textKind := kindForRole(role)
	if r.IsMeta {
		textKind = "meta"
	}
	add := func(c *content) {
		if c == nil {
			return
		}
		if c.Text != "" {
			k := textKind
			if k == "user" && isHarnessText(c.Text) {
				k = "meta"
			}
			out = append(out, piece{kind: k, text: c.Text})
			return
		}
		for _, b := range c.Blocks {
			switch b.Type {
			case "text", "":
				if b.Text == "" {
					continue
				}
				k := textKind
				if k == "user" && isHarnessText(b.Text) {
					k = "meta"
				}
				tool := ""
				if k == "tool" && r.Message != nil {
					tool = r.Message.ToolName
				}
				out = append(out, piece{kind: k, tool: tool, text: b.Text})
			case "tool_use", "toolCall":
				if b.ID != "" && b.Name != "" {
					tools[b.ID] = b.Name
				}
			case "tool_result":
				if b.Content == nil {
					continue
				}
				name := tools[b.ToolUseID]
				if b.Content.Text != "" {
					out = append(out, piece{kind: "tool", tool: name, text: b.Content.Text})
				}
				for _, inner := range b.Content.Blocks {
					if inner.Text != "" {
						out = append(out, piece{kind: "tool", tool: name, text: inner.Text})
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

// kindForRole maps a record's role onto a presentation kind.
func kindForRole(role string) string {
	switch role {
	case "user":
		return "user"
	case "assistant":
		return "assistant"
	case "toolResult", "tool":
		return "tool"
	default:
		return "meta"
	}
}

// isHarnessText reports text the agent's harness wrote into a user
// record rather than the user typing it: slash-command echoes, their
// captured output, injected reminders. Shown quietly so a real prompt
// stands out.
func isHarnessText(s string) bool {
	s = strings.TrimSpace(s)
	for _, tag := range []string{
		"<command-name>", "<command-message>", "<command-args>",
		"<local-command-stdout>", "<local-command-stderr>",
		"<local-command-caveat>", "<system-reminder>", "<bash-input>",
		"<bash-stdout>", "<bash-stderr>", "<task-notification>",
	} {
		if strings.HasPrefix(s, tag) {
			return true
		}
	}
	return false
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
	proj := newProjector()
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return out, err
		}
		out, _, err = proj.project(io.LimitReader(f, MaxFileBytes), out)
		f.Close()
		if err != nil {
			return out, err
		}
	}
	return out, nil
}
