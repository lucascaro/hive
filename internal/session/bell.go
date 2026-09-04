package session

// bellScanner reports real BEL bytes in a PTY stream.
//
// "Real" is the whole job. 0x07 is also the conventional terminator of
// an OSC sequence — `ESC ] 2 ; some title BEL` — and shells set their
// window title constantly, so counting every 0x07 would report a bell
// on every prompt. That is not a subtle miscount: it would light up
// every session in the sidebar permanently, which is worse than not
// having the feature.
//
// The scanner is a two-state machine over the byte stream rather than
// a per-chunk scan, because a PTY read can split an OSC sequence
// anywhere — including between the title text and its terminating BEL.
// insideUnterminatedEscape (vt.go) answers the same question by
// back-scanning a fixed window, which is right for its caller (a
// one-shot boundary search inside a buffer) and wrong here: an OSC
// longer than the window would leak its terminator through as a bell.
//
// Not safe for concurrent use; the readLoop is its only caller.
type bellScanner struct {
	// state is what the byte stream is in the middle of.
	state bellState
}

type bellState uint8

const (
	bellText   bellState = iota // ordinary output
	bellEsc                     // saw ESC, waiting to learn which sequence
	bellCSI                     // inside ESC [ … final-byte
	bellOSC                     // inside ESC ] … BEL or ESC \
	bellOSCEsc                  // inside OSC and saw ESC (possible ST)
)

// Scan feeds one chunk and reports whether it contained at least one
// bell. It returns a bool rather than a count: every caller wants "did
// this session ask for attention", and a program that rings twice in
// one chunk is not asking twice as hard.
func (s *bellScanner) Scan(p []byte) bool {
	rang := false
	for _, c := range p {
		switch s.state {
		case bellText:
			switch c {
			case 0x1B:
				s.state = bellEsc
			case 0x07:
				rang = true
			}
		case bellEsc:
			switch c {
			case '[':
				s.state = bellCSI
			case ']':
				s.state = bellOSC
			case 0x1B:
				// ESC ESC: stay here; the second one starts the real
				// sequence.
			default:
				// A short escape (ESC 7, ESC =, …) ends immediately.
				s.state = bellText
			}
		case bellCSI:
			// CSI runs until a final byte in 0x40–0x7E. A BEL cannot
			// terminate one, so a 0x07 here is a real bell that a
			// program emitted mid-sequence — rare, but counting it is
			// more correct than swallowing it.
			if c == 0x07 {
				rang = true
				continue
			}
			if c >= 0x40 && c <= 0x7E {
				s.state = bellText
			}
		case bellOSC:
			switch c {
			case 0x07:
				// The terminator, not a bell. This is the case the
				// whole type exists for.
				s.state = bellText
			case 0x1B:
				s.state = bellOSCEsc
			}
		case bellOSCEsc:
			// ESC \ is the other OSC terminator (ST). Anything else
			// means the ESC was literal payload and we are still in the
			// sequence.
			if c == '\\' {
				s.state = bellText
			} else {
				s.state = bellOSC
			}
		}
	}
	return rang
}
