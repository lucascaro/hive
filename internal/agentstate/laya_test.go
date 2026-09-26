package agentstate

import (
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// The Laya tier (spec 458): a classification of the visible screen that
// fills in only where no agent tier is speaking.

func TestClassifyAppliesOnHeuristicTier(t *testing.T) {
	m := New(t0)
	if !m.Classify(wire.StateWaitingInput, t0.Add(time.Second)) {
		t.Fatal("Classify on a heuristic session changed nothing")
	}
	if got := m.Snapshot(); got.State != wire.StateWaitingInput || got.Source != wire.StateSourceLaya {
		t.Errorf("snapshot = %+v, want waiting_input from laya", got)
	}
}

func TestClassifyIgnoredWhileHookTrusted(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindPrompt, t0, "go"))
	if m.Classify(wire.StateIdle, t0.Add(10*time.Second)) {
		t.Fatal("Classify overrode a hook that reported 10s ago")
	}
	if got := m.Snapshot(); got.State != wire.StateWorking || got.Source != wire.StateSourceHook {
		t.Errorf("snapshot = %+v, want working from hook", got)
	}
}

func TestClassifyOverridesStaleHook(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindPrompt, t0, "go"))
	at := t0.Add(HookStaleAfter + time.Second)
	if !m.Classify(wire.StateWaitingInput, at) {
		t.Fatal("Classify did not override a hook silent past HookStaleAfter")
	}
	if got := m.Snapshot(); got.State != wire.StateWaitingInput || got.Source != wire.StateSourceLaya {
		t.Errorf("snapshot = %+v, want waiting_input from laya", got)
	}
}

// A Laya write must not look like the agent speaking: the tier stays as
// stale as it was, and the agent's next event wins outright.
func TestClassifyDoesNotRefreshHookClock(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindPrompt, t0, "go"))
	staleBefore, _ := m.StaleAt()
	hookSeen, order, reported, event := m.hookSeenAt, m.orderAt, m.reportedAt, m.lastEventAt

	m.Classify(wire.StateIdle, t0.Add(HookStaleAfter+time.Second))
	if m.hookSeenAt != hookSeen || m.orderAt != order || m.reportedAt != reported || m.lastEventAt != event {
		t.Error("Classify moved a tier clock")
	}
	if _, ok := m.StaleAt(); ok {
		t.Error("StaleAt set on a laya-sourced state")
	}
	_ = staleBefore

	next := t0.Add(HookStaleAfter + 2*time.Second)
	m.Apply(hookEvent(KindTurnEnd, next, "done"))
	if got := m.Snapshot(); got.State != wire.StateWaitingInput || got.Source != wire.StateSourceHook {
		t.Errorf("snapshot = %+v, want the hook's waiting_input back", got)
	}
}

func TestClassifyRespectsWantsUser(t *testing.T) {
	for _, s := range []State{wire.StateWaitingInput, wire.StateWaitingPermission, wire.StateError} {
		m := New(t0)
		m.state = s
		if m.Classify(wire.StateWorking, t0.Add(time.Minute)) {
			t.Errorf("Classify moved %q — only the user answers a wait", s)
		}
	}
}

func TestClassifyRejectsExitedAndUnknown(t *testing.T) {
	m := New(t0)
	for _, s := range []State{wire.StateExited, "starting", "bogus"} {
		if m.Classify(s, t0) {
			t.Errorf("Classify accepted %q", s)
		}
	}
	m.Exit()
	if m.Classify(wire.StateIdle, t0) {
		t.Error("Classify resurrected an exited session")
	}
}

func TestClassifySameLabelIsNoChange(t *testing.T) {
	m := New(t0)
	m.Classify(wire.StateWorking, t0)
	if m.Classify(wire.StateWorking, t0.Add(time.Second)) {
		t.Error("re-classifying the same label reported a change")
	}
}

// Timing a Laya working out would flip it idle until the next
// classification put it back; LayaRecheckAfter bounds it instead.
func TestTickLeavesLayaStateAlone(t *testing.T) {
	m := New(t0)
	m.Classify(wire.StateWorking, t0)
	if m.Tick(t0.Add(5 * QuietAfter)) {
		t.Fatal("Tick timed out a laya working")
	}
	if got := m.Snapshot(); got.State != wire.StateWorking || got.Source != wire.StateSourceLaya {
		t.Errorf("snapshot = %+v, want laya working", got)
	}
}

func TestOutputDemotesLayaToHeuristic(t *testing.T) {
	m := New(t0)
	m.Classify(wire.StateIdle, t0)
	if !m.Output(t0.Add(time.Second)) {
		t.Fatal("output on a laya idle changed nothing")
	}
	if got := m.Snapshot(); got.State != wire.StateWorking || got.Source != wire.StateSourceHeuristic {
		t.Errorf("snapshot = %+v, want heuristic working", got)
	}
}

// piStuckWorking returns a Pi session whose last real event was a
// tool_start at t0 and whose heartbeat has kept replaying it every 5s
// until the returned time — the "Pi says working, screen says waiting"
// case the spec exists for.
func piStuckWorking(t *testing.T) (*Machine, Event, time.Time) {
	t.Helper()
	m := New(t0)
	ev := piKeyed(KindToolStart, "A", 1, t0, t0)
	deliver(m, ev)
	var now time.Time
	for s := 5 * time.Second; s <= HookStaleAfter+5*time.Second; s += 5 * time.Second {
		now = t0.Add(s)
		hb := ev
		hb.Now = now
		deliver(m, hb)
	}
	return m, ev, now
}

func TestClassifiableWithHeartbeatButNoEvents(t *testing.T) {
	m, _, now := piStuckWorking(t)
	if !m.trusted(now) {
		t.Fatal("precondition: the heartbeat should keep the tier trusted")
	}
	if !m.Classifiable(now) {
		t.Error("a Pi that has only heartbeated for HookStaleAfter is not classifiable")
	}
}

func TestClassifiableFalseWhileEventsFresh(t *testing.T) {
	m := New(t0)
	deliver(m, piKeyed(KindToolStart, "A", 1, t0, t0))
	deliver(m, piKeyed(KindToolEnd, "A", 2, t0.Add(20*time.Second), t0.Add(20*time.Second)))
	if m.Classifiable(t0.Add(HookStaleAfter + time.Second)) {
		t.Error("classifiable 11s after a real event")
	}
}

func TestPingDoesNotAdvanceLastEventAt(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindPrompt, t0, "go"))
	late := t0.Add(HookStaleAfter)
	m.Apply(Event{Kind: KindPing, Source: wire.StateSourceHook, At: late, Now: late})
	if !m.lastEventAt.Equal(t0) {
		t.Errorf("lastEventAt = %v after a ping, want %v", m.lastEventAt, t0)
	}
	if !m.Classifiable(late.Add(time.Second + time.Millisecond)) {
		t.Error("a ping kept the tier from going event-stale")
	}
}

func TestReplayDoesNotRestoreOverLaya(t *testing.T) {
	m, ev, now := piStuckWorking(t)
	if !m.Classify(wire.StateWaitingInput, now) {
		t.Fatal("Classify refused an event-stale Pi")
	}
	hb := ev
	hb.Now = now.Add(5 * time.Second)
	if handled, changed := m.Replay(hb); !handled || changed {
		t.Fatalf("Replay = (%v, %v), want (true, false)", handled, changed)
	}
	if got := m.Snapshot(); got.State != wire.StateWaitingInput || got.Source != wire.StateSourceLaya {
		t.Errorf("snapshot = %+v, want laya waiting_input to survive the heartbeat", got)
	}
}

// Output on a heartbeat-trusted laya session must not hand it to the
// heuristic tier, or the next Replay restores the stale report Laya
// corrected (Replay restores anything that is not the extension tier).
func TestOutputThenHeartbeatDoesNotRestoreStaleExtState(t *testing.T) {
	m, ev, now := piStuckWorking(t)
	m.Classify(wire.StateIdle, now)
	m.Output(now.Add(time.Second))
	hb := ev
	hb.Now = now.Add(2 * time.Second)
	deliver(m, hb)
	if got := m.Snapshot(); got.Source != wire.StateSourceLaya || got.State != wire.StateIdle {
		t.Errorf("snapshot = %+v, want laya idle — not the replayed working", got)
	}
}

func TestApplyAfterLayaRestoresExtension(t *testing.T) {
	m, _, now := piStuckWorking(t)
	m.Classify(wire.StateIdle, now)
	deliver(m, piKeyed(KindPrompt, "A", 2, now.Add(time.Second), now.Add(time.Second)))
	if got := m.Snapshot(); got.State != wire.StateWorking || got.Source != wire.StateSourceExtension {
		t.Errorf("snapshot = %+v, want the extension's working back", got)
	}
}

func TestStaleAtFalseForLaya(t *testing.T) {
	m, _, now := piStuckWorking(t)
	if _, ok := m.StaleAt(); !ok {
		t.Fatal("precondition: extension tier has a StaleAt")
	}
	m.Classify(wire.StateIdle, now)
	if _, ok := m.StaleAt(); ok {
		t.Error("StaleAt set on a laya-sourced state")
	}
}

// A Laya wait is a guess about one screen; when the screen changes it
// no longer holds. Found end to end: a shell's startup banner read as
// waiting_input, and — sticky like an agent's wait — it pulsed until
// looked at and hid the permission prompt printed after it.
func TestOutputClearsLayaWait(t *testing.T) {
	m := New(t0)
	m.Classify(wire.StateWaitingInput, t0)
	if !m.Output(t0.Add(time.Second)) {
		t.Fatal("output did not clear a Laya wait")
	}
	if got := m.Snapshot(); got.State != wire.StateWorking || got.Source != wire.StateSourceHeuristic {
		t.Errorf("snapshot = %+v, want heuristic working", got)
	}
}

func TestOutputKeepsAgentAndBellWaits(t *testing.T) {
	m := New(t0)
	m.Bell(t0)
	if m.Output(t0.Add(time.Second)) || m.Snapshot().State != wire.StateWaitingInput {
		t.Error("output cleared a bell's wait")
	}
	h := New(t0)
	h.Apply(hookEvent(KindWaitingPermission, t0, ""))
	if h.Output(t0.Add(time.Second)) || h.Snapshot().State != wire.StateWaitingPermission {
		t.Error("output cleared an agent-reported wait")
	}
}

// On a heartbeating Pi, Output leaves the tier alone, so Laya revises
// its own wait instead of it standing.
func TestLayaRevisesItsOwnWait(t *testing.T) {
	m, _, now := piStuckWorking(t)
	m.Classify(wire.StateWaitingInput, now)
	m.Output(now.Add(time.Second))
	if !m.Classify(wire.StateWorking, now.Add(2*time.Second)) {
		t.Fatal("Laya could not revise its own wait")
	}
	if got := m.Snapshot(); got.State != wire.StateWorking || got.Source != wire.StateSourceLaya {
		t.Errorf("snapshot = %+v, want laya working", got)
	}
}

// Review finding (PR #464): a bell on a Laya-sourced session is the
// program asking, not a guess — it must not be revisable or cleared by
// the next redraw.
func TestBellTakesTierBackFromLaya(t *testing.T) {
	m := New(t0)
	m.Classify(wire.StateIdle, t0)
	m.Bell(t0.Add(time.Second))
	if got := m.Snapshot(); got.State != wire.StateWaitingInput || got.Source != wire.StateSourceHeuristic {
		t.Fatalf("snapshot = %+v, want waiting_input on the heuristic tier", got)
	}
	if m.Output(t0.Add(2 * time.Second)) {
		t.Error("output cleared a bell's wait")
	}
	if m.Classify(wire.StateWorking, t0.Add(3*time.Second)) {
		t.Error("Laya revised a bell's wait")
	}
}

// On a heartbeating Pi, the bell becomes the extension's last say, so
// the next heartbeat keeps the wait rather than restoring the stale
// report Laya had corrected.
func TestBellOnLayaPiSurvivesHeartbeat(t *testing.T) {
	m, ev, now := piStuckWorking(t)
	m.Classify(wire.StateIdle, now)
	m.Bell(now.Add(time.Second))
	hb := ev
	hb.Now = now.Add(2 * time.Second)
	deliver(m, hb)
	if got := m.Snapshot(); got.State != wire.StateWaitingInput || got.Source != wire.StateSourceExtension {
		t.Errorf("snapshot = %+v, want the bell's wait kept on the extension tier", got)
	}
}

// Review finding (PR #464): a ping after a classification relabelled
// Laya's guess as the agent's report, pinning a Laya wait.
func TestStatelessEventsKeepLayaLabel(t *testing.T) {
	for name, ev := range map[string]Event{
		"ping":     {Kind: KindPing, Source: wire.StateSourceHook},
		"plan":     {Kind: KindPlan, Source: wire.StateSourceHook},
		"subagent": {Kind: KindToolStart, Source: wire.StateSourceHook, AgentID: "sub-1", CallID: "c1", Tool: "Bash"},
	} {
		m := New(t0)
		m.Classify(wire.StateWaitingInput, t0)
		ev.At, ev.Now = t0.Add(time.Second), t0.Add(time.Second)
		m.Apply(ev)
		if got := m.Snapshot(); got.Source != wire.StateSourceLaya {
			t.Errorf("%s: source = %q, want laya kept", name, got.Source)
		}
		// Still Laya's to revise once no tier is speaking — at once for a
		// ping (no state-bearing event), after HookStaleAfter for real
		// activity like a plan or a subagent.
		if !m.Classify(wire.StateWorking, t0.Add(HookStaleAfter+2*time.Second)) {
			t.Errorf("%s: the Laya wait became unrevisable", name)
		}
	}
}

func TestStateBearingEventTakesOverFromLaya(t *testing.T) {
	m := New(t0)
	m.Classify(wire.StateWaitingInput, t0)
	m.Apply(hookEvent(KindPrompt, t0.Add(time.Second), "go"))
	if got := m.Snapshot(); got.Source != wire.StateSourceHook || got.State != wire.StateWorking {
		t.Errorf("snapshot = %+v, want the hook's working", got)
	}
}
