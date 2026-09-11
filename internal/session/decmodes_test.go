package session

import (
	"bytes"
	"strings"
	"testing"
)

// The bug these guard: a program enables bracketed paste (and mouse
// tracking, and app-cursor keys) once at startup. Any reattach or
// width-changing replay repaints the client from RenderSnapshot, whose
// DECSTR preamble clears exactly those modes — and nothing re-asserted
// them, so the tile lost them for the life of the session. Symptom:
// a >1 KiB paste arrives unframed and the agent splits it at the macOS
// PTY input-buffer boundary into several "pasted text" chunks.

func TestSnapshotRestoresBracketedPaste(t *testing.T) {
	v := NewVT(80, 24)
	// What a real agent sends on startup, interleaved with output so the
	// scanner has to find it in a stream rather than at a chunk boundary.
	v.Write([]byte("hello\x1b[?2004h world\r\n"))

	snap := v.RenderSnapshot()
	if !bytes.Contains(snap, []byte("\x1b[?2004h")) {
		t.Fatalf("snapshot does not re-enable bracketed paste.\nsnapshot: %q", snap)
	}
	// Ordering is the whole point: DECSTR must not land after the
	// re-assert, or it clears what we just restored.
	if bytes.Index(snap, []byte("\x1b[!p")) > bytes.Index(snap, []byte("\x1b[?2004h")) {
		t.Fatal("soft reset comes after the mode restore; it would clear it")
	}
}

func TestSnapshotRestoresMouseAndCursorModes(t *testing.T) {
	v := NewVT(80, 24)
	// A TUI's typical opening: app cursor keys, button-event mouse
	// tracking with SGR encoding, focus reporting, bracketed paste.
	v.Write([]byte("\x1b[?1h\x1b[?1002;1006h\x1b[?1004h\x1b[?2004h"))

	snap := v.RenderSnapshot()
	for _, mode := range []string{"\x1b[?1h", "\x1b[?1002h", "\x1b[?1006h", "\x1b[?1004h", "\x1b[?2004h"} {
		if !bytes.Contains(snap, []byte(mode)) {
			t.Errorf("snapshot is missing %q", mode)
		}
	}
}

func TestSnapshotOmitsDisabledModes(t *testing.T) {
	v := NewVT(80, 24)
	// Enabled then disabled: DECSTR already leaves the client with the
	// mode off, so re-asserting anything would be wrong.
	v.Write([]byte("\x1b[?2004h\x1b[?1002h"))
	v.Write([]byte("\x1b[?2004l"))

	snap := v.RenderSnapshot()
	if bytes.Contains(snap, []byte("\x1b[?2004h")) {
		t.Error("snapshot re-enables a mode the program turned off")
	}
	if !bytes.Contains(snap, []byte("\x1b[?1002h")) {
		t.Error("snapshot dropped a mode that is still on")
	}
}

// A PTY read is cut wherever the kernel happens to cut it, so a mode
// sequence routinely arrives in two pieces. Scanning each Write in
// isolation would miss it — and miss it silently.
func TestModeSequenceSplitAcrossWrites(t *testing.T) {
	for _, split := range []int{1, 2, 3, 4, 5, 6, 7} {
		seq := "\x1b[?2004h"
		v := NewVT(80, 24)
		v.Write([]byte(seq[:split]))
		v.Write([]byte(seq[split:]))

		if !bytes.Contains(v.RenderSnapshot(), []byte("\x1b[?2004h")) {
			t.Errorf("split after %d byte(s): mode was not tracked", split)
		}
	}
}

func TestReplayBytesAppendsModesAfterRing(t *testing.T) {
	v := NewVT(80, 24)
	v.Write([]byte("\x1b[?2004hsome output\r\n"))

	replay := v.ReplayBytes()
	if !bytes.HasSuffix(replay, []byte("\x1b[?2004h")) {
		t.Fatalf("replay must end with the mode restore so the ring cannot\n"+
			"overwrite it; got tail %q", tail(replay, 24))
	}
	if !bytes.Contains(replay, []byte("some output")) {
		t.Error("replay dropped the ring contents")
	}
}

// The ring is capped, so on a long session the original mode sequences
// scroll out of it entirely. That is precisely when a re-replay used to
// produce a tile with bracketed paste off, and it is why ReplayBytes
// appends live state instead of trusting the ring.
func TestReplayRestoresModesAfterRingOverflow(t *testing.T) {
	v := NewVT(80, 24)
	v.Write([]byte("\x1b[?2004h"))
	// Overflow the ring so the set sequence is trimmed away.
	chunk := []byte(strings.Repeat("x", 64<<10) + "\r\n")
	for written := 0; written < ringCap+(1<<20); written += len(chunk) {
		v.Write(chunk)
	}

	if bytes.Contains(v.RingBytes(), []byte("\x1b[?2004h")) {
		t.Skip("ring did not overflow past the mode sequence; test is not exercising the case")
	}
	if !bytes.Contains(v.ReplayBytes(), []byte("\x1b[?2004h")) {
		t.Fatal("mode was trimmed out of the ring and never re-asserted")
	}
}

// Guard against the scanner mistaking ordinary output for a mode change.
func TestTrackerIgnoresNonModeSequences(t *testing.T) {
	v := NewVT(80, 24)
	v.Write([]byte(
		"\x1b[31mred\x1b[m" + // SGR
			"\x1b[10;20H" + // CUP
			"\x1b]0;a title\x07" + // OSC
			"\x1b[?25l" + // DECTCEM: snapshot owns this one
			"\x1b[?1049h" + // alt screen: snapshot owns this one too
			"\x1b[?2004$p", // a mode *query*, not a set
	))

	restore := v.decModes.restoreBytes()
	if len(restore) != 0 {
		t.Fatalf("tracker claimed modes from non-set sequences: %q", restore)
	}
}

func tail(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[len(b)-n:]
}
