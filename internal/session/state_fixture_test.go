package session

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agentstate"
	"github.com/lucascaro/hive/internal/wire"
)

// This file replays recorded, real-`claude` PTY output through a VT and
// an agentstate.Machine, at recorded offsets in VIRTUAL time (no
// sleeping — see replayFixture), to table-test the heuristic tier
// against ground truth instead of guesswork. See
// testdata/state/README.md for how the fixtures were captured and how
// to re-record them.
//
// The driving loop mirrors registry.sampleStateLocked exactly: on each
// 500ms virtual tick, feed in whatever fixture bytes have "arrived" by
// then, call Machine.Output only if the screen digest changed, then
// always call Machine.Tick.

// fixtureRecord is one (offset-since-start, chunk) pair, as written by
// the recorder tool (ms uint32 BE, len uint32 BE, bytes).
type fixtureRecord struct {
	offsetMs uint32
	data     []byte
}

// loadFixture parses a recorder .bin file.
func loadFixture(t *testing.T, name string) []fixtureRecord {
	t.Helper()
	path := filepath.Join("testdata", "state", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var recs []fixtureRecord
	for i := 0; i < len(b); {
		if i+8 > len(b) {
			t.Fatalf("%s: truncated record header at byte %d", name, i)
		}
		off := binary.BigEndian.Uint32(b[i : i+4])
		n := binary.BigEndian.Uint32(b[i+4 : i+8])
		i += 8
		if i+int(n) > len(b) {
			t.Fatalf("%s: truncated record body at byte %d", name, i)
		}
		recs = append(recs, fixtureRecord{offsetMs: off, data: b[i : i+int(n)]})
		i += int(n)
	}
	if len(recs) == 0 {
		t.Fatalf("%s: no records parsed", name)
	}
	return recs
}

// tickState is one sampled point on the replayed timeline.
type tickState struct {
	ms    int64
	state agentstate.State
}

// replayFixture feeds recs into a fresh 80x24 VT and agentstate.Machine
// at their recorded offsets in virtual time, sampling every tickEvery
// up to totalMs. This is registry.sampleStateLocked's loop, transplanted
// onto a fake clock instead of a real ticker + wall clock so the whole
// replay is instant and deterministic: no goroutine, no sleeping, no
// real PTY.
func replayFixture(recs []fixtureRecord, tickEvery time.Duration, totalMs int64) []tickState {
	epoch := time.Unix(0, 0)
	vt := NewVT(80, 24)
	m := agentstate.New(epoch)

	lastDigest := vt.ScreenDigest()
	ri := 0
	var timeline []tickState
	step := tickEvery.Milliseconds()
	for elapsed := int64(0); elapsed <= totalMs; elapsed += step {
		now := epoch.Add(time.Duration(elapsed) * time.Millisecond)
		for ri < len(recs) && int64(recs[ri].offsetMs) <= elapsed {
			_, _ = vt.Write(recs[ri].data)
			ri++
		}
		if d := vt.ScreenDigest(); d != lastDigest {
			lastDigest = d
			m.Output(now)
		}
		m.Tick(now)
		timeline = append(timeline, tickState{ms: elapsed, state: m.Snapshot().State})
	}
	return timeline
}

// stateAt returns the state sampled at the tick nearest ms (ticks are
// tickEvery apart starting at 0), so callers can assert against
// timestamps read off the recording without hand-computing tick
// indices.
func stateAt(t *testing.T, tl []tickState, tickEvery time.Duration, ms int64) agentstate.State {
	t.Helper()
	idx := ms / tickEvery.Milliseconds()
	if idx < 0 {
		idx = 0
	}
	if idx >= int64(len(tl)) {
		t.Fatalf("stateAt(%dms): timeline only covers %d ticks (%dms)", ms, len(tl), tl[len(tl)-1].ms)
	}
	return tl[idx].state
}

// assertStateRange asserts every tick in [fromMs, toMs] has the given
// state, reporting every mismatch (not just the first) so a failure
// shows the whole shape of the drift.
func assertStateRange(t *testing.T, tl []tickState, tickEvery time.Duration, fromMs, toMs int64, want agentstate.State) {
	t.Helper()
	step := tickEvery.Milliseconds()
	bad := 0
	for ms := fromMs; ms <= toMs; ms += step {
		got := stateAt(t, tl, tickEvery, ms)
		if got != want {
			bad++
			t.Errorf("t=%dms: state = %q, want %q", ms, got, want)
		}
	}
	if bad > 0 {
		t.Fatalf("%d/%d ticks in [%d,%d]ms wrong (see above)", bad, (toMs-fromMs)/step+1, fromMs, toMs)
	}
}

// bytesInWindow sums the length of every fixture record whose offset
// falls in [fromMs, toMs]. Used to prove a window actually carries
// recorded bytes rather than the test silently passing on an empty
// fixture.
func bytesInWindow(recs []fixtureRecord, fromMs, toMs uint32) int {
	n := 0
	for _, r := range recs {
		if r.offsetMs >= fromMs && r.offsetMs <= toMs {
			n += len(r.data)
		}
	}
	return n
}

const tick = 500 * time.Millisecond

// TestFixtureIdlePrompt is the regression guard the whole fixture
// harness exists for: a real `claude` at an untouched prompt must read
// as idle, and STAY idle, for as long as the recording's idle window
// lasts.
//
// Surprise, recorded here rather than papered over: this recording's
// idle window (~2.65s to ~10.5s, see testdata/state/README.md) contains
// ZERO bytes on the wire — no ESC[?6n, nothing. That was first suspected
// to be an artifact of the recorder never answering claude's
// device-status queries (ESC[6n / ESC[?6n cursor-position report, ESC c
// primary device attributes) — a real interactive terminal answers
// those, this recorder's PTY didn't. The recorder was fixed to answer
// both (see the scratch recorder's main.go), and the fixture was
// re-recorded: claude still sends ZERO ESC[6n / ESC[?6n queries
// anywhere in the recording, idle or not. It does send ESC[c three
// times, but only during the trust-dialog/startup dance (≤2.27s),
// never again once the screen settles. So the "polls every 200ms while
// idle" behaviour VT.ScreenDigest's doc comment describes did not
// reproduce here even with the query answered — this recorded
// scenario (claude 2.1.261, this PTY lib, no real interactive
// terminal) just doesn't trigger it. Per the task's own rule, that
// discrepancy is reported, not silently fixed into the test or into
// vt.go/machine.go — see the report for this task.
//
// Because of that, "prove the byte stream is non-empty" is asserted
// against the whole fixture (startup paint + the /exit teardown) rather
// than the idle window itself, which is the one honest way to keep that
// guard meaningful given what was actually recorded.
func TestFixtureIdlePrompt(t *testing.T) {
	recs := loadFixture(t, "claude-idle-prompt.bin")

	total := 0
	for _, r := range recs {
		total += len(r.data)
	}
	if total < 1000 {
		t.Fatalf("fixture suspiciously small (%d bytes); recording likely broken", total)
	}
	if got := bytesInWindow(recs, 2800, 10450); got != 0 {
		t.Errorf("idle window carried %d bytes; the ESC[?6n-while-idle case this test was written to guard actually reproduced — update this test's expectations, don't just note it", got)
	}

	tl := replayFixture(recs, tick, 11000)

	// Startup paint: the TUI drawing the trust-banner-free chat screen
	// is real screen change, so it must read as working.
	assertStateRange(t, tl, tick, 1000, 2000, wire.StateWorking)

	// Settled: last content-changing write lands at ~2.65s (see
	// README.md); QuietAfter is 2s, so by 5s the heuristic tier must
	// have called it idle — and, this being the regression guard, it
	// must STAY idle for the whole rest of the recorded idle window
	// (through 10s; /exit is sent at 10.5s).
	assertStateRange(t, tl, tick, 5000, 10000, wire.StateIdle)
}

// TestFixtureTyping walks idle -> working (while the user types) ->
// idle, driven by a fixture of a real claude echoing keystrokes typed
// at 200ms/char.
func TestFixtureTyping(t *testing.T) {
	recs := loadFixture(t, "claude-typing.bin")
	tl := replayFixture(recs, tick, 14000)

	// Idle before typing starts (paint settles ~2.7s, typing starts 6.5s).
	assertStateRange(t, tl, tick, 5000, 6000, wire.StateIdle)

	// Working while characters are echoed back (6.5s-7.3s).
	if got := bytesInWindow(recs, 6400, 7400); got == 0 {
		t.Fatal("typing window carried no bytes; fixture or offsets are wrong")
	}
	assertStateRange(t, tl, tick, 7000, 8500, wire.StateWorking)

	// Idle again once QuietAfter has elapsed since the last keystroke
	// (last char at ~7.3s + 2s = 9.3s), before /exit at ~10.3s.
	assertStateRange(t, tl, tick, 9500, 10000, wire.StateIdle)
}

// TestFixtureStreaming walks idle -> working (while a real reply
// streams in) -> idle, driven by a fixture of one live API turn ("reply
// with exactly the word pong and nothing else").
func TestFixtureStreaming(t *testing.T) {
	recs := loadFixture(t, "claude-streaming.bin")
	tl := replayFixture(recs, tick, 19000)

	// Idle before the prompt is sent (paint settles ~2.8s, prompt at 6.5s).
	assertStateRange(t, tl, tick, 5000, 6500, wire.StateIdle)

	// Working throughout the stream (prompt to the last content change
	// at ~16.4s; see README.md for the full offset list).
	if got := bytesInWindow(recs, 7000, 16000); got == 0 {
		t.Fatal("streaming window carried no bytes; fixture or offsets are wrong")
	}
	assertStateRange(t, tl, tick, 7500, 16000, wire.StateWorking)

	// Idle once the reply has settled and QuietAfter has elapsed
	// (16.4s + 2s = 18.4s), just before /exit at 18.5s.
	assertStateRange(t, tl, tick, 18500, 18500, wire.StateIdle)
}

// TestFixtureRecordsAreWellFormed is a cheap sanity check on the parser
// itself, independent of the state machine: every fixture must parse to
// at least one record and the offsets must be non-decreasing, exactly
// as the recorder emits them.
func TestFixtureRecordsAreWellFormed(t *testing.T) {
	for _, name := range []string{"claude-idle-prompt.bin", "claude-typing.bin", "claude-streaming.bin"} {
		t.Run(name, func(t *testing.T) {
			recs := loadFixture(t, name)
			var prev uint32
			for i, r := range recs {
				if r.offsetMs < prev {
					t.Errorf("record %d: offset %d < previous %d", i, r.offsetMs, prev)
				}
				prev = r.offsetMs
				if len(r.data) == 0 {
					t.Errorf("record %d: empty chunk", i)
				}
			}
		})
	}
}

// TestFixturePermissionSkipped documents, rather than silently drops,
// the one fixture the task asked for that could not be recorded: `ls`
// via the Bash tool never produced a permission prompt against a real
// claude 2.1.261, even with --permission-mode default. Claude Code
// evidently pre-approves at least some read-only shell commands, so
// there was nothing to capture — no waiting_permission window ever
// appeared on the wire. Recorded here so the omission shows up in `go
// test -v` output rather than only in a task report.
//
// The heuristic tier cannot distinguish "waiting on a permission
// prompt" from any other unchanging screen — reading waiting_permission
// off raw bytes would require parsing the TUI's own dialog chrome, which
// is exactly the kind of guesswork this whole fixture suite exists to
// replace with ground truth. That gap is filled by the hook tier
// (agentstate.KindWaitingPermission), not the heuristic one; see
// docs/exec-plans/completed/336-session-state-model.md's frozen transition
// table.
func TestFixturePermissionSkipped(t *testing.T) {
	t.Skip("no permission prompt appeared for `ls` via the Bash tool " +
		"(tried default and --permission-mode default); see this test's " +
		"doc comment and the task report")
}
