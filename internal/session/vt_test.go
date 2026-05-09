package session

import (
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

// rowText concatenates display content of row y across cols. Wide cells
// contribute their grapheme once and the shadow cell contributes "" so
// the resulting string lines up with display columns.
func rowText(v *VT, y, cols int) string {
	var b strings.Builder
	for x := range cols {
		c := v.term.CellAt(x, y)
		if c == nil || c.IsZero() {
			b.WriteByte(' ')
			continue
		}
		if c.Content == "" {
			// Shadow half of a wide cell — already accounted for by the
			// preceding cell's wide grapheme.
			continue
		}
		b.WriteString(c.Content)
	}
	return b.String()
}

// TestVTSnapshotRoundTrip writes a known sequence into one VT, captures
// its rendered snapshot, then feeds that snapshot into a fresh VT and
// asserts the visible cells match. Bold attribute on "world" must
// survive the round-trip.
func TestVTSnapshotRoundTrip(t *testing.T) {
	src := NewVT(20, 5)
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
		s := strings.TrimRight(rowText(src, y, 20), " ")
		d := strings.TrimRight(rowText(dst, y, 20), " ")
		if s != d {
			t.Errorf("row %d mismatch:\n src=%q\n dst=%q", y, s, d)
		}
	}

	// Bold on "world" (row 1, cols 0..4) must round-trip.
	for x := range 5 {
		sg := src.term.CellAt(x, 1)
		dg := dst.term.CellAt(x, 1)
		if sg == nil || sg.Style.Attrs&uv.AttrBold == 0 {
			t.Fatalf("source bold not stored at (%d,1) — test setup wrong", x)
		}
		if dg == nil || dg.Style.Attrs&uv.AttrBold == 0 {
			t.Errorf("bold attr lost at (%d,1) after round-trip", x)
		}
	}

	// Cursor should also round-trip.
	sc := src.term.CursorPosition()
	dc := dst.term.CursorPosition()
	if sc.X != dc.X || sc.Y != dc.Y {
		t.Errorf("cursor mismatch: src=(%d,%d) dst=(%d,%d)", sc.X, sc.Y, dc.X, dc.Y)
	}
}

// TestVTSnapshotWideCharRoundTrip is the regression test for #142.
// A line of CJK characters must round-trip with the same display-column
// layout, so xterm.js paints the snapshot identically to what it
// painted from the live byte stream.
func TestVTSnapshotWideCharRoundTrip(t *testing.T) {
	src := NewVT(40, 5)
	// Wide chars on row 0, narrow content on row 1 — the narrow row
	// guards that we don't accidentally shift unrelated rows.
	if _, err := src.Write([]byte("こんにちは世界\r\nhello")); err != nil {
		t.Fatalf("write: %v", err)
	}

	dst := NewVT(40, 5)
	if _, err := dst.Write(src.RenderSnapshot()); err != nil {
		t.Fatalf("replay: %v", err)
	}

	// Each wide grapheme should land at the same x and have Width == 2
	// in both source and destination.
	wideXs := []int{0, 2, 4, 6, 8, 10, 12}
	for _, x := range wideXs {
		s := src.term.CellAt(x, 0)
		d := dst.term.CellAt(x, 0)
		if s == nil || d == nil {
			t.Fatalf("nil cell at (%d,0): src=%v dst=%v", x, s, d)
		}
		if s.Content != d.Content {
			t.Errorf("wide cell (%d,0) content drift: src=%q dst=%q", x, s.Content, d.Content)
		}
		if s.Width != 2 || d.Width != 2 {
			t.Errorf("wide cell (%d,0) width drift: src.Width=%d dst.Width=%d", x, s.Width, d.Width)
		}
	}

	// Narrow row 1 must match too.
	if got, want := rowText(dst, 1, 40), rowText(src, 1, 40); strings.TrimRight(got, " ") != strings.TrimRight(want, " ") {
		t.Errorf("narrow row drifted after wide-char round-trip: src=%q dst=%q", want, got)
	}
}

var cupRE = regexp.MustCompile(`\x1b\[(\d+);(\d+)H`)

// TestVTSnapshotWideCharCursorPosition guards the specific failure
// mode in #142: the snapshot's final CUP must address xterm.js display
// columns, not raw cell index. Writing two wide chars then "abc" lands
// the live cursor at display column 8 (1-indexed); the snapshot must
// say so.
func TestVTSnapshotWideCharCursorPosition(t *testing.T) {
	v := NewVT(20, 3)
	if _, err := v.Write([]byte("世界abc")); err != nil {
		t.Fatalf("write: %v", err)
	}

	cur := v.term.CursorPosition()
	wantCol := cur.X + 1
	wantRow := cur.Y + 1
	if wantCol != 8 {
		// Sanity check: 2 wide chars (4 cols) + "abc" (3 cols) = cursor
		// at col 7 (0-indexed) → wantCol == 8. If the emulator stores
		// columns differently this test's premise is wrong.
		t.Fatalf("test premise: expected cursor at display col 8, got %d", wantCol)
	}

	snap := string(v.RenderSnapshot())

	// Find the LAST CUP in the snapshot — that's the cursor positioning
	// we emit at the end. Earlier CUPs are part of the soft-reset preface
	// (\x1b[H = \x1b[1;1H is implicit) or absent.
	matches := cupRE.FindAllStringSubmatch(snap, -1)
	if len(matches) == 0 {
		t.Fatalf("no CUP in snapshot: %q", snap)
	}
	last := matches[len(matches)-1]
	gotRow, _ := strconv.Atoi(last[1])
	gotCol, _ := strconv.Atoi(last[2])
	if gotRow != wantRow || gotCol != wantCol {
		t.Errorf("snapshot CUP wrong: got (%d,%d), want (%d,%d). snap=%q", gotRow, gotCol, wantRow, wantCol, snap)
	}
}

// TestVTSnapshotWideCharOverlay covers failure mode #4 from the spec:
// an absolute CUP that targets a column inside a wide-rune region
// should land at the same display column on round-trip.
func TestVTSnapshotWideCharOverlay(t *testing.T) {
	src := NewVT(20, 3)
	// "世界" fills display cols 0..3. CUP to row 1, col 6 (1-indexed),
	// then write "X". Live: 世界  X (with two spaces between 界 and X).
	if _, err := src.Write([]byte("世界\x1b[1;6HX")); err != nil {
		t.Fatalf("write: %v", err)
	}

	dst := NewVT(20, 3)
	if _, err := dst.Write(src.RenderSnapshot()); err != nil {
		t.Fatalf("replay: %v", err)
	}

	// 'X' must land at display col 5 in both source and destination.
	sx := src.term.CellAt(5, 0)
	dx := dst.term.CellAt(5, 0)
	if sx == nil || sx.Content != "X" {
		t.Fatalf("test premise: expected 'X' at col 5 on source, got %+v", sx)
	}
	if dx == nil || dx.Content != "X" {
		t.Errorf("CUP-overlay-into-wide-region drifted: expected 'X' at col 5 on destination, got %+v", dx)
	}
}

// TestVTReverseVideoNoDoubleApply guards against a class of bug where
// reverse-video colours are emitted with the swap pre-applied AND \x1b[7m,
// causing the receiving terminal to swap them again. The new emulator
// uses ultraviolet's Style.Diff for SGR encoding which should not
// double-apply, but the test stays valuable as a regression net.
func TestVTReverseVideoNoDoubleApply(t *testing.T) {
	src := NewVT(10, 1)
	if _, err := src.Write([]byte("\x1b[31;47;7mX\x1b[m")); err != nil {
		t.Fatalf("write: %v", err)
	}
	srcCell := src.term.CellAt(0, 0)
	if srcCell == nil || srcCell.Style.Attrs&uv.AttrReverse == 0 {
		t.Fatalf("test premise: reverse attr not stored on source cell: %+v", srcCell)
	}

	dst := NewVT(10, 1)
	if _, err := dst.Write(src.RenderSnapshot()); err != nil {
		t.Fatalf("replay: %v", err)
	}
	dstCell := dst.term.CellAt(0, 0)
	if dstCell == nil {
		t.Fatal("destination cell missing after replay")
	}
	if dstCell.Style.Attrs&uv.AttrReverse == 0 {
		t.Errorf("reverse attr lost on round-trip")
	}
	// Foreground / background colours must match — if the snapshot
	// double-applied reverse, src's red FG would land as white on dst.
	if !srcCell.Style.Equal(&dstCell.Style) {
		t.Errorf("style drift after reverse-video round-trip: src=%+v dst=%+v", srcCell.Style, dstCell.Style)
	}
}

// TestVTSnapshotRoundTripRGB verifies a 24-bit RGB foreground survives
// snapshot → replay so GUI reattach preserves modern prompt/TUI styling.
// Originally landed as #144 against the vt10x backend; the swap to
// charmbracelet/x/vt should keep this property without per-color SGR
// gymnastics — guarding against a regression in the new emulator's
// renderer.
func TestVTSnapshotRoundTripRGB(t *testing.T) {
	src := NewVT(10, 1)
	if _, err := src.Write([]byte("\x1b[38;2;200;100;50mhi\x1b[m")); err != nil {
		t.Fatalf("write: %v", err)
	}
	srcCell := src.term.CellAt(0, 0)
	if srcCell == nil || srcCell.Style.Fg == nil {
		t.Fatalf("test setup wrong: source FG color not stored: %+v", srcCell)
	}

	dst := NewVT(10, 1)
	if _, err := dst.Write(src.RenderSnapshot()); err != nil {
		t.Fatalf("replay: %v", err)
	}
	dstCell := dst.term.CellAt(0, 0)
	if dstCell == nil || dstCell.Style.Fg == nil {
		t.Fatalf("destination cell missing FG after replay: %+v", dstCell)
	}
	// Compare RGBA values — Style.Equal can be too strict if the
	// underlying color.Color implementation differs by concrete type
	// while encoding the same RGBA.
	sr, sg, sb, sa := srcCell.Style.Fg.RGBA()
	dr, dg, db, da := dstCell.Style.Fg.RGBA()
	if sr != dr || sg != dg || sb != db || sa != da {
		t.Errorf("RGB FG drifted across snapshot: src=%v dst=%v", srcCell.Style.Fg, dstCell.Style.Fg)
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
	if !v.term.IsAltScreen() {
		t.Fatal("test setup wrong: emulator did not switch to alt screen on \\x1b[?1049h")
	}
	snap := string(v.RenderSnapshot())
	if !strings.HasPrefix(snap, "\x1b[?1049h") {
		t.Errorf("alt-screen snapshot must enter alt screen first; got prefix %q", snap[:min(20, len(snap))])
	}
}

// TestVTWriteDoesNotBlockOnQueries guards against a real bug class
// from the charmbracelet/x/vt swap: the underlying emulator answers
// terminal queries (DA1/DA2, mode reports, color queries, in-band
// resize) by writing to an unbuffered io.Pipe. Without a drainer
// goroutine the FIRST query from an agent would block vt.Write
// indefinitely, which blocks deliver()'s critical section, which
// starves every client's live byte stream — agent TUIs end up blank
// in the GUI while plain shells (which rarely query) work fine.
func TestVTWriteDoesNotBlockOnQueries(t *testing.T) {
	v := NewVT(80, 24)
	// DA1 ("\x1b[c"), DA2 ("\x1b[>c"), DECRQM mode report, OSC 10 color
	// query — a representative slice of what agents emit on startup.
	queries := []byte("\x1b[c\x1b[>c\x1b[?25$p\x1b]10;?\x07hello")
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := v.Write(queries); err != nil {
			t.Errorf("vt.Write: %v", err)
		}
	}()
	select {
	case <-done:
		// Snapshot must still produce the literal content the agent
		// wrote alongside the queries.
		if !strings.Contains(string(v.RenderSnapshot()), "hello") {
			t.Errorf("snapshot missing 'hello' after queries: %q", v.RenderSnapshot())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("vt.Write blocked on terminal-query response — drainer goroutine not running")
	}
}

// TestVTCloseReleasesDrainer guards that VT.Close unblocks the
// internal drainer goroutine. Without it, every closed session would
// leak a goroutine and a pinned emulator. We probe goroutine count
// before/after Close on a tight loop of throwaway VTs — a leak at
// scale shows up as a steadily climbing count, but a few iterations
// is enough to confirm Close actually drains.
func TestVTCloseReleasesDrainer(t *testing.T) {
	const n = 50
	before := runtime.NumGoroutine()
	for range n {
		v := NewVT(80, 24)
		// Trigger at least one query response to make sure the drainer
		// has had bytes to consume — guards against trivial paths where
		// the goroutine exits before doing any work.
		if _, err := v.Write([]byte("\x1b[c")); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := v.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
	// Goroutines unwind asynchronously; give the scheduler a beat.
	deadline := time.Now().Add(2 * time.Second)
	var after int
	for time.Now().Before(deadline) {
		runtime.Gosched()
		after = runtime.NumGoroutine()
		if after-before < 5 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("drainer goroutines leaked: before=%d after=%d (created %d VTs)", before, after, n)
}

// TestVTResize sanity-checks that Resize doesn't blow up and the
// snapshot reflects content placed before the resize.
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
