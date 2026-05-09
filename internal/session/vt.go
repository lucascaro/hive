// Package session — vt.go wraps a headless VT emulator that mirrors the
// PTY's visible state. On reattach the daemon asks the emulator to render
// a synthesized snapshot of the current screen, instead of replaying the
// raw byte ring (which wraps mid-CSI and turns repaint-style TUIs into
// fake "history").
//
// We use github.com/charmbracelet/x/vt (a width-aware emulator backed by
// charmbracelet/ultraviolet) so that CJK and wide-emoji rows snapshot at
// the same column layout xterm.js produces from the live byte stream.
// The previous backend (hinshun/vt10x) advanced the cursor by one cell
// per rune and had no concept of double-width cells, so any line with
// wide content shifted on reattach until the next live byte arrived
// (#142).
package session

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/x/vt"
)

// VT is a goroutine-safe wrapper around vt.SafeEmulator. SafeEmulator's
// own lock guards the emulator's parser/state; our Mutex serializes
// Write/Resize/RenderSnapshot against each other so we never read a
// half-updated screen. cursorVisible is updated from a callback that
// the emulator invokes inside its parser, which we don't hold a lock
// across — atomic.Bool keeps the read in RenderSnapshot race-free
// without depending on Charm's internal callback-dispatch model.
type VT struct {
	mu            sync.Mutex
	term          *vt.SafeEmulator
	cursorVisible atomic.Bool
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
	v := &VT{
		term: vt.NewSafeEmulator(cols, rows),
	}
	v.cursorVisible.Store(true)
	v.term.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(visible bool) {
			v.cursorVisible.Store(visible)
		},
	})
	// charmbracelet/x/vt responds to terminal queries (DA1/DA2, mode
	// reports, color queries, mouse events, in-band resize) by writing
	// to an internal io.Pipe exposed via Read. The pipe is unbuffered,
	// so the FIRST query an agent emits would block the parser inside
	// vt.Write — which would in turn block deliver()'s critical section
	// and starve every client's live byte stream. Plain shells rarely
	// query, agent TUIs do constantly. Drain and discard: xterm.js on
	// the client side already answers these queries from its own
	// emulator, so the daemon-side response is redundant.
	go io.Copy(io.Discard, v.term) //nolint:errcheck
	return v
}

// Write feeds raw PTY bytes into the emulator. Best-effort — the
// emulator silently drops sequences it doesn't model.
func (v *VT) Write(p []byte) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.term.Write(p)
}

// Close releases the underlying emulator. After Close, Write returns
// io.ErrClosedPipe; the drainer goroutine in NewVT unblocks on the
// resulting EOF from the response pipe and exits, so the goroutine
// and the emulator's internal state are eligible for GC.
func (v *VT) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.term.Close()
}

// Resize updates the emulator's grid dimensions.
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
//  1. soft reset + clear + home, so it works on any starting state
//  2. enter alt-screen first if the session is on it (so a later \x1b[?1049l
//     from the live PTY swaps the client back cleanly without discarding
//     the snapshot)
//  3. the emulator's own Render() output, which encodes SGR transitions
//     and emits a single grapheme per wide cell (the second half of a
//     wide cell is a zero-width shadow that Render skips). \n separators
//     are upgraded to \r\n.
//  4. final cursor positioning — the emulator already tracks display
//     columns (the cursor advances by cell.Width), so cur.X+1 maps
//     directly to xterm.js's 1-indexed column.
//  5. cursor visibility (DECTCEM) per emulator state.
func (v *VT) RenderSnapshot() []byte {
	v.mu.Lock()
	defer v.mu.Unlock()

	cols, rows := v.term.Width(), v.term.Height()
	if cols <= 0 || rows <= 0 {
		return []byte("\x1b[!p\x1b[2J\x1b[H")
	}

	var buf bytes.Buffer
	if v.term.IsAltScreen() {
		buf.WriteString("\x1b[?1049h")
	}
	// Soft reset + erase-display + home. \x1b[!p is DECSTR.
	buf.WriteString("\x1b[!p\x1b[2J\x1b[H")

	rendered := v.term.Render()
	// Render uses LF as line separator; we write to a PTY in raw mode,
	// so the receiving terminal needs explicit CR.
	buf.WriteString(strings.ReplaceAll(rendered, "\n", "\r\n"))
	// Defensive SGR reset before CUP. Render emits a final ResetStyle on
	// any line that ended with a non-zero pen, but adding one here costs
	// 3 bytes and guards against future renderer changes.
	buf.WriteString("\x1b[m")

	cur := v.term.CursorPosition()
	row := cur.Y + 1
	col := cur.X + 1
	if row < 1 {
		row = 1
	}
	if col < 1 {
		col = 1
	}
	fmt.Fprintf(&buf, "\x1b[%d;%dH", row, col)

	if v.cursorVisible.Load() {
		buf.WriteString("\x1b[?25h")
	} else {
		buf.WriteString("\x1b[?25l")
	}

	return buf.Bytes()
}
