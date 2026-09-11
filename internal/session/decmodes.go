package session

import (
	"bytes"
	"slices"
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
// vt10x models some of these (1, 1000/1002/1003, 1004, 1006 all reach
// its setMode) but not the one that bites — 2004 falls through to
// "unknown private set/reset mode", as do 1005 and 1015 — and it exposes
// only a coarse ModeFlag bitmask, not the mode numbers to replay. So we
// sniff the whole set off the byte stream ourselves rather than stitch
// two sources of truth together.
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
	9:    true, // mouse: X10 compatibility
	1000: true, // mouse: normal tracking (press/release)
	1002: true, // mouse: button-event tracking (drag)
	1003: true, // mouse: any-event tracking (motion)
	1004: true, // focus in/out reporting
	1005: true, // mouse encoding: UTF-8
	1006: true, // mouse encoding: SGR
	1015: true, // mouse encoding: urxvt
	2004: true, // bracketed paste
}

// Exclusive groups. The receiving terminal keeps ONE slot per group, not
// a flag per mode: xterm.js parks 9/1000/1002/1003 in
// coreMouseService.activeProtocol and 1005/1006/1015 in activeEncoding.
// So setting any member supersedes whichever member was on, and
// resetting ANY member clears the slot outright — `\x1b[?1002l` sets the
// protocol to NONE even if 1000 was set earlier and never reset.
//
// Tracking the group rather than the individual modes is what keeps the
// restore honest in the reset direction: without it, a program that
// downgrades 1000 -> 1002 -> 1002l leaves 1000 marked on, and the
// reattach snapshot turns mouse reporting back on for a program that
// switched it off — the client then feeds it mouse escapes as input.
const (
	decGroupNone = iota
	decGroupMouseProtocol
	decGroupMouseEncoding
)

var decModeGroup = map[int]int{
	9:    decGroupMouseProtocol,
	1000: decGroupMouseProtocol,
	1002: decGroupMouseProtocol,
	1003: decGroupMouseProtocol,
	1005: decGroupMouseEncoding,
	1006: decGroupMouseEncoding,
	1015: decGroupMouseEncoding,
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
	decBang                       // saw ESC [ !, expecting p (DECSTR)
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
	// enabled holds the sticky modes currently on, in the order the
	// program turned them on. At most one member of each exclusive group
	// (see decModeGroup) is ever present; set order is kept only so the
	// restore bytes are deterministic.
	enabled []int
}

// feed scans p for private mode changes, recording the latest state of
// every mode in stickyDECModes. A reset from the program clears tracked
// state, but the two resets clear different amounts — see softReset.
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
			case 'c':
				// RIS — the terminal drops everything.
				t.enabled = t.enabled[:0]
				t.state = decGround
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
			case b == '!':
				t.state = decBang
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
		case decBang:
			switch b {
			case 'p':
				// DECSTR — soft reset clears only the ungrouped modes;
				// see softReset for why the mouse slots survive.
				t.softReset()
				t.state = decGround
			case 0x1b:
				t.state = decEsc
			default:
				t.state = decGround
			}
		}
	}
}

// softReset drops the modes a program-issued DECSTR (\x1b[!p) actually
// clears in the receiving terminal, and only those.
//
// The tracker exists to mirror the real client, so it has to split the
// two resets the way xterm.js 5.5.0 does. softReset() (InputHandler.ts)
// calls _coreService.reset(), which restores DEFAULT_DEC_PRIVATE_MODES —
// applicationCursorKeys (1), sendFocus (1004), bracketedPasteMode (2004),
// i.e. exactly our decGroupNone members. It never touches
// CoreMouseService, so activeProtocol (9/1000/1002/1003) and
// activeEncoding (1005/1006/1015) survive a DECSTR. Only RIS clears
// those: ESC c -> fullReset() -> CoreTerminal.reset(), which calls
// coreMouseService.reset() as well.
//
// Clearing the mouse slots here too would leave the tracker believing
// mouse reporting is off while the program still has it on, and no
// replay path would re-assert it — a reattach would silently kill mouse
// reporting for the life of that tile, which is the same class of bug
// this file was written to fix.
func (t *decModeTracker) softReset() {
	t.enabled = slices.DeleteFunc(t.enabled, func(m int) bool {
		return decModeGroup[m] == decGroupNone
	})
}

// set records each sticky mode named in a ";"-separated parameter list.
// A mode in an exclusive group takes its group's slot: setting it evicts
// whichever member held the slot, and resetting any member empties the
// slot rather than uncovering the previous occupant — see decModeGroup
// for why the terminal behaves that way.
func (t *decModeTracker) set(params []byte, enabled bool) {
	for _, field := range bytes.Split(params, []byte(";")) {
		n, err := strconv.Atoi(string(field))
		if err != nil || !stickyDECModes[n] {
			continue
		}
		group := decModeGroup[n]
		t.enabled = slices.DeleteFunc(t.enabled, func(m int) bool {
			return m == n || (group != decGroupNone && decModeGroup[m] == group)
		})
		if enabled {
			t.enabled = append(t.enabled, n)
		}
	}
}

// restoreBytes returns the sequences needed to re-assert every sticky
// mode currently enabled, in the order the program set them — which
// makes the output deterministic (tests compare it byte-for-byte).
// Order carries no meaning beyond that: at most one member of each
// exclusive group is ever enabled, so no replay order can land the
// terminal in a mode the program did not ask for.
//
// Disabled modes emit nothing: a snapshot's DECSTR has already cleared
// them in the receiving terminal, so an explicit reset would be bytes
// spent to reach the state we are already in.
func (t *decModeTracker) restoreBytes() []byte {
	if len(t.enabled) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for _, mode := range t.enabled {
		buf.WriteString("\x1b[?")
		buf.WriteString(strconv.Itoa(mode))
		buf.WriteString("h")
	}
	return buf.Bytes()
}
