package proto

// KittyFilter is a tiny streaming filter that strips kitty-keyboard-protocol
// CSI sequences from a byte stream. It is used by hivec-spike on the
// PTY→stdout path so that escape sequences emitted by remote programs
// (e.g. claude, recent vim) do not put the LOCAL outer terminal into
// an extended keyboard mode. Without this, keys like Ctrl-Q arrive at
// the client as multi-byte CSI escapes instead of raw control bytes,
// breaking single-byte detach.
//
// What we drop: any CSI sequence whose first byte after `\e[` is one of
// `<`, `=`, `>`, `?` (the "private parameter" introducers used by the
// kitty keyboard protocol) and whose final byte is `u`. That covers:
//   \e[>flags u   push / enable
//   \e[<u         pop / disable
//   \e[=flags;mode u   set
//   \e[?u         query
//
// Everything else (including normal CSI sequences) passes through
// unchanged. This is intentionally conservative — we only strip
// sequences that match the exact kitty-keyboard shape.
type KittyFilter struct {
	state filterState
	parm  byte // first byte after `\e[` for the in-progress CSI
	buf   []byte
}

type filterState int

const (
	stNormal filterState = iota
	stEsc                // saw ESC (0x1b)
	stCsi                // saw ESC [
	stCsiKitty           // saw ESC [ (one of <>=?)
)

// Filter consumes p and returns the bytes that should be forwarded.
// It mutates internal state, so calls must be sequential.
func (f *KittyFilter) Filter(p []byte) []byte {
	out := make([]byte, 0, len(p))
	for _, b := range p {
		switch f.state {
		case stNormal:
			if b == 0x1b {
				f.state = stEsc
				f.buf = f.buf[:0]
				f.buf = append(f.buf, b)
				continue
			}
			out = append(out, b)
		case stEsc:
			f.buf = append(f.buf, b)
			if b == '[' {
				f.state = stCsi
			} else {
				// Not a CSI; flush buffered ESC + this byte.
				out = append(out, f.buf...)
				f.buf = f.buf[:0]
				f.state = stNormal
			}
		case stCsi:
			f.buf = append(f.buf, b)
			if b == '<' || b == '=' || b == '>' || b == '?' {
				f.parm = b
				f.state = stCsiKitty
				continue
			}
			// Plain CSI; pass through. We don't inspect further, so we
			// just flush buffered (`\e[` + b) and return to normal —
			// the *rest* of the CSI body is ordinary bytes and will
			// pass through stNormal until the final byte. This works
			// because all CSI final bytes (0x40-0x7e) are non-special
			// in stNormal.
			out = append(out, f.buf...)
			f.buf = f.buf[:0]
			f.state = stNormal
		case stCsiKitty:
			f.buf = append(f.buf, b)
			// CSI body bytes: 0x30-0x3f (digits, ;, :), then a final
			// byte 0x40-0x7e.
			if b >= 0x30 && b <= 0x3f {
				continue
			}
			if b == 'u' {
				// Drop the entire sequence — it's a kitty keyboard CSI.
				f.buf = f.buf[:0]
				f.state = stNormal
				continue
			}
			// CSI ended with something other than 'u' (e.g. 'h' or 'l'
			// for DEC private modes). Not kitty-keyboard; flush as-is.
			out = append(out, f.buf...)
			f.buf = f.buf[:0]
			f.state = stNormal
		}
	}
	return out
}
