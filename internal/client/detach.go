package client

// prefixByte is the detach prefix: Ctrl-A.
const prefixByte = 0x01

// DetachScanner inspects the client's keystroke stream for the
// Ctrl-A d detach sequence. Feed it one byte at a time with Push; it
// returns the bytes to forward to the session and whether the user
// asked to detach. The zero value is ready to use.
type DetachScanner struct {
	armed bool // last byte was the unconsumed prefix
}

// Push processes one input byte. forward is the (possibly empty) slice
// of bytes to send to the session; detach is true when the user typed
// the detach sequence.
func (s *DetachScanner) Push(b byte) (forward []byte, detach bool) {
	if s.armed {
		s.armed = false
		switch b {
		case 'd', 'D':
			return nil, true
		case prefixByte:
			return []byte{prefixByte}, false
		default:
			return []byte{prefixByte, b}, false
		}
	}
	if b == prefixByte {
		s.armed = true
		return nil, false
	}
	return []byte{b}, false
}
