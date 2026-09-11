package session

import (
	"bytes"
	"sort"
	"strconv"
)

// DEC private modes that a program sets once, early, and then relies on
// for the life of the session.
//
// This matters because our reattach path is not a byte replay of the
// session's whole history: RenderSnapshot opens with DECSTR (\x1b[!p),
// which clears these in the receiving terminal, and EmitAtomicReplay
// streams a ring that may have trimmed the original set sequences (and
// the client term.reset()s before consuming it). The program never
// re-sends them — it has no idea a new client attached — so whatever
// the snapshot does not re-assert is lost for the life of that tile.
//
// Bracketed paste is the one that bites hardest: with 2004 lost, xterm
// writes a paste unframed, the agent falls back to guessing paste
// boundaries from read sizes, and a >1 KiB paste splits at the macOS
// PTY input-buffer boundary (1022 bytes) into several "pasted text"
// chunks. Mouse tracking and app-cursor keys fail the same way, just
// more visibly.
//
// vt10x models none of these (see VT.Write), so we sniff them off the
// byte stream ourselves.
//
// Deliberately NOT here:
//   - 25 (DECTCEM) and 1049 (alt screen), which RenderSnapshot already
//     emits explicitly from emulator state; listing them here would
//     double-emit and could fight that logic.
//   - 7 (DECAWM), whose DECSTR default is already "on" — the common case
//     needs no restore, and a program that turns wrap off mid-session is
//     rare enough not to pay for.
var stickyDECModes = map[int]bool{
	1:    true, // DECCKM — application cursor keys
	1000: true, // mouse: normal tracking (press/release)
	1002: true, // mouse: button-event tracking (drag)
	1003: true, // mouse: any-event tracking (motion)
	1004: true, // focus in/out reporting
	1005: true, // mouse encoding: UTF-8
	1006: true, // mouse encoding: SGR
	1015: true, // mouse encoding: urxvt
	2004: true, // bracketed paste
}

// maxDECParams caps the parameter bytes we buffer for one sequence. A
// real "\x1b[?1002;1006h" is a handful of bytes; anything longer is
// garbage or a binary stream that happens to contain ESC [ ?, and we
// drop back to ground rather than grow a buffer on it.
const maxDECParams = 64

type decScanState uint8

const (
	decGround decScanState = iota // scanning for ESC
	decEsc                        // saw ESC
	decCSI                        // saw ESC [
	decPriv                       // saw ESC [ ?, collecting params
)

// decModeTracker is a streaming scanner for DEC private mode set/reset
// (`\x1b[?<params>h` / `...l`). It is a scanner, not a parser: it looks
// only for the private-mode form and ignores every other sequence, so
// it cannot be confused by SGR, CUP, OSC payloads and the like.
//
// State persists across calls because PTY reads split wherever the
// kernel happens to cut — a mode sequence can and does arrive in two
// chunks.
type decModeTracker struct {
	state  decScanState
	params []byte
	on     map[int]bool
}

// feed scans p for private mode changes, recording the latest state of
// every mode in stickyDECModes.
func (t *decModeTracker) feed(p []byte) {
	for _, b := range p {
		switch t.state {
		case decGround:
			if b == 0x1b {
				t.state = decEsc
			}
		case decEsc:
			switch b {
			case '[':
				t.state = decCSI
			case 0x1b:
				// Another ESC restarts the sequence.
			default:
				t.state = decGround
			}
		case decCSI:
			switch {
			case b == '?':
				t.state = decPriv
				t.params = t.params[:0]
			case b == 0x1b:
				t.state = decEsc
			default:
				// Some other CSI (SGR, CUP, …). Not ours.
				t.state = decGround
			}
		case decPriv:
			switch {
			case b >= '0' && b <= '9', b == ';':
				if len(t.params) >= maxDECParams {
					t.state = decGround
					continue
				}
				t.params = append(t.params, b)
			case b == 'h' || b == 'l':
				t.set(t.params, b == 'h')
				t.state = decGround
			case b == 0x1b:
				t.state = decEsc
			default:
				// A private-mode sequence with a final byte we don't
				// handle (e.g. \x1b[?25$p, a mode query). Not ours.
				t.state = decGround
			}
		}
	}
}

// set records each sticky mode named in a ";"-separated parameter list.
func (t *decModeTracker) set(params []byte, enabled bool) {
	for _, field := range bytes.Split(params, []byte(";")) {
		n, err := strconv.Atoi(string(field))
		if err != nil || !stickyDECModes[n] {
			continue
		}
		if t.on == nil {
			t.on = make(map[int]bool, len(stickyDECModes))
		}
		t.on[n] = enabled
	}
}

// restoreBytes returns the sequences needed to re-assert every sticky
// mode currently enabled, in ascending mode order so the output is
// deterministic (tests compare it byte-for-byte).
//
// Disabled modes emit nothing: a snapshot's DECSTR has already cleared
// them in the receiving terminal, so an explicit reset would be bytes
// spent to reach the state we are already in.
func (t *decModeTracker) restoreBytes() []byte {
	if len(t.on) == 0 {
		return nil
	}
	modes := make([]int, 0, len(t.on))
	for mode, enabled := range t.on {
		if enabled {
			modes = append(modes, mode)
		}
	}
	if len(modes) == 0 {
		return nil
	}
	sort.Ints(modes)

	var buf bytes.Buffer
	for _, mode := range modes {
		buf.WriteString("\x1b[?")
		buf.WriteString(strconv.Itoa(mode))
		buf.WriteString("h")
	}
	return buf.Bytes()
}
