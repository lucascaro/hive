package session

import "strings"

// OSC 7501 is the Program Status Protocol: a program prints
// `OSC 7501 ; state=<s>[:key=value…]` to say what it is doing, and asks
// whether the terminal listens with `OSC 7501 ; ?`. Claude Code only
// emits status once the terminal has answered that query (spec 517,
// docs/references/agent-osc-signals.md), so the daemon, which owns the
// PTY, answers it.

// oscPrefixMax bounds how much of one OSC payload is kept. Status
// reports put `state` and `kind` first and the free-text `title` /
// `msg` fields (base64, unbounded) last, so a prefix is enough, and a
// long question never costs the whole report. Bytes past the prefix are
// consumed to the terminator and dropped.
const oscPrefixMax = 256

// statusQueryReply is what the terminal says back to `OSC 7501 ; ?`. It
// is the probe-verified form: Claude accepts it and starts emitting.
var statusQueryReply = []byte("\x1b]7501;?\x1b\\")

// Status is one parsed OSC 7501 report. Only the fields Hive acts on are
// kept; `msg` and `title` are program-controlled text and are never
// read.
type Status struct {
	State string // working, idle, blocked, done, error, clear
	Kind  string // blocked only: permission, question, auth
}

// maxPendingStatuses caps what one chunk can queue, so a program
// printing nothing but status reports cannot grow the scanner without
// bound. Only the latest few matter.
const maxPendingStatuses = 64

// classifyOSC inspects one finished OSC payload prefix. It records a
// capability query or a status report on the scanner; anything else
// (titles, hyperlinks, other protocols) is ignored.
func (s *bellScanner) classifyOSC() {
	p := string(s.payload[:s.plen])
	rest, ok := strings.CutPrefix(p, "7501;")
	if !ok {
		return
	}
	if strings.HasPrefix(rest, "?") {
		s.query = true
		return
	}
	var st Status
	for _, f := range strings.Split(rest, ":") {
		k, v, ok := strings.Cut(f, "=")
		if !ok {
			continue
		}
		switch k {
		case "state":
			st.State = v
		case "kind":
			st.Kind = v
		}
	}
	// Only the protocol's states; anything else (including a payload
	// cut off mid-value by the prefix bound) is not a report.
	switch st.State {
	case "working", "idle", "blocked", "done", "error", "clear":
	default:
		return
	}
	if len(s.statuses) == maxPendingStatuses {
		copy(s.statuses, s.statuses[1:])
		s.statuses = s.statuses[:maxPendingStatuses-1]
	}
	s.statuses = append(s.statuses, st)
}

// Take drains what Scan found since the last call: whether a 7501
// capability query was seen, and the status reports in order.
func (s *bellScanner) Take() (query bool, statuses []Status) {
	query, statuses = s.query, s.statuses
	s.query, s.statuses = false, nil
	return query, statuses
}
