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

// VT is a goroutine-safe wrapper around a vt10x.Terminal. The emulator
// itself takes a State lock internally on Write/Parse, but our own
// Mutex serializes Write/Resize/RenderSnapshot against each other so we
// never read a half-updated screen.
type VT struct {
	mu   sync.Mutex
	term vt10x.Terminal
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
func (v *VT) Write(p []byte) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.term.Write(p)
}

// Resize updates the emulator's grid dimensions. Returns nil for now;
// vt10x.Resize doesn't surface errors.
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
//  2. each row, with SGR transitions emitted only when attrs change,
//     short rows padded via \x1b[K, lines separated by \r\n
//  3. final cursor positioning
//  4. cursor visibility (DECTCEM) per emulator state
func (v *VT) RenderSnapshot() []byte {
	v.mu.Lock()
	defer v.mu.Unlock()

	cols, rows := v.term.Size()
	if cols <= 0 || rows <= 0 {
		return []byte("\x1b[!p\x1b[2J\x1b[H")
	}

	var buf bytes.Buffer
	// If the session is on the alt screen (vim, htop, less, claude TUI…),
	// enter alt screen FIRST so the snapshot lands there. Otherwise the
	// next \x1b[?1049l from the live PTY would swap the client back to a
	// blank normal screen and discard everything we just painted.
	if v.term.Mode()&vt10x.ModeAltScreen != 0 {
		buf.WriteString("\x1b[?1049h")
	}
	// Soft reset + erase-display + home. Note: \x1b[!p is DECSTR.
	buf.WriteString("\x1b[!p\x1b[2J\x1b[H")

	// Track current SGR state; "no attrs, default colours" means we've
	// just emitted \x1b[m (or are at the start, post soft-reset).
	var (
		curMode int16     = 0
		curFG   vt10x.Color = vt10x.DefaultFG
		curBG   vt10x.Color = vt10x.DefaultBG
		atDefault         = true
	)

	for y := range rows {
		// Find last non-blank cell so we don't burn bytes on the
		// trailing spaces — emit \x1b[K instead.
		lastNonBlank := -1
		for x := cols - 1; x >= 0; x-- {
			g := v.term.Cell(x, y)
			if g.Char != 0 && g.Char != ' ' {
				lastNonBlank = x
				break
			}
			// A cell with non-default attrs but a space still counts
			// as visible — preserve highlighted blanks.
			if g.Mode != 0 || g.FG != vt10x.DefaultFG || g.BG != vt10x.DefaultBG {
				lastNonBlank = x
				break
			}
		}

		for x := 0; x <= lastNonBlank; x++ {
			g := v.term.Cell(x, y)
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

		// Reset to defaults before \x1b[K so the erase-to-EOL doesn't
		// inherit a coloured background from the last cell.
		if !atDefault {
			buf.WriteString("\x1b[m")
			curMode = 0
			curFG = vt10x.DefaultFG
			curBG = vt10x.DefaultBG
			atDefault = true
		}
		buf.WriteString("\x1b[K")
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
	default:
		// Sentinel / RGB-encoded — fall back to default.
		// vt10x stuffs RGB into 1<<24+r<<16+g<<8+b territory; rather
		// than try to decode here, leave the colour at default. Worst
		// case the snapshot is slightly less colourful than the live
		// stream — live output will fix it on the next byte.
	}
}
