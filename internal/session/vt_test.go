package session

import (
	"strings"
	"testing"

	"github.com/hinshun/vt10x"
)

// TestVTSnapshotRoundTrip writes a known sequence into one VT, captures
// its rendered snapshot, then feeds that snapshot into a fresh VT and
// asserts the visible cells (chars only) match. Ignoring exact SGR
// equality keeps the test resilient to minor SGR encoding differences;
// what matters for the bug fix is that the *visible state* round-trips.
func TestVTSnapshotRoundTrip(t *testing.T) {
	src := NewVT(20, 5)
	// "hello" then move to next line, then bold "world".
	if _, err := src.Write([]byte("hello\r\n\x1b[1mworld\x1b[m")); err != nil {
		t.Fatalf("write: %v", err)
	}
	snap := src.RenderSnapshot()
	if len(snap) == 0 {
		t.Fatal("empty snapshot")
	}
	if !strings.Contains(string(snap), "hello") {
		t.Errorf("snapshot missing 'hello': %q", snap)
	}
	if !strings.Contains(string(snap), "world") {
		t.Errorf("snapshot missing 'world': %q", snap)
	}

	dst := NewVT(20, 5)
	if _, err := dst.Write(snap); err != nil {
		t.Fatalf("replay write: %v", err)
	}

	for y := range 5 {
		var srcLine, dstLine strings.Builder
		for x := range 20 {
			sg := src.term.Cell(x, y)
			dg := dst.term.Cell(x, y)
			sc := sg.Char
			dc := dg.Char
			if sc == 0 {
				sc = ' '
			}
			if dc == 0 {
				dc = ' '
			}
			srcLine.WriteRune(sc)
			dstLine.WriteRune(dc)
		}
		if strings.TrimRight(srcLine.String(), " ") != strings.TrimRight(dstLine.String(), " ") {
			t.Errorf("row %d mismatch:\n src=%q\n dst=%q", y, srcLine.String(), dstLine.String())
		}
	}

	// Bold "world" attrs must survive the round-trip — guards against
	// SGR-bit drift in vt10x and against the writer dropping attrs.
	for x := range 5 { // "world" at row 1, cols 0..4
		sg := src.term.Cell(x, 1)
		dg := dst.term.Cell(x, 1)
		if sg.Mode&vtAttrBold == 0 {
			t.Fatalf("source bold not stored at (%d,1) — test setup wrong", x)
		}
		if dg.Mode&vtAttrBold == 0 {
			t.Errorf("bold attr lost at (%d,1) after round-trip: src.Mode=%b dst.Mode=%b", x, sg.Mode, dg.Mode)
		}
	}

	// Cursor should also round-trip.
	sc := src.term.Cursor()
	dc := dst.term.Cursor()
	if sc.X != dc.X || sc.Y != dc.Y {
		t.Errorf("cursor mismatch: src=(%d,%d) dst=(%d,%d)", sc.X, sc.Y, dc.X, dc.Y)
	}
}

// TestVTReverseVideoNoDoubleApply guards a real bug: vt10x stores
// reverse-video cells with FG/BG already swapped AND keeps the
// attrReverse bit. A naive snapshot that re-emits both the swapped
// colours and \x1b[7m makes the receiving terminal reverse them again,
// landing on the wrong colours for any selection bar / status line / fzf
// preview / vim visual-mode highlight.
func TestVTReverseVideoNoDoubleApply(t *testing.T) {
	src := NewVT(10, 1)
	// Red FG on white BG, then enable reverse, then write "X". After
	// vt10x's setChar, the cell is stored as FG=white, BG=red, with
	// attrReverse set.
	if _, err := src.Write([]byte("\x1b[31;47;7mX\x1b[m")); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Sanity: vt10x really did the pre-swap.
	g := src.term.Cell(0, 0)
	if g.Mode&vtAttrReverse == 0 {
		t.Fatalf("test setup wrong: reverse bit not set on stored cell")
	}
	if g.FG != 7 || g.BG != 1 { // 7=white(swapped FG), 1=red(swapped BG)
		t.Fatalf("test setup wrong: vt10x did not pre-swap as expected: FG=%v BG=%v", g.FG, g.BG)
	}

	dst := NewVT(10, 1)
	if _, err := dst.Write(src.RenderSnapshot()); err != nil {
		t.Fatalf("replay: %v", err)
	}

	// After replay the destination cell must end up with the SAME stored
	// colours as the source (vt10x will pre-swap them too on replay,
	// landing on the same FG/BG pair).
	d := dst.term.Cell(0, 0)
	if d.FG != g.FG || d.BG != g.BG {
		t.Errorf("reverse-video colours drifted: src FG=%v BG=%v, dst FG=%v BG=%v", g.FG, g.BG, d.FG, d.BG)
	}
	if d.Mode&vtAttrReverse == 0 {
		t.Errorf("reverse attr lost on round-trip")
	}
}

// TestVTAltScreenSnapshot guards that a snapshot taken while the session
// is in the alt-screen buffer enters alt-screen first, so a later
// \x1b[?1049l from the live PTY swaps cleanly without discarding the
// snapshot we just painted.
func TestVTAltScreenSnapshot(t *testing.T) {
	v := NewVT(10, 3)
	if _, err := v.Write([]byte("\x1b[?1049hALT")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if v.term.Mode()&vt10x.ModeAltScreen == 0 {
		t.Fatal("test setup wrong: vt10x did not switch to alt screen on \\x1b[?1049h")
	}
	snap := string(v.RenderSnapshot())
	if !strings.HasPrefix(snap, "\x1b[?1049h") {
		t.Errorf("alt-screen snapshot must enter alt screen first; got prefix %q", snap[:min(20, len(snap))])
	}
}

// TestVTResize sanity-checks that Resize doesn't blow up and the
// snapshot reflects the new dimensions.
func TestVTResize(t *testing.T) {
	v := NewVT(10, 3)
	if _, err := v.Write([]byte("abc")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := v.Resize(40, 10); err != nil {
		t.Fatalf("resize: %v", err)
	}
	snap := v.RenderSnapshot()
	if !strings.Contains(string(snap), "abc") {
		t.Errorf("snapshot missing 'abc' after resize: %q", snap)
	}
}
