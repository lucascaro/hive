package session

import (
	"strings"
	"testing"
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

	// Cursor should also round-trip.
	sc := src.term.Cursor()
	dc := dst.term.Cursor()
	if sc.X != dc.X || sc.Y != dc.Y {
		t.Errorf("cursor mismatch: src=(%d,%d) dst=(%d,%d)", sc.X, sc.Y, dc.X, dc.Y)
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
