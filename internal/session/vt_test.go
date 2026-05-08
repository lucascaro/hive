package session

import (
	"bytes"
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

// TestWriteColorRGBForeground verifies RGB-encoded colors emit a
// truecolor SGR fragment instead of being dropped to default.
func TestWriteColorRGBForeground(t *testing.T) {
	var buf bytes.Buffer
	writeColor(&buf, vt10x.Color(0xFF8040), true)
	if got, want := buf.String(), ";38;2;255;128;64"; got != want {
		t.Errorf("RGB FG: got %q, want %q", got, want)
	}
}

// TestWriteColorRGBBackground verifies RGB BG emits ;48;2;…
func TestWriteColorRGBBackground(t *testing.T) {
	var buf bytes.Buffer
	writeColor(&buf, vt10x.Color(0xFF8040), false)
	if got, want := buf.String(), ";48;2;255;128;64"; got != want {
		t.Errorf("RGB BG: got %q, want %q", got, want)
	}
}

// TestWriteColorSentinelsNoOutput guards against decoding sentinels
// (DefaultFG/DefaultBG/DefaultCursor at 1<<24+i) as if they were RGB.
func TestWriteColorSentinelsNoOutput(t *testing.T) {
	for _, c := range []vt10x.Color{vt10x.DefaultFG, vt10x.DefaultBG, vt10x.DefaultCursor} {
		var buf bytes.Buffer
		writeColor(&buf, c, true)
		if buf.Len() != 0 {
			t.Errorf("sentinel %d should emit nothing, got %q", c, buf.String())
		}
	}
}

// TestVTSnapshotRoundTripRGB verifies a 24-bit RGB foreground survives
// snapshot → replay so GUI reattach preserves modern prompt/TUI styling.
func TestVTSnapshotRoundTripRGB(t *testing.T) {
	src := NewVT(10, 1)
	if _, err := src.Write([]byte("\x1b[38;2;200;100;50mhi\x1b[m")); err != nil {
		t.Fatalf("write: %v", err)
	}
	srcFG := src.term.Cell(0, 0).FG
	if srcFG != vt10x.Color(200<<16|100<<8|50) {
		t.Fatalf("test setup wrong: vt10x did not store RGB as expected, got %v", srcFG)
	}

	dst := NewVT(10, 1)
	if _, err := dst.Write(src.RenderSnapshot()); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if got := dst.term.Cell(0, 0).FG; got != srcFG {
		t.Errorf("RGB FG lost across snapshot: src=%v dst=%v", srcFG, got)
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

// TestVTSnapshotPreservesScrollback guards the documented contract that
// reattach repaints include lines that scrolled off the visible
// viewport. Regression: PR #141 swapped raw-byte replay for a vt10x
// snapshot of the visible region only, dropping all scrollback.
func TestVTSnapshotPreservesScrollback(t *testing.T) {
	const cols, rows = 20, 5
	v := NewVT(cols, rows)
	// Write rows*3 distinct lines so rows*2 of them scroll off.
	for i := range rows*3 {
		line := []byte("line-")
		line = append(line, byte('0'+i/10), byte('0'+i%10))
		line = append(line, '\r', '\n')
		if _, err := v.Write(line); err != nil {
			t.Fatalf("write line %d: %v", i, err)
		}
	}
	snap := string(v.RenderSnapshot())
	for i := range rows*2 {
		want := "line-" + string([]byte{byte('0' + i/10), byte('0' + i%10)})
		if !strings.Contains(snap, want) {
			t.Errorf("snapshot missing scrollback line %q", want)
		}
	}
}

// TestVTSnapshotScrollbackCappedAtHistoryRows guards that the ring
// drops the oldest entries once over capacity, so memory stays bounded.
func TestVTSnapshotScrollbackCappedAtHistoryRows(t *testing.T) {
	const cols, rows = 20, 5
	const extra = 50
	v := NewVT(cols, rows)
	total := rows + historyRows + extra
	for i := range total {
		// 4-digit line numbers so each line is unique.
		line := []byte("line-")
		for d := 1000; d > 0; d /= 10 {
			line = append(line, byte('0'+(i/d)%10))
		}
		line = append(line, '\r', '\n')
		if _, err := v.Write(line); err != nil {
			t.Fatalf("write line %d: %v", i, err)
		}
	}
	if got := len(v.history); got != historyRows {
		t.Errorf("history len = %d, want %d", got, historyRows)
	}
	snap := string(v.RenderSnapshot())
	// The first `extra` lines must have been dropped from the ring.
	for i := range extra {
		needle := "line-"
		for d := 1000; d > 0; d /= 10 {
			needle += string(byte('0' + (i/d)%10))
		}
		if strings.Contains(snap, needle) {
			t.Errorf("snapshot still contains dropped line %q", needle)
		}
	}
}

// TestVTSnapshotScrollbackSkippedOnAltScreen guards that alt-screen
// sessions get the existing snapshot path: enter alt-screen first, no
// scrollback prepended. Vim/htop/claude TUI own their own redraw.
func TestVTSnapshotScrollbackSkippedOnAltScreen(t *testing.T) {
	v := NewVT(20, 3)
	for range 10 {
		if _, err := v.Write([]byte("normal-line\r\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if _, err := v.Write([]byte("\x1b[?1049hALT-CONTENT")); err != nil {
		t.Fatalf("alt: %v", err)
	}
	snap := string(v.RenderSnapshot())
	if !strings.HasPrefix(snap, "\x1b[?1049h") {
		t.Errorf("alt-screen snapshot must enter alt screen first; got prefix %q", snap[:min(20, len(snap))])
	}
	if strings.Contains(snap, "normal-line") {
		t.Errorf("alt-screen snapshot must not include normal-screen scrollback")
	}
	if !strings.Contains(snap, "ALT-CONTENT") {
		t.Errorf("alt-screen snapshot missing alt content: %q", snap)
	}
}

// TestVTSnapshotScrollbackSurvivesAltScreenRoundTrip guards that the
// ring keeps its normal-screen captures while alt-screen is up, so a
// vim invocation in the middle of a session doesn't erase prior
// scrollback when the user exits vim.
func TestVTSnapshotScrollbackSurvivesAltScreenRoundTrip(t *testing.T) {
	v := NewVT(20, 3)
	for range 10 {
		if _, err := v.Write([]byte("normal-line\r\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if _, err := v.Write([]byte("\x1b[?1049hALT")); err != nil {
		t.Fatalf("alt enter: %v", err)
	}
	if _, err := v.Write([]byte("\x1b[?1049l")); err != nil {
		t.Fatalf("alt exit: %v", err)
	}
	snap := string(v.RenderSnapshot())
	if !strings.Contains(snap, "normal-line") {
		t.Errorf("post-alt snapshot lost normal-screen scrollback: %q", snap)
	}
}

// TestVTSnapshotScrollbackPreservesSGR guards that styled lines that
// scroll off the viewport keep their SGR attrs in the captured ring,
// not just plain text.
func TestVTSnapshotScrollbackPreservesSGR(t *testing.T) {
	v := NewVT(20, 3)
	// Bold red colored line, then enough plain lines to scroll it off.
	if _, err := v.Write([]byte("\x1b[1;31mRED-BOLD\x1b[m\r\n")); err != nil {
		t.Fatalf("colored: %v", err)
	}
	for range 10 {
		if _, err := v.Write([]byte("plain\r\n")); err != nil {
			t.Fatalf("plain: %v", err)
		}
	}
	snap := string(v.RenderSnapshot())
	if !strings.Contains(snap, "RED-BOLD") {
		t.Fatalf("snapshot missing the colored scrollback line: %q", snap)
	}
	// History entry for "RED-BOLD" must contain bold (\x1b[0;1) and
	// red (;31) before the text. Search for the substring up to the
	// "RED-BOLD" occurrence.
	idx := strings.Index(snap, "RED-BOLD")
	if idx < 0 {
		t.Fatal("RED-BOLD not found")
	}
	prefix := snap[:idx]
	if !strings.Contains(prefix, ";1") {
		t.Errorf("scrollback line lost bold SGR: %q", prefix)
	}
	if !strings.Contains(prefix, ";31") {
		t.Errorf("scrollback line lost red SGR: %q", prefix)
	}
}

// TestVTSnapshotScrollbackIgnoresClear guards that \x1b[2J wipes the
// pre-rows in place (no eviction match), matching xterm.js's default
// "clear doesn't push to scrollback" behavior.
func TestVTSnapshotScrollbackIgnoresClear(t *testing.T) {
	v := NewVT(20, 3)
	if _, err := v.Write([]byte("before-clear\r\n\x1b[H\x1b[2J")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := len(v.history); got != 0 {
		t.Errorf("history should be empty after \\x1b[2J; got %d entries", got)
	}
}

// TestVTSnapshotScrollbackResize guards that Resize doesn't blow up
// when the ring is non-empty, and the resized snapshot still
// round-trips the visible region.
func TestVTSnapshotScrollbackResize(t *testing.T) {
	v := NewVT(20, 3)
	for range 10 {
		if _, err := v.Write([]byte("line\r\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := v.Resize(40, 10); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if _, err := v.Write([]byte("after-resize")); err != nil {
		t.Fatalf("write: %v", err)
	}
	snap := string(v.RenderSnapshot())
	if !strings.Contains(snap, "after-resize") {
		t.Errorf("snapshot missing post-resize content: %q", snap)
	}
}
