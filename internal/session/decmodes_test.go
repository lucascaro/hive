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

func TestSnapshotResetsDisabledModes(t *testing.T) {
	v := NewVT(80, 24)
	v.Write([]byte("\x1b[?2004h\x1b[?1002h"))
	v.Write([]byte("\x1b[?2004l"))

	snap := v.RenderSnapshot()
	if bytes.Contains(snap, []byte("\x1b[?2004h")) {
		t.Error("snapshot re-enables a mode the program turned off")
	}
	if !bytes.Contains(snap, []byte("\x1b[?2004l")) {
		t.Error("snapshot must state the off mode explicitly, not stay silent")
	}
	if !bytes.Contains(snap, []byte("\x1b[?1002h")) {
		t.Error("snapshot dropped a mode that is still on")
	}
}

// The defect the ever-seen gate closes, and the one an "omits disabled
// modes" assertion written only against 2004 could never catch.
//
// The ordinary reattach path does NOT term.reset() the tile — it reuses
// the live xterm and leans on the snapshot's DECSTR preamble. But
// xterm's softReset() restores coreService.decPrivateModes only; it
// never calls into CoreMouseService. So for 2004 silence happens to be
// correct, and for 1002/1006 it is a bug: a TUI that enables mouse
// tracking, then exits and sends the reset while the sink is
// unregistered, leaves the reattached tile still reporting clicks —
// \x1b[<0;12;5M typed into a plain shell.
func TestSnapshotResetsDisabledGroupedModes(t *testing.T) {
	v := NewVT(80, 24)
	// A TUI's opening, then its exit sequence.
	v.Write([]byte("\x1b[?1002h\x1b[?1006h"))
	v.Write([]byte("\x1b[?1006l\x1b[?1002l"))

	snap := v.RenderSnapshot()
	for _, want := range []string{"\x1b[?1002l", "\x1b[?1006l"} {
		if !bytes.Contains(snap, []byte(want)) {
			t.Errorf("snapshot must turn %q off explicitly; DECSTR does not "+
				"reach xterm's CoreMouseService.\nsnapshot: %q", want, snap)
		}
	}
}

// Never-seen modes stay silent. This is what keeps a session that used
// no sticky mode replaying byte-identical to the ring (and emitting no
// CAN either) — see TestReplayWithNoModesIsJustTheRing.
func TestRestoreIsSilentForUntouchedModes(t *testing.T) {
	v := NewVT(80, 24)
	v.Write([]byte("\x1b[?2004h\x1b[?2004l"))

	got := v.decModes.restoreBytes()
	if want := "\x1b[?2004l"; string(got) != want {
		t.Fatalf("only the touched mode should speak: got %q, want %q", got, want)
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

// appendRing only normalises the FRONT of the ring to a safe replay
// boundary. The tail is wherever the last PTY read stopped, so the ring
// routinely ends mid-sequence — and a mode restore concatenated straight
// onto that is consumed as the unterminated sequence's payload, which is
// the restore silently doing nothing. CAN (0x18) aborts whatever the cut
// left open, in every parser state, and is inert in plain text.
func TestReplayAbortsAnUnterminatedRingTail(t *testing.T) {
	for _, tc := range []struct{ name, tail string }{
		{"mid-CSI", "\x1b[3"},
		{"mid-CSI-private", "\x1b[?10"},
		{"mid-OSC", "\x1b]0;a par"},
		{"mid-DCS", "\x1bP1;2|payloa"},
		{"clean text", "all done"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := NewVT(80, 24)
			v.Write([]byte("\x1b[?2004h"))
			v.Write([]byte(tc.tail))

			want := "\x18\x1b[?2004h"
			if got := v.ReplayBytes(); !bytes.HasSuffix(got, []byte(want)) {
				t.Fatalf("replay must end with CAN + the mode restore so a cut\n"+
					"sequence cannot swallow it; got tail %q", tail(got, 24))
			}
		})
	}
}

// No modes set means nothing to protect, so no CAN either — a bare
// re-replay of the ring must stay byte-identical to the ring.
func TestReplayWithNoModesIsJustTheRing(t *testing.T) {
	v := NewVT(80, 24)
	v.Write([]byte("plain output\r\n"))

	if got, want := v.ReplayBytes(), v.ringBytes(); !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
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

	if bytes.Contains(v.ringBytes(), []byte("\x1b[?2004h")) {
		t.Fatal("ring did not overflow past the mode sequence; this test is no " +
			"longer exercising the case it exists for — check ringCap and the write loop")
	}
	if !bytes.Contains(v.ReplayBytes(), []byte("\x1b[?2004h")) {
		t.Fatal("mode was trimmed out of the ring and never re-asserted")
	}
}

// 9/1000/1002/1003 share one slot in the receiving terminal
// (xterm.js: coreMouseService.activeProtocol), so setting one supersedes
// whichever was on. A program that downgrades 1003 -> 1002 without
// sending \x1b[?1003l must not have 1003 replayed at it, in any order.
func TestRestoreKeepsOnlyTheLastModeOfAnExclusiveGroup(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"protocol downgrade", "\x1b[?1003h\x1b[?1002h", "\x1b[?1002h"},
		{"protocol re-set", "\x1b[?1002h\x1b[?1003h\x1b[?1002h", "\x1b[?1002h"},
		{"x10 supersedes", "\x1b[?1000h\x1b[?9h", "\x1b[?9h"},
		{"encoding group", "\x1b[?1015h\x1b[?1006h", "\x1b[?1006h"},
		{"groups are independent", "\x1b[?1002h\x1b[?1006h", "\x1b[?1002h\x1b[?1006h"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := NewVT(80, 24)
			v.Write([]byte(tc.in))

			if got := v.decModes.restoreBytes(); !bytes.Equal(got, []byte(tc.want)) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Resetting ANY member of an exclusive group empties the slot — xterm.js
// maps 9/1000/1002/1003 in DECRST to activeProtocol="NONE" flat, with no
// memory of an earlier member. Uncovering the previous occupant would
// turn mouse reporting back on for a program that switched it off, and
// the client would then feed it mouse escapes as keyboard input.
// The group is ONE slot, so a reset of any member leaves the group with
// one thing to say and it is `l` — never an `h` for some earlier member
// uncovered by the reset, and never silence (see
// TestSnapshotResetsDisabledGroupedModes for why silence is wrong).
func TestRestoreDropsTheWholeGroupOnReset(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"reset the current member", "\x1b[?1000h\x1b[?1002h\x1b[?1002l", "\x1b[?1002l"},
		{"reset a superseded member", "\x1b[?1000h\x1b[?1002h\x1b[?1000l", "\x1b[?1000l"},
		{"encoding group", "\x1b[?1015h\x1b[?1006h\x1b[?1006l", "\x1b[?1006l"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := NewVT(80, 24)
			v.Write([]byte(tc.in))

			if got := v.decModes.restoreBytes(); !bytes.Equal(got, []byte(tc.want)) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// The mirror image of the bug this file exists for: when the *program*
// resets the terminal, the modes are gone in the receiving terminal too,
// and re-asserting them would push bracketed paste onto a plain shell
// that never enabled it — pastes then arrive wrapped in literal \x1b[200~.
//
// The two resets are NOT the same amount, and the tracker has to mirror
// the real client rather than pick the tidier rule. In xterm.js 5.5.0
// softReset() resets coreService.decPrivateModes only (1, 1004, 2004)
// and leaves CoreMouseService alone; fullReset() goes through
// CoreTerminal.reset(), which resets the mouse service too.
func TestTrackerClearsUngroupedModesOnDECSTR(t *testing.T) {
	v := NewVT(80, 24)
	v.Write([]byte("\x1b[?1h\x1b[?1004h\x1b[?2004h\x1b[?1002h\x1b[?1006h"))
	v.Write([]byte("\x1b[!p"))

	got := v.decModes.restoreBytes()
	for _, off := range []string{"\x1b[?1l", "\x1b[?1004l", "\x1b[?2004l"} {
		if !bytes.Contains(got, []byte(off)) {
			t.Errorf("DECSTR should have cleared %q; restore is %q", off, got)
		}
	}
	// The mouse slots survive a DECSTR in the real client. Forgetting
	// them here would leave a reattach unable to re-assert mouse
	// reporting the program still has on.
	for _, kept := range []string{"\x1b[?1002h", "\x1b[?1006h"} {
		if !bytes.Contains(got, []byte(kept)) {
			t.Errorf("DECSTR must not clear %q; restore is %q", kept, got)
		}
	}
}

func TestTrackerClearsEverythingOnRIS(t *testing.T) {
	v := NewVT(80, 24)
	v.Write([]byte("\x1b[?1h\x1b[?1004h\x1b[?2004h\x1b[?1002h\x1b[?1006h"))
	v.Write([]byte("\x1bc"))

	// RIS clears every enabled flag, but the modes stay SEEN: the client
	// we reattach is not the one the program RIS'd, so the snapshot still
	// has to say "off" out loud.
	got := v.decModes.restoreBytes()
	if bytes.ContainsRune(got, 'h') {
		t.Fatalf("a mode survived a RIS from the program: %q", got)
	}
	for _, off := range []string{"\x1b[?1l", "\x1b[?1004l", "\x1b[?2004l", "\x1b[?1002l", "\x1b[?1006l"} {
		if !bytes.Contains(got, []byte(off)) {
			t.Errorf("RIS restore is missing %q; got %q", off, got)
		}
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
