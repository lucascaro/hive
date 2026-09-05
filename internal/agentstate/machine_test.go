package agentstate

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lucascaro/hive/internal/wire"
)

var t0 = time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

// hookEvent is the shorthand every table row below uses for "the agent
// reported something".
func hookEvent(kind string, at time.Time, text string) Event {
	return Event{Kind: kind, Source: wire.StateSourceHook, At: at, Text: text}
}

func TestNewIsIdleOnTheHeuristicTier(t *testing.T) {
	got := New(t0).Snapshot()
	want := Snapshot{}
	if got != want {
		t.Errorf("New = %+v, want the zero snapshot %+v — registry.Entry "+
			"relies on those being the same thing", got, want)
	}
}

// The heuristic tier is the whole of phase 1: it is what a plain shell
// and every agent without hooks gets.
func TestHeuristicTier(t *testing.T) {
	tests := []struct {
		name    string
		steps   func(m *Machine) bool
		want    string
		changed bool // what the last step returned
	}{
		{
			name:    "output starts working",
			steps:   func(m *Machine) bool { return m.Output(t0) },
			want:    wire.StateWorking,
			changed: true,
		},
		{
			name: "more output while working changes nothing",
			steps: func(m *Machine) bool {
				m.Output(t0)
				return m.Output(t0.Add(10 * time.Millisecond))
			},
			want:    wire.StateWorking,
			changed: false,
		},
		{
			name: "quiet goes idle",
			steps: func(m *Machine) bool {
				m.Output(t0)
				return m.Tick(t0.Add(QuietAfter))
			},
			want:    wire.StateIdle,
			changed: true,
		},
		{
			name: "not yet quiet stays working",
			steps: func(m *Machine) bool {
				m.Output(t0)
				return m.Tick(t0.Add(QuietAfter - time.Millisecond))
			},
			want:    wire.StateWorking,
			changed: false,
		},
		{
			name:    "bell while idle waits for input",
			steps:   func(m *Machine) bool { return m.Bell(t0) },
			want:    wire.StateWaitingInput,
			changed: true,
		},
		{
			name: "bell while working waits for input",
			steps: func(m *Machine) bool {
				m.Output(t0)
				return m.Bell(t0.Add(time.Second))
			},
			want:    wire.StateWaitingInput,
			changed: true,
		},
		{
			name: "a second bell is not news",
			steps: func(m *Machine) bool {
				m.Bell(t0)
				return m.Bell(t0.Add(time.Second))
			},
			want:    wire.StateWaitingInput,
			changed: false,
		},
		{
			// The reported regression: an agent rang the bell and then
			// carried on redrawing, and the redraw buried its own
			// request for attention before any client could paint it.
			// Only the user looking (ClearWaiting) ends a wait.
			name: "redrawing does not answer a request for the user",
			steps: func(m *Machine) bool {
				m.Bell(t0)
				return m.Output(t0.Add(time.Second))
			},
			want:    wire.StateWaitingInput,
			changed: false,
		},
		{
			name: "waiting does not time out into idle",
			steps: func(m *Machine) bool {
				m.Bell(t0)
				return m.Tick(t0.Add(time.Hour))
			},
			want:    wire.StateWaitingInput,
			changed: false,
		},
		{
			name:    "exit",
			steps:   func(m *Machine) bool { return m.Exit() },
			want:    wire.StateExited,
			changed: true,
		},
		{
			name: "a second exit is not news",
			steps: func(m *Machine) bool {
				m.Exit()
				return m.Exit()
			},
			want:    wire.StateExited,
			changed: false,
		},
		{
			name: "output after exit cannot resurrect the session",
			steps: func(m *Machine) bool {
				m.Exit()
				return m.Output(t0.Add(time.Second))
			},
			want:    wire.StateExited,
			changed: false,
		},
		{
			name: "clear waiting resolves the bell",
			steps: func(m *Machine) bool {
				m.Bell(t0)
				return m.ClearWaiting()
			},
			want:    wire.StateIdle,
			changed: true,
		},
		{
			name: "clear waiting on a working session is a no-op",
			steps: func(m *Machine) bool {
				m.Output(t0)
				return m.ClearWaiting()
			},
			want:    wire.StateWorking,
			changed: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := New(t0)
			changed := tc.steps(m)
			if got := m.Snapshot().State; got != tc.want {
				t.Errorf("state = %q, want %q", got, tc.want)
			}
			if changed != tc.changed {
				t.Errorf("last step reported changed=%v, want %v — the "+
					"registry broadcasts on exactly this", changed, tc.changed)
			}
			if src := m.Snapshot().Source; src != wire.StateSourceHeuristic {
				t.Errorf("source = %q, want the heuristic tier", src)
			}
		})
	}
}

func TestAgentTierOverridesTheHeuristicOne(t *testing.T) {
	m := New(t0)
	m.Output(t0) // working, heuristic

	if !m.Apply(hookEvent(KindWaitingPermission, t0.Add(time.Second), "")) {
		t.Fatal("a permission prompt must be a change")
	}
	if got := m.Snapshot(); got.State != wire.StateWaitingPermission || got.Source != wire.StateSourceHook {
		t.Fatalf("snapshot = %+v, want waiting_permission on the hook tier", got)
	}

	// The prompt repainting itself must not read as "working again".
	// This is the single most important rule in the file: without it
	// every permission prompt flickers back to working on its next
	// redraw and the user never sees it.
	if m.Output(t0.Add(2 * time.Second)) {
		t.Error("output moved a hook-owned session")
	}
	if got := m.Snapshot().State; got != wire.StateWaitingPermission {
		t.Errorf("state = %q, want it held at waiting_permission", got)
	}

	// A bell during a permission wait is absorbed: one wait, one alert.
	if m.Bell(t0.Add(3 * time.Second)) {
		t.Error("bell moved a session already waiting")
	}

	// And the quiet timer must not invent a turn end for a tier that
	// reports its own.
	if m.Tick(t0.Add(time.Hour)) {
		t.Error("tick moved a hook-owned session")
	}
}

func TestHookTierGoesStaleAndOutputTakesOver(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindPrompt, t0, "do the thing"))
	if got := m.Snapshot().Source; got != wire.StateSourceHook {
		t.Fatalf("source = %q, want hook", got)
	}

	// Still fresh: output changes nothing.
	if m.Output(t0.Add(HookStaleAfter)) {
		t.Error("output moved a session whose hook is still fresh")
	}
	// One instant past the window, with bytes still arriving: the hook
	// is gone (crashed, killed, uninstalled) and the heuristic tier
	// must take the session back rather than leave it pinned forever.
	if !m.Output(t0.Add(HookStaleAfter + time.Nanosecond)) {
		t.Fatal("a stale hook tier must be demoted by continuing output")
	}
	if got := m.Snapshot(); got.Source != wire.StateSourceHeuristic || got.State != wire.StateWorking {
		t.Errorf("snapshot = %+v, want working on the heuristic tier", got)
	}
}

func TestApplyMapsEveryKind(t *testing.T) {
	tests := []struct {
		kind string
		want string
	}{
		{KindPrompt, wire.StateWorking},
		{KindTurnEnd, wire.StateIdle},
		{KindWaitingInput, wire.StateWaitingInput},
		{KindWaitingPermission, wire.StateWaitingPermission},
		{KindPermissionResolved, wire.StateWorking},
		{KindError, wire.StateError},
		{KindSessionEnd, wire.StateExited},
	}
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			m := New(t0)
			m.Apply(hookEvent(tc.kind, t0, ""))
			if got := m.Snapshot().State; got != tc.want {
				t.Errorf("state = %q, want %q", got, tc.want)
			}
		})
	}
}

// A ping — and anything the agent invents that we have not heard of —
// must keep the tier alive without moving the state. That is what stops
// a renamed hook event in a future Claude release from silently
// dropping every session back to the heuristic tier.
func TestPingAndUnknownKindsHoldTheTierWithoutMovingState(t *testing.T) {
	for _, kind := range []string{KindPing, "some_future_event"} {
		t.Run(kind, func(t *testing.T) {
			m := New(t0)
			m.Apply(hookEvent(KindWaitingPermission, t0, ""))
			if m.Apply(hookEvent(kind, t0.Add(time.Second), "")) {
				t.Error("reported a change")
			}
			if got := m.Snapshot().State; got != wire.StateWaitingPermission {
				t.Errorf("state = %q, want it unmoved", got)
			}
			// The tier is still fresh a moment after the ping, which is
			// the point: the staleness clock was reset.
			if m.Output(t0.Add(HookStaleAfter)) {
				t.Error("the ping did not refresh the staleness clock")
			}
		})
	}
}

func TestLastPromptKeepsTheFirstQuestion(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindPrompt, t0, "port the parser"))
	m.Apply(hookEvent(KindTurnEnd, t0.Add(time.Minute), "done"))
	m.Apply(hookEvent(KindPrompt, t0.Add(2*time.Minute), "now add tests"))

	got := m.Snapshot()
	if got.LastPrompt != "port the parser" {
		t.Errorf("LastPrompt = %q; it must hold the ask the session was "+
			"opened for, which is what the user scans a list of ten for",
			got.LastPrompt)
	}
	if got.LastSummary != "done" {
		t.Errorf("LastSummary = %q, want the last turn's summary", got.LastSummary)
	}
}

func TestTextIsCappedAtTheWireLimit(t *testing.T) {
	m := New(t0)
	long := strings.Repeat("x", wire.MaxSummaryLen*3)
	m.Apply(hookEvent(KindPrompt, t0, long))
	m.Apply(hookEvent(KindError, t0, long))

	got := m.Snapshot()
	if len(got.LastPrompt) != wire.MaxSummaryLen {
		t.Errorf("LastPrompt is %d bytes, want it capped at %d",
			len(got.LastPrompt), wire.MaxSummaryLen)
	}
	if len(got.LastSummary) != wire.MaxSummaryLen {
		t.Errorf("LastSummary is %d bytes, want it capped at %d",
			len(got.LastSummary), wire.MaxSummaryLen)
	}
}

// Capping cuts on a byte boundary, so it must not leave a partial
// rune behind — the field is JSON-encoded and rebroadcast, and Title
// (registry.truncateTitle) has always dropped the tail for this
// reason. The multi-byte string is sized so the cut lands mid-rune.
func TestCappedTextStaysValidUTF8(t *testing.T) {
	m := New(t0)
	// 3 bytes per rune: MaxSummaryLen is not a multiple of 3, so
	// slicing at it splits the rune that straddles the boundary.
	m.Apply(hookEvent(KindPrompt, t0, strings.Repeat("é", wire.MaxSummaryLen)))
	if got := m.Snapshot().LastPrompt; !utf8.ValidString(got) {
		t.Errorf("LastPrompt is not valid UTF-8 after capping (%d bytes)", len(got))
	}
}

// Exit is terminal on every feeder. Output and Bell already guard it;
// Apply must too, or a hook process still in flight when the PTY dies
// revives a dead session — and a revived waiting_input can never be
// cleared, because ClearWaiting needs a user to type into a gone PTY.
func TestApplyCannotResurrectAnExitedSession(t *testing.T) {
	for _, kind := range []string{KindPrompt, KindWaitingInput, KindWaitingPermission, KindTurnEnd} {
		m := New(t0)
		m.Apply(hookEvent(KindPrompt, t0, "do the thing"))
		m.Exit()
		if m.Apply(hookEvent(kind, t0.Add(time.Second), "late")) {
			t.Errorf("%s after Exit reported a change", kind)
		}
		if got := m.Snapshot().State; got != wire.StateExited {
			t.Errorf("%s after Exit left state %q, want %q", kind, got, wire.StateExited)
		}
	}
}

// The exit code is deliberately not consulted: a shell exiting 1 is
// exited, not error, or a red dot stops meaning anything.
func TestNonZeroExitIsExitedNotError(t *testing.T) {
	m := New(t0)
	m.Output(t0)
	m.Exit()
	if got := m.Snapshot().State; got != wire.StateExited {
		t.Errorf("state = %q, want %q", got, wire.StateExited)
	}
}

// A hooked agent rings when its turn finishes; Stop maps to idle, so
// the bell is the only "come look" that moment produces. It must count
// on the hook tier, without demoting the tier, and a keystroke must
// clear it even though the tier is not heuristic.
func TestBellCountsOnTheHookTier(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindPrompt, t0, "do the thing"))
	m.Apply(hookEvent(KindTurnEnd, t0.Add(time.Second), "done"))
	if !m.Bell(t0.Add(time.Second)) {
		t.Fatal("bell ignored on the hook tier")
	}
	got := m.Snapshot()
	if got.State != wire.StateWaitingInput || got.Source != wire.StateSourceHook {
		t.Fatalf("snapshot = %+v, want waiting_input still on the hook tier", got)
	}
	if !m.ClearWaiting() {
		t.Fatal("keystroke did not clear a hook-tier bell wait")
	}
	if got := m.Snapshot().State; got != wire.StateIdle {
		t.Fatalf("state = %q, want idle", got)
	}
	// A keystroke into a permission dialog is the answer, too.
	m.Apply(hookEvent(KindWaitingPermission, t0.Add(2*time.Second), ""))
	if !m.ClearWaiting() {
		t.Fatal("keystroke did not clear a permission wait")
	}
	if got := m.Snapshot().State; got != wire.StateWorking {
		t.Fatalf("state after answering a permission = %q, want working (the tool is running)", got)
	}
}

// TestApplyDropsOutOfOrderEvents pins the ordering guard. Each report
// is its own connection served on its own goroutine, so a pair sent
// milliseconds apart can arrive inverted; without the guard the older
// event wins and leaves a glyph nothing corrects until HookStaleAfter.
func TestApplyDropsOutOfOrderEvents(t *testing.T) {
	m := New(time.Now())
	t0 := time.Now()

	if !m.Apply(Event{Kind: KindPrompt, Source: wire.StateSourceHook, At: t0, Text: "do a thing"}) {
		t.Fatal("prompt should have changed state")
	}
	if got := m.Snapshot().State; got != wire.StateWorking {
		t.Fatalf("state = %q, want %q", got, wire.StateWorking)
	}

	// The turn ends a second later.
	if !m.Apply(Event{Kind: KindTurnEnd, Source: wire.StateSourceHook, At: t0.Add(time.Second), Text: "done"}) {
		t.Fatal("turn_end should have changed state")
	}
	if got := m.Snapshot().State; got != wire.StateIdle {
		t.Fatalf("state = %q, want %q", got, wire.StateIdle)
	}

	// A permission_resolved stamped BEFORE the turn end lands late.
	// Applying it would report "working" for a turn that is over.
	before := m.Snapshot()
	if m.Apply(Event{Kind: KindPermissionResolved, Source: wire.StateSourceHook, At: t0.Add(500 * time.Millisecond)}) {
		t.Error("a stale event reported a change")
	}
	if got := m.Snapshot(); got != before {
		t.Errorf("stale event mutated the machine: %+v -> %+v", before, got)
	}

	// The clock is not rewound either: a fresh event at the real "now"
	// still applies, and staleness is still measured from the newest
	// event seen, not the stale one.
	if !m.Apply(Event{Kind: KindPrompt, Source: wire.StateSourceHook, At: t0.Add(2 * time.Second), Text: "next"}) {
		t.Error("a newer event after a stale one should still apply")
	}
	if got := m.Snapshot().State; got != wire.StateWorking {
		t.Errorf("state = %q, want %q", got, wire.StateWorking)
	}
}

// TestApplyAcceptsEqualTimestamps guards the boundary: two events in
// the same nanosecond are indistinguishable, so the guard must not
// silently drop the second one.
func TestApplyAcceptsEqualTimestamps(t *testing.T) {
	m := New(time.Now())
	at := time.Now()
	m.Apply(Event{Kind: KindPrompt, Source: wire.StateSourceHook, At: at})
	if !m.Apply(Event{Kind: KindTurnEnd, Source: wire.StateSourceHook, At: at, Text: "done"}) {
		t.Fatal("an event with an equal timestamp was dropped")
	}
	if got := m.Snapshot().State; got != wire.StateIdle {
		t.Errorf("state = %q, want %q", got, wire.StateIdle)
	}
}

// TestApplyFirstEventNeverDropped guards the zero-value case: the guard
// keys off hookSeenAt, which is zero until the first agent event, and
// must not compare against it.
func TestApplyFirstEventNeverDropped(t *testing.T) {
	m := New(time.Now())
	// An At well in the past — a reporter that stamped early, or a
	// replayed fixture — must still promote the session off heuristics.
	if !m.Apply(Event{Kind: KindPrompt, Source: wire.StateSourceHook, At: time.Now().Add(-time.Hour)}) {
		t.Fatal("the first agent event was dropped")
	}
	if got := m.Snapshot().Source; got != wire.StateSourceHook {
		t.Errorf("source = %q, want %q", got, wire.StateSourceHook)
	}
}
