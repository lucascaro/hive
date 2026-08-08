// Package session — vt.go wraps a headless VT emulator that mirrors the
// PTY's visible state. On reattach the daemon asks the emulator to render
// a synthesized snapshot of the current screen, instead of replaying the
// raw byte ring (which wraps mid-CSI and turns repaint-style TUIs into
// fake "history").
package session

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/hinshun/vt10x"
)

// vt10x stores attributes as a bitfield on Glyph.Mode. The constants
// themselves are unexported in the upstream package, but the bit values
// are stable (see hinshun/vt10x/state.go). Mirroring them here is the
// least-invasive way to render SGR transitions without forking the lib.
const (
	vtAttrReverse   int16 = 1 << 0
	vtAttrUnderline int16 = 1 << 1
	vtAttrBold      int16 = 1 << 2
	// 1<<3 is attrGfx (DEC line-drawing charset). vt10x substitutes the
	// rune at parse time, so Cell().Char is already the box-drawing glyph
	// — there's no SGR code to emit and we intentionally don't mirror it.
	vtAttrItalic int16 = 1 << 4
	vtAttrBlink  int16 = 1 << 5
)

// historyRows caps the number of evicted rows we keep for prepending to
// the snapshot. Matched to xterm.js's client-side `scrollback: 5000` — the
// snapshot is now the ONLY history a normal-screen attach receives (see
// InitialReplayBytes), so anything less silently truncates what the user
// could previously scroll back through. ~80 cols × 5000 rows × ~100
// bytes/row ≈ 500 KiB worst case per session, still ~16× under the 8 MiB
// raw ring it replaces on attach.
//
// Keep in sync with the xterm `scrollback` option in session-term.js.
const historyRows = 5000

// ringCap is the default size of the raw-byte scrollback ring. 8 MiB ≈
// tens of thousands of lines of typical agent output. The ring lets us
// replay the entire session into a freshly-reset xterm.js on resize,
// recovering scrollback that xterm baked at the wrong width — which
// xterm itself does not reflow on resize. Larger than the row history
// because it stores raw bytes (CSI sequences and all) rather than
// pre-rendered rows.
const ringCap = 8 << 20

// VT is a goroutine-safe wrapper around a vt10x.Terminal. The emulator
// itself takes a State lock internally on Write/Parse, but our own
// Mutex serializes Write/Resize/RenderSnapshot against each other so we
// never read a half-updated screen.
//
// `history` holds rows that have scrolled off the top of the live grid,
// captured by a heuristic in Write (compare pre/post top rows). Each
// entry is a self-contained ANSI byte string: it begins at SGR defaults
// (emitting `\x1b[0…m` lazily on the first non-default cell), ends at
// SGR defaults followed by `\x1b[K`, so concatenation needs only
// `\r\n` separators with no SGR bleed across rows.
type VT struct {
	mu      sync.Mutex
	term    vt10x.Terminal
	history [][]byte

	// preScratch is the reusable pre-write glyph grid used by writeChunk.
	// Guarded by mu; re-allocated only when the terminal is resized.
	preScratch [][]vt10x.Glyph

	// ring holds the raw PTY bytes seen so far, capped at ringCap. On
	// overflow we drop the oldest bytes but advance to a safe boundary
	// (ESC or UTF-8 lead byte) so replay never starts mid-escape or
	// mid-multibyte rune. Used by clients to repaint xterm.js from a
	// clean slate after a width-changing resize.
	ring []byte
}

// NewVT constructs a VT sized cols x rows. Falls back to 80x24 when
// either dimension is zero so callers don't need to bother.
func NewVT(cols, rows int) *VT {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	return &VT{
		term: vt10x.New(vt10x.WithSize(cols, rows)),
	}
}

// Write feeds raw PTY bytes into the emulator. Best-effort — vt10x
// silently swallows sequences it doesn't model (mouse, OSC titles,
// bracketed paste etc), which is fine: those don't affect the rendered
// grid and live PTY bytes still flow to clients via the fanout path.
//
// While on the normal screen, Write also runs an eviction heuristic:
// pre-snapshot the top rows, run the underlying term.Write, then look
// for the largest k where preRows[k] == postRow[0]. If found, rows
// preRows[0..k-1] were scrolled off and we push them onto `history`.
// We bail when the post-write screen is entirely blank — that is a
// full clear/repaint, not a scroll, and matching against any blank
// preRows[kk] would otherwise push the cleared content into history.
// This catches the common scrolling-output case; CUP-and-overwrite,
// `\x1b[2J` clears (even when split across chunk boundaries from prior
// content), and scroll-region operations all fall through with no
// capture (matching xterm.js's own "2J doesn't push to scrollback"
// behavior).
func (v *VT) Write(p []byte) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.appendRing(p)

	cols, rows := v.term.Size()
	// The eviction heuristic can only see rows-1 lines of scroll per pass
	// (it looks for the pre-write row that became the new top row). A PTY
	// read is up to 64 KiB — on a high-output session that is hundreds of
	// lines in one Write, which scrolls the whole screen past and leaves
	// NO matching row, so the entire chunk is dropped from history. That
	// was survivable while attach streamed the raw ring; now the snapshot
	// is the only history a normal-screen attach gets, so it would strand
	// exactly the firehose sessions with an empty scrollback.
	//
	// Feeding the emulator in sub-screen slices keeps every pass inside
	// the heuristic's window. vt10x's parser is stateful across Write
	// calls, so any split point is safe; we cut on \n anyway.
	//
	// The window is rows-2, not rows-1: the chunk's first line overwrites
	// the pre-write bottom row before the screen scrolls, so at rows-1
	// newlines the new top row is that fresh line and matches nothing in
	// preRows. Capture then drops to ZERO rather than degrading — measured
	// on a 40-row terminal, 38 lines/chunk captures everything and 39
	// captures nothing. Keep the slack.
	total := 0
	for _, chunk := range splitByLines(p, rows-2) {
		n, err := v.writeChunk(chunk, cols, rows)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// writeChunk feeds one sub-screen slice to the emulator and runs the
// eviction heuristic across it. Caller holds v.mu.
func (v *VT) writeChunk(p []byte, cols, rows int) (int, error) {
	preAlt := v.term.Mode()&vt10x.ModeAltScreen != 0

	var preRows [][]vt10x.Glyph
	if !preAlt && cols > 0 && rows > 0 {
		// Reuse the scratch grid across chunks. A firehose read is split
		// into dozens of sub-screen chunks (see Write), and a fresh
		// rows×cols glyph grid per chunk is pure GC churn — captureEvictions
		// copies out what it keeps (captureRowANSI allocates its own bytes),
		// so the grid is dead the moment it returns.
		if len(v.preScratch) != rows || (rows > 0 && len(v.preScratch[0]) != cols) {
			v.preScratch = make([][]vt10x.Glyph, rows)
			for y := range rows {
				v.preScratch[y] = make([]vt10x.Glyph, cols)
			}
		}
		preRows = v.preScratch
		for y := range rows {
			row := preRows[y]
			for x := range cols {
				row[x] = v.term.Cell(x, y)
			}
		}
	}

	n, err := v.term.Write(p)

	postAlt := v.term.Mode()&vt10x.ModeAltScreen != 0
	if preRows != nil && !postAlt {
		v.captureEvictions(preRows, cols, rows)
	}
	return n, err
}

// splitByLines cuts p into slices of at most maxLines newlines each,
// always breaking immediately after a \n so no line is split in two.
// Returns p whole when it already fits (the overwhelmingly common case:
// interactive output arrives a few lines at a time) so the fast path
// allocates nothing.
func splitByLines(p []byte, maxLines int) [][]byte {
	if maxLines < 1 || len(p) == 0 {
		return [][]byte{p}
	}
	if bytes.Count(p, []byte{'\n'}) <= maxLines {
		return [][]byte{p}
	}
	var out [][]byte
	start, seen := 0, 0
	for i, b := range p {
		if b != '\n' {
			continue
		}
		seen++
		if seen == maxLines {
			out = append(out, p[start:i+1])
			start, seen = i+1, 0
		}
	}
	if start < len(p) {
		out = append(out, p[start:])
	}
	return out
}

// captureEvictions runs the post-write heuristic and pushes evicted
// rows onto the history ring. Caller holds v.mu.
//
// Bail when the entire post-write screen is blank: that is a clear /
// full-screen erase, not a scroll, and matching `preRows[kk]==post[0]`
// would otherwise push the just-cleared content into history. Once we
// know the screen still has content, every preRows[0..k-1] is pushed
// verbatim — blanks included — so vertical layout of the preserved
// scrollback matches the original output (paragraph spacing, blank
// separators).
func (v *VT) captureEvictions(preRows [][]vt10x.Glyph, cols, rows int) {
	// Bail when the post-write screen is entirely blank: a clear /
	// full-screen erase (`\x1b[H\x1b[2J`, app exit, full repaint), not
	// a scroll. Without this guard, a chunk boundary between content
	// and the clear sequence leaks the cleared content into scrollback.
	if termAllBlank(v.term, cols, rows) {
		return
	}
	// Find largest k in [1, rows) where preRows[k] equals the post-write
	// top row. Largest match wins, so a single chunk that scrolls N
	// lines is captured correctly. A blank candidate is fine here — the
	// "all blank post" guard above already filters the false-positive
	// case, and a legitimate scroll whose new top happens to be blank
	// (e.g. an empty line in the middle of mixed output) still needs to
	// match against blank preRows entries to capture the rest correctly.
	k := -1
	for kk := rows - 1; kk >= 1; kk-- {
		if rowsEqualTerm(preRows[kk], v.term, 0, cols) {
			k = kk
			break
		}
	}
	if k < 1 {
		return
	}
	for y := 0; y < k; y++ {
		v.pushHistory(captureRowANSI(preRows[y]))
	}
}

// appendRing copies p onto the byte ring, trimming the oldest bytes
// when the cap is exceeded. After trimming, scan forward for a safe
// replay boundary so a replay from offset 0 never begins in the
// middle of a CSI/OSC sequence or a multi-byte UTF-8 rune — either
// of which would surface as visible literal-text garbage at the top
// of the user's scrollback after an overflow.
//
// Boundary preference, from most-preferred to least:
//
//  1. A LF or CR byte (0x0A / 0x0D). Newlines never appear inside CSI
//     parameter sequences and are an unambiguous parser boundary.
//  2. The byte immediately after a CSI/OSC final byte (the `m` of
//     "[31m", the `BEL` of "]2;…\a"). We pick this up by tracking
//     whether the scan position is inside an unterminated escape.
//  3. An ESC byte (0x1B) — the start of a fresh sequence.
//  4. A UTF-8 leading byte (ASCII or 0xC0+) that is not inside an
//     active escape per the back-scan.
//
// If nothing safe is in `scanWindow`, fall through and drop exactly
// `drop` bytes. Worst case: one glitched cell of literal text at the
// top of replay. Caller holds v.mu.
func (v *VT) appendRing(p []byte) {
	if len(p) == 0 {
		return
	}
	v.ring = append(v.ring, p...)
	if len(v.ring) <= ringCap {
		return
	}
	drop := len(v.ring) - ringCap
	const scanWindow = 4 << 10
	limit := drop + scanWindow
	if limit > len(v.ring) {
		limit = len(v.ring)
	}
	safe := drop
	for i := drop; i < limit; i++ {
		b := v.ring[i]
		// Newlines are the gold-standard boundary: never inside CSI.
		if b == 0x0A || b == 0x0D {
			safe = i
			break
		}
	}
	// If we didn't find a newline, fall back to ESC / UTF-8 leading
	// byte, but verify the candidate is NOT inside an unterminated
	// escape by back-scanning up to backScan bytes for an unmatched
	// ESC.
	if safe == drop {
		const backScan = 64
		for i := drop; i < limit; i++ {
			b := v.ring[i]
			if b == 0x1B {
				safe = i
				break
			}
			if b < 0x80 || (b&0xC0) == 0xC0 {
				if !insideUnterminatedEscape(v.ring, i, backScan) {
					safe = i
					break
				}
			}
		}
	}
	retained := len(v.ring) - safe
	next := make([]byte, retained)
	copy(next, v.ring[safe:])
	v.ring = next
}

// insideUnterminatedEscape reports whether position `pos` in `b` is
// in the middle of a CSI / OSC sequence — i.e. there's an ESC in the
// preceding `back` bytes that has not yet seen a terminating byte.
// Walks each sequence explicitly:
//
//   CSI: ESC [ <params 0x30–0x3F | intermediates 0x20–0x2F>*
//        <final 0x40–0x7E>      — '[' itself is a final-range byte
//                                  but only the FIRST byte after ESC.
//   OSC: ESC ] <data>* <BEL | ESC \>
//   short: ESC <single byte>   — e.g. ESC 7 (DECSC). One-byte tail.
//
// "Back" caps how far we look backwards; CSI sequences are typically
// under 16 bytes, OSC titles can be longer (terminal title up to
// ~256 bytes), so 256 is a safe upper bound.
func insideUnterminatedEscape(b []byte, pos int, back int) bool {
	start := pos - back
	if start < 0 {
		start = 0
	}
	// Find the most recent ESC before pos.
	escAt := -1
	for i := pos - 1; i >= start; i-- {
		if b[i] == 0x1B {
			escAt = i
			break
		}
	}
	if escAt < 0 || escAt+1 >= pos {
		return false
	}
	switch b[escAt+1] {
	case '[':
		// CSI: scan from escAt+2 to pos-1 looking for a final byte.
		for i := escAt + 2; i < pos; i++ {
			c := b[i]
			if c >= 0x40 && c <= 0x7E {
				return false
			}
		}
		return true
	case ']':
		// OSC: scan for BEL or a subsequent ESC (start of ST).
		for i := escAt + 2; i < pos; i++ {
			c := b[i]
			if c == 0x07 || c == 0x1B {
				return false
			}
		}
		return true
	default:
		// Short escape: ESC <X>. Terminated at escAt+2. If pos is
		// past escAt+2 we're not inside any escape anymore.
		return pos < escAt+2
	}
}

// pushHistory appends b to the ring, trimming the oldest entries when
// over capacity. Caller holds v.mu.
func (v *VT) pushHistory(b []byte) {
	v.history = append(v.history, b)
	// Compact in batches, not per row. Re-slicing on every push is O(n) per
	// row — at historyRows=5000 that is a 5000-element copy for each of the
	// ~1500 rows in a single 64 KiB firehose read, which measured as an 18×
	// throughput cliff. Letting the slice run to 2× and then trimming back
	// makes it amortized O(1) at the cost of ≤ historyRows extra retained
	// rows (~500 KiB worst case). Readers must take the LAST historyRows
	// entries, not the whole slice — see RenderSnapshot.
	if len(v.history) > 2*historyRows {
		v.history = append([][]byte(nil), v.history[len(v.history)-historyRows:]...)
	}
}

// historyTail returns the newest historyRows entries — the window callers
// must render. pushHistory lets the slice overshoot to amortize compaction,
// so len(v.history) can exceed historyRows between trims. Caller holds v.mu.
func (v *VT) historyTail() [][]byte {
	if len(v.history) <= historyRows {
		return v.history
	}
	return v.history[len(v.history)-historyRows:]
}

// InitialReplayBytes returns the payload to paint a freshly-attaching
// client — a compact RenderSnapshot for BOTH screen types now (current
// screen + up to historyRows (== xterm's scrollback) of history on the
// normal screen; a single screen on the alt screen).
//
// It used to return the full multi-MB raw ring on the normal screen so deep
// scrollback survived the attach. But the ring is 8 MiB (tens of thousands
// of lines) while xterm.js keeps only `scrollback: 5000`, so the attach
// streamed ~8× more than xterm could retain: on a high-output session the
// client spent 13–38s parsing the whole ring on the main thread, visibly
// animating the entire history scrolling past, only to discard ~90% of it.
// That restream-on-every-attach is the "I see the entire history replay
// every time I enter grid" report (each grid tile attaches → full ring).
//
// The snapshot paints near-instantly and is NOT lossy from the user's point
// of view: historyRows is pinned to xterm's own `scrollback: 5000`, so the
// snapshot carries every line the client could have retained anyway. What
// the ring cost was the ~90% of parsed bytes xterm discarded on the way,
// plus every intermediate repaint animating past — the actual source of the
// 13–38s stall, not the byte count.
//
// The raw ring is still there for the one case that needs it: reflow.
// RequestScrollbackReplay (EmitAtomicReplay) returns it, and the client
// fires that when a width-changing resize happens while the user is
// scrolled UP into history. A follower never pays for a reflow of history
// it isn't looking at.
//
// The returned bool reports whether a snapshot was sent; it is now always
// true. Kept in the signature so callers' hived.log lines still record the
// path (and so a future full-ring initial path could flip it) without an
// API change.
func (v *VT) InitialReplayBytes() (replay []byte, snapshot bool) {
	return v.RenderSnapshot(), true
}

// RingBytes returns a defensive copy of the raw-byte scrollback ring.
// Callers can stream the result to a client that wants to repaint
// xterm.js from a clean slate after a width-changing resize.
func (v *VT) RingBytes() []byte {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.ring) == 0 {
		return nil
	}
	out := make([]byte, len(v.ring))
	copy(out, v.ring)
	return out
}

// Resize updates the emulator's grid dimensions. Returns nil for now;
// vt10x.Resize doesn't surface errors. History rows captured at the old
// width remain in the ring as-is — re-laying them out is not worth the
// complexity since most users don't resize mid-session.
func (v *VT) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.term.Resize(cols, rows)
	return nil
}

// RenderSnapshot produces a self-contained ANSI byte stream that paints
// the current visible state on a fresh terminal. Sequence:
//
//  1. soft reset + erase scrollback (3J) + erase visible (2J) + home
//  2. (normal screen only) all retained history rows, joined by \r\n
//  3. each visible row, with SGR transitions emitted only when attrs
//     change, short rows padded via \x1b[K, lines separated by \r\n
//  4. final cursor positioning (viewport-relative — xterm.js scrolls
//     history into its scrollback automatically as the visible region
//     fills, so no row offset is needed)
//  5. cursor visibility (DECTCEM) per emulator state
func (v *VT) RenderSnapshot() []byte {
	v.mu.Lock()
	defer v.mu.Unlock()

	cols, rows := v.term.Size()
	if cols <= 0 || rows <= 0 {
		return []byte("\x1b[!p\x1b[3J\x1b[2J\x1b[H")
	}

	var buf bytes.Buffer
	onAlt := v.term.Mode()&vt10x.ModeAltScreen != 0
	// If the session is on the alt screen (vim, htop, less, claude TUI…),
	// enter alt screen FIRST so the snapshot lands there. Otherwise the
	// next \x1b[?1049l from the live PTY would swap the client back to a
	// blank normal screen and discard everything we just painted.
	if onAlt {
		buf.WriteString("\x1b[?1049h")
	}
	// Soft reset + erase scrollback + erase visible + home. \x1b[!p is
	// DECSTR; \x1b[3J wipes any pre-existing scrollback in the receiving
	// terminal so reattach truly replaces what's there.
	buf.WriteString("\x1b[!p\x1b[3J\x1b[2J\x1b[H")

	// History block (normal screen only). Each ring entry is already a
	// self-contained `\x1b[m...\x1b[K` byte string, so we just join them
	// with \r\n and add a trailing \r\n before the visible block.
	if hist := v.historyTail(); !onAlt && len(hist) > 0 {
		for i, line := range hist {
			if i > 0 {
				buf.WriteString("\r\n")
			}
			buf.Write(line)
		}
		buf.WriteString("\r\n")
	}

	// Track current SGR state; "no attrs, default colours" means we've
	// just emitted \x1b[m (or are at the start, post soft-reset).
	var (
		curMode int16       = 0
		curFG   vt10x.Color = vt10x.DefaultFG
		curBG   vt10x.Color = vt10x.DefaultBG
		atDefault           = true
	)

	for y := range rows {
		renderRow(&buf, v.term, y, cols, &curMode, &curFG, &curBG, &atDefault)
		if y < rows-1 {
			buf.WriteString("\r\n")
		}
	}

	// Final SGR reset before cursor positioning, just to be safe.
	if !atDefault {
		buf.WriteString("\x1b[m")
	}

	cur := v.term.Cursor()
	// vt10x cursor is 0-indexed; CUP is 1-indexed.
	row := cur.Y + 1
	col := cur.X + 1
	if row < 1 {
		row = 1
	}
	if col < 1 {
		col = 1
	}
	fmt.Fprintf(&buf, "\x1b[%d;%dH", row, col)

	if v.term.CursorVisible() {
		buf.WriteString("\x1b[?25h")
	} else {
		buf.WriteString("\x1b[?25l")
	}

	return buf.Bytes()
}

// renderRow appends one row of `term` (at row y, width cols) to buf,
// using and updating the given SGR-tracking state. Trailing blank cells
// are elided via \x1b[K. Caller is responsible for line separation
// (\r\n) and for any final SGR reset after the last row.
func renderRow(buf *bytes.Buffer, term vt10x.Terminal, y, cols int,
	curMode *int16, curFG, curBG *vt10x.Color, atDefault *bool,
) {
	// Find last non-blank cell so we don't burn bytes on the trailing
	// spaces — emit \x1b[K instead.
	lastNonBlank := -1
	for x := cols - 1; x >= 0; x-- {
		g := term.Cell(x, y)
		if g.Char != 0 && g.Char != ' ' {
			lastNonBlank = x
			break
		}
		// A cell with non-default attrs but a space still counts as
		// visible — preserve highlighted blanks.
		if g.Mode != 0 || g.FG != vt10x.DefaultFG || g.BG != vt10x.DefaultBG {
			lastNonBlank = x
			break
		}
	}

	for x := 0; x <= lastNonBlank; x++ {
		g := term.Cell(x, y)
		if g.Mode != *curMode || g.FG != *curFG || g.BG != *curBG {
			writeSGR(buf, g.Mode, g.FG, g.BG)
			*curMode, *curFG, *curBG = g.Mode, g.FG, g.BG
			*atDefault = (*curMode == 0 && *curFG == vt10x.DefaultFG && *curBG == vt10x.DefaultBG)
		}
		ch := g.Char
		if ch == 0 {
			ch = ' '
		}
		buf.WriteRune(ch)
	}

	// Reset to defaults before \x1b[K so the erase-to-EOL doesn't
	// inherit a coloured background from the last cell.
	if !*atDefault {
		buf.WriteString("\x1b[m")
		*curMode = 0
		*curFG = vt10x.DefaultFG
		*curBG = vt10x.DefaultBG
		*atDefault = true
	}
	buf.WriteString("\x1b[K")
}

// captureRowANSI renders a captured glyph row to a self-contained ANSI
// byte string. Always starts at SGR defaults, ends at SGR defaults +
// \x1b[K, so concatenating multiple captured rows with \r\n separators
// is safe — there's no SGR bleed across rows.
func captureRowANSI(glyphs []vt10x.Glyph) []byte {
	var buf bytes.Buffer
	var (
		curMode int16       = 0
		curFG   vt10x.Color = vt10x.DefaultFG
		curBG   vt10x.Color = vt10x.DefaultBG
		atDefault           = true
	)
	// Find last non-blank cell.
	cols := len(glyphs)
	lastNonBlank := -1
	for x := cols - 1; x >= 0; x-- {
		g := glyphs[x]
		if g.Char != 0 && g.Char != ' ' {
			lastNonBlank = x
			break
		}
		if g.Mode != 0 || g.FG != vt10x.DefaultFG || g.BG != vt10x.DefaultBG {
			lastNonBlank = x
			break
		}
	}
	for x := 0; x <= lastNonBlank; x++ {
		g := glyphs[x]
		if g.Mode != curMode || g.FG != curFG || g.BG != curBG {
			writeSGR(&buf, g.Mode, g.FG, g.BG)
			curMode, curFG, curBG = g.Mode, g.FG, g.BG
			atDefault = (curMode == 0 && curFG == vt10x.DefaultFG && curBG == vt10x.DefaultBG)
		}
		ch := g.Char
		if ch == 0 {
			ch = ' '
		}
		buf.WriteRune(ch)
	}
	if !atDefault {
		buf.WriteString("\x1b[m")
	}
	buf.WriteString("\x1b[K")
	return buf.Bytes()
}

// rowsEqualTerm reports whether the captured glyph row matches row y of
// the live terminal. Used by the eviction heuristic to detect that an
// old row has shifted up by k positions.
func rowsEqualTerm(glyphs []vt10x.Glyph, term vt10x.Terminal, y, cols int) bool {
	if len(glyphs) != cols {
		return false
	}
	for x := range cols {
		g := term.Cell(x, y)
		if g != glyphs[x] {
			return false
		}
	}
	return true
}

// termAllBlank reports whether every cell of the live terminal across
// rows [0, rows) and cols [0, cols) is empty space at default attrs.
// Used to bail out of the eviction heuristic when the post-write
// screen is fully cleared, which would otherwise yield a false-positive
// match against any blank preRows[kk].
func termAllBlank(term vt10x.Terminal, cols, rows int) bool {
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			g := term.Cell(x, y)
			if g.Char != 0 && g.Char != ' ' {
				return false
			}
			if g.Mode != 0 || g.FG != vt10x.DefaultFG || g.BG != vt10x.DefaultBG {
				return false
			}
		}
	}
	return true
}

// writeSGR emits the minimum SGR sequence to adopt (mode, fg, bg).
// vt10x represents 16-color ANSI as raw indices [0,16) and 256-color
// xterm palette as [16,256); RGB and "default" are stored in the
// 1<<24-base sentinel range.
func writeSGR(buf *bytes.Buffer, mode int16, fg, bg vt10x.Color) {
	// Undo vt10x's storage transformations before emitting SGR, so the
	// receiving terminal applies each effect exactly once:
	//
	//   * Reverse video: vt10x's setChar pre-swaps FG/BG into the cell
	//     AND keeps the attrReverse bit. Re-swap so emitting `;7` doesn't
	//     reverse a row of selection-bar colours twice.
	//
	//   * Bold-bright: vt10x bumps FG by +8 when bold && FG<8 (so a
	//     "bold red" cell stores FG=9). Demote FG back to its low-ANSI
	//     index; xterm.js (and most terminals) auto-brighten bold by
	//     default, reproducing the original look. Cells where the user
	//     explicitly set bold + bright FG are indistinguishable in
	//     storage, so they take the same path — visually identical
	//     under normal "bold = brighter" rendering.
	if mode&vtAttrReverse != 0 {
		fg, bg = bg, fg
	}
	if mode&vtAttrBold != 0 && fg >= 8 && fg < 16 {
		fg -= 8
	}

	// Always start from a clean slate — keeps the implementation simple
	// and still compact in practice (most rows have few transitions).
	buf.WriteString("\x1b[0")
	if mode&vtAttrBold != 0 {
		buf.WriteString(";1")
	}
	if mode&vtAttrItalic != 0 {
		buf.WriteString(";3")
	}
	if mode&vtAttrUnderline != 0 {
		buf.WriteString(";4")
	}
	if mode&vtAttrBlink != 0 {
		buf.WriteString(";5")
	}
	if mode&vtAttrReverse != 0 {
		buf.WriteString(";7")
	}
	writeColor(buf, fg, true)
	writeColor(buf, bg, false)
	buf.WriteString("m")
}

func writeColor(buf *bytes.Buffer, c vt10x.Color, isFG bool) {
	defaultColor := vt10x.DefaultFG
	if !isFG {
		defaultColor = vt10x.DefaultBG
	}
	if c == defaultColor {
		return
	}
	switch {
	case c < 8:
		base := 30
		if !isFG {
			base = 40
		}
		fmt.Fprintf(buf, ";%d", base+int(c))
	case c < 16:
		// bright variants
		base := 90
		if !isFG {
			base = 100
		}
		fmt.Fprintf(buf, ";%d", base+int(c-8))
	case c < 256:
		if isFG {
			fmt.Fprintf(buf, ";38;5;%d", int(c))
		} else {
			fmt.Fprintf(buf, ";48;5;%d", int(c))
		}
	case c < 1<<24:
		// vt10x stores 24-bit RGB as Color(r<<16 | g<<8 | b) (see
		// vt10x/state.go setAttr). Decode and emit the standard
		// truecolor SGR so reattach preserves modern prompt/TUI styling.
		r, g, b := (c>>16)&0xff, (c>>8)&0xff, c&0xff
		if isFG {
			fmt.Fprintf(buf, ";38;2;%d;%d;%d", r, g, b)
		} else {
			fmt.Fprintf(buf, ";48;2;%d;%d;%d", r, g, b)
		}
	default:
		// Sentinel range (DefaultFG/DefaultBG/DefaultCursor at >=1<<24).
		// These represent "default" — emit nothing.
	}
}
