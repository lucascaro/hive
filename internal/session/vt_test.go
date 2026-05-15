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

// TestVTSnapshotScrollbackIgnoresClearAcrossChunks guards the
// chunk-boundary case for `\x1b[H\x1b[2J`. PTY reads arrive in
// arbitrary chunks, so the eviction heuristic must reject a clear that
// arrives in its own Write call (preRows = [content, blank…], post =
// all blank). Without the post-top-blank bail-out, the largest blank
// preRows[kk] matches the blank post-top and pre-clear content leaks
// into history.
func TestVTSnapshotScrollbackIgnoresClearAcrossChunks(t *testing.T) {
	v := NewVT(20, 3)
	if _, err := v.Write([]byte("line-A\r\nline-B\r\n")); err != nil {
		t.Fatalf("write content: %v", err)
	}
	// Separate Write — simulates a PTY chunk boundary between content
	// and the clear sequence.
	if _, err := v.Write([]byte("\x1b[H\x1b[2J")); err != nil {
		t.Fatalf("write clear: %v", err)
	}
	if got := len(v.history); got != 0 {
		t.Errorf("history should be empty after chunk-boundary clear; got %d entries: %q", got, v.history)
	}
	snap := string(v.RenderSnapshot())
	if strings.Contains(snap, "line-A") || strings.Contains(snap, "line-B") {
		t.Errorf("snapshot leaked cleared content into scrollback: %q", snap)
	}
}

// TestVTSnapshotScrollbackPreservesBlankLines guards that real blank
// lines in the output keep their position when scrolled off the
// viewport. The eviction push loop must not drop blank rows once a
// scroll has been confidently detected — that would collapse paragraph
// spacing in the preserved scrollback.
func TestVTSnapshotScrollbackPreservesBlankLines(t *testing.T) {
	const cols, rows = 20, 3
	v := NewVT(cols, rows)
	// Write each line in its own chunk so the eviction heuristic runs
	// per-line; that mirrors how the PTY pipeline actually delivers
	// output. Sequence: "first", blank, "second", blank, "third", then
	// enough filler to scroll the early lines off the visible viewport.
	chunks := []string{
		"first\r\n",
		"\r\n",
		"second\r\n",
		"\r\n",
		"third\r\n",
	}
	for _, c := range chunks {
		if _, err := v.Write([]byte(c)); err != nil {
			t.Fatalf("write %q: %v", c, err)
		}
	}
	for range rows + 2 {
		if _, err := v.Write([]byte("filler\r\n")); err != nil {
			t.Fatalf("write filler: %v", err)
		}
	}
	// Replay the snapshot into a wider, taller VT so all history rows
	// land in the visible region and we can read row contents directly.
	snap := v.RenderSnapshot()
	dst := NewVT(cols, 30)
	if _, err := dst.Write(snap); err != nil {
		t.Fatalf("replay: %v", err)
	}
	// Find "first" row, then assert the row immediately after it is blank,
	// then "second" two rows below "first".
	rowText := func(y int) string {
		var b strings.Builder
		for x := range cols {
			ch := dst.term.Cell(x, y).Char
			if ch == 0 {
				ch = ' '
			}
			b.WriteRune(ch)
		}
		return strings.TrimRight(b.String(), " ")
	}
	firstY, secondY := -1, -1
	for y := 0; y < 30; y++ {
		txt := rowText(y)
		if firstY < 0 && strings.Contains(txt, "first") {
			firstY = y
		} else if firstY >= 0 && secondY < 0 && strings.Contains(txt, "second") {
			secondY = y
		}
	}
	if firstY < 0 {
		t.Fatalf("replay missing 'first' row; snapshot=%q", snap)
	}
	if secondY < 0 {
		t.Fatalf("replay missing 'second' row; snapshot=%q", snap)
	}
	// Must be at least one blank row separating "first" and "second" —
	// without that, the eviction push-loop is dropping blank rows and
	// collapsing paragraph spacing in the preserved scrollback.
	if secondY-firstY < 2 {
		t.Errorf("'second' appears immediately after 'first' (firstY=%d, secondY=%d): blank separator was dropped", firstY, secondY)
	}
	for y := firstY + 1; y < secondY; y++ {
		if got := rowText(y); got != "" {
			t.Errorf("expected blank row between 'first' and 'second' at y=%d, got %q", y, got)
		}
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

// TestVT_RingCapturesAllBytes verifies that the byte ring buffer
// retains every byte handed to Write, in order, until it fills the
// cap. The ring is what powers the GUI's resize-replay path — if it
// loses bytes, replay produces a corrupted xterm state.
func TestVT_RingCapturesAllBytes(t *testing.T) {
	v := NewVT(80, 24)
	chunks := [][]byte{
		[]byte("hello "),
		[]byte("\x1b[31mred\x1b[m "),
		[]byte("world\r\n"),
		[]byte("second line\r\n"),
	}
	var want bytes.Buffer
	for _, c := range chunks {
		if _, err := v.Write(c); err != nil {
			t.Fatalf("write: %v", err)
		}
		want.Write(c)
	}
	got := v.RingBytes()
	if !bytes.Equal(got, want.Bytes()) {
		t.Errorf("ring mismatch:\n  got  %q\n  want %q", got, want.Bytes())
	}
}

// TestVT_RingOverflowDropsAtSafeBoundary verifies that when the ring
// trims to stay under cap, the surviving prefix starts at an ESC byte
// or a UTF-8 leading byte — not a multibyte continuation byte or a
// raw parameter byte inside an active CSI. Otherwise the first replay
// frame would be parsed as garbage by xterm.js.
func TestVT_RingOverflowDropsAtSafeBoundary(t *testing.T) {
	v := NewVT(80, 24)
	// Build a stream that, when trimmed mid-byte, would land inside a
	// UTF-8 multibyte run. Interleave plain ASCII + CSI + a UTF-8 rune.
	var stream bytes.Buffer
	for i := 0; i < (ringCap/16)+128; i++ {
		stream.WriteString("\x1b[32mAB\xe2\x9c\x93\x1b[m ") // CSI + "AB" + ✓ + reset + space
	}
	if _, err := v.Write(stream.Bytes()); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := v.RingBytes()
	if len(got) == 0 {
		t.Fatalf("ring empty after overflowing write")
	}
	if len(got) > ringCap {
		t.Errorf("ring exceeds cap: got %d, cap %d", len(got), ringCap)
	}
	first := got[0]
	// Acceptable: ESC, ASCII byte (<0x80), or UTF-8 lead (>=0xC0).
	// Forbidden: UTF-8 continuation byte (0x80–0xBF, mask 0x80=set & 0x40=clear).
	if first&0xC0 == 0x80 {
		t.Errorf("ring starts inside multibyte sequence: first byte %#x", first)
	}
}

// TestVT_ReplayReproducesScreen replays the byte ring into a fresh VT
// and asserts the resulting visible screen matches the original. This
// is the property the GUI's resize-replay path depends on: replaying
// the ring into a freshly-reset xterm.js produces the same visible
// state as the live session.
func TestVT_ReplayReproducesScreen(t *testing.T) {
	src := NewVT(40, 10)
	script := []string{
		"first line\r\n",
		"\x1b[1mbold here\x1b[m\r\n",
		"\x1b[31mred\x1b[m and \x1b[32mgreen\x1b[m\r\n",
		"  indented\r\n",
		"final\r\n",
	}
	for _, s := range script {
		if _, err := src.Write([]byte(s)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	ring := src.RingBytes()
	if len(ring) == 0 {
		t.Fatal("ring empty")
	}

	dst := NewVT(40, 10)
	if _, err := dst.Write(ring); err != nil {
		t.Fatalf("replay write: %v", err)
	}

	// Compare visible cells (chars only).
	src.mu.Lock()
	dst.mu.Lock()
	defer src.mu.Unlock()
	defer dst.mu.Unlock()
	cols, rows := src.term.Size()
	for y := 0; y < rows; y++ {
		var s, d strings.Builder
		for x := 0; x < cols; x++ {
			cs := src.term.Cell(x, y)
			cd := dst.term.Cell(x, y)
			cc := cs.Char
			if cc == 0 {
				cc = ' '
			}
			dc := cd.Char
			if dc == 0 {
				dc = ' '
			}
			s.WriteRune(cc)
			d.WriteRune(dc)
		}
		got := strings.TrimRight(d.String(), " ")
		want := strings.TrimRight(s.String(), " ")
		if got != want {
			t.Errorf("row %d mismatch:\n  src: %q\n  dst: %q", y, want, got)
		}
	}

	// vt10x's Cursor() doesn't have stable equality semantics across
	// instances (offsets vary), but x/y should match.
	if src.term.Cursor().X != dst.term.Cursor().X ||
		src.term.Cursor().Y != dst.term.Cursor().Y {
		t.Errorf("cursor mismatch: src=(%d,%d) dst=(%d,%d)",
			src.term.Cursor().X, src.term.Cursor().Y,
			dst.term.Cursor().X, dst.term.Cursor().Y)
	}
}

// TestVT_RingOverflowPrefersNewlineBoundary verifies that the
// overflow-trim path prefers a newline as its replay boundary over
// CSI / UTF-8 candidates. Newlines never appear inside CSI sequences,
// so a replay starting on `\n` is guaranteed not to emit literal CSI
// parameter bytes as visible text.
func TestVT_RingOverflowPrefersNewlineBoundary(t *testing.T) {
	v := NewVT(80, 24)
	// Stream with explicit newlines + CSI; overflow once.
	var stream bytes.Buffer
	for i := 0; i < (ringCap/8)+200; i++ {
		stream.WriteString("\x1b[31mhi\x1b[m\r\n")
	}
	if _, err := v.Write(stream.Bytes()); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := v.RingBytes()
	if len(got) == 0 {
		t.Fatalf("ring empty")
	}
	// First byte should be a newline or an ESC — NOT a CSI parameter
	// digit / 'm' / 'h' / 'i' which would render as literal text.
	first := got[0]
	if first != 0x0A && first != 0x0D && first != 0x1B {
		t.Errorf("ring starts at unsafe byte %#x (%q); expected newline or ESC", first, string([]byte{first}))
	}
}

// TestVT_InsideUnterminatedEscape exercises the helper directly.
func TestVT_InsideUnterminatedEscape(t *testing.T) {
	cases := []struct {
		name string
		buf  string
		pos  int
		want bool
	}{
		{"plain text", "hello world", 5, false},
		{"after CSI terminator", "\x1b[31mhi", 5, false}, // pos=5 is the 'h' after 'm'
		{"inside CSI param", "\x1b[31mhi", 3, true},      // pos=3 is the '1' inside [31m
		{"inside OSC", "\x1b]2;title", 4, true},
		{"after OSC BEL", "\x1b]2;title\x07more", 10, false},
		{"no ESC nearby", "abcdefghij\x1b[mlater", 5, false},
	}
	for _, c := range cases {
		got := insideUnterminatedEscape([]byte(c.buf), c.pos, 64)
		if got != c.want {
			t.Errorf("%s: pos=%d got %v want %v", c.name, c.pos, got, c.want)
		}
	}
}
