package agentstate

import (
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// The osc tier (spec 517): Claude's OSC 7501 status read from the PTY.
// It fills gaps, never overrides a fresh tier, and has exactly one
// exception — Esc, which no hook reports.

func snap(t *testing.T, m *Machine, state State, src Source) {
	t.Helper()
	if got := m.Snapshot(); got.State != state || got.Source != src {
		t.Fatalf("snapshot = %+v, want %s from %q", got, state, src)
	}
}

func TestOSCFillsGapWhenNoTierSpeaks(t *testing.T) {
	m := New(t0)
	if !m.OSC("working", "", t0.Add(time.Second)) {
		t.Fatal("osc working on a quiet heuristic session changed nothing")
	}
	snap(t, m, wire.StateWorking, wire.StateSourceOSC)
}

func TestOSCDoneMapsToTurnEnd(t *testing.T) {
	m := New(t0)
	m.OSC("working", "", t0)
	m.OSC("done", "", t0.Add(time.Second))
	snap(t, m, wire.StateWaitingInput, wire.StateSourceOSC)
	if !m.Snapshot().TurnEnded {
		t.Error("osc done is a finished turn, like a hook turn_end")
	}
}

func TestOSCBlockedKinds(t *testing.T) {
	for kind, want := range map[string]State{
		"question":   wire.StateWaitingInput,
		"auth":       wire.StateWaitingInput,
		"":           wire.StateWaitingInput,
		"permission": wire.StateWaitingPermission,
	} {
		m := New(t0)
		m.OSC("blocked", kind, t0)
		snap(t, m, want, wire.StateSourceOSC)
	}
}

func TestOSCIgnoresClearAndUnknownStates(t *testing.T) {
	m := New(t0)
	m.OSC("working", "", t0)
	for _, st := range []string{"clear", "dancing", ""} {
		if m.OSC(st, "", t0.Add(time.Second)) {
			t.Errorf("osc %q changed state", st)
		}
	}
	snap(t, m, wire.StateWorking, wire.StateSourceOSC)
}

func TestOSCDoesNotOverrideFreshHook(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindWaitingPermission, t0, ""))
	for _, st := range []string{"working", "done", "blocked"} {
		if m.OSC(st, "question", t0.Add(2*time.Second)) {
			t.Errorf("osc %s overrode a fresh hook wait", st)
		}
	}
	snap(t, m, wire.StateWaitingPermission, wire.StateSourceHook)
}

// The wait guard alone would pass the test above; this one reaches the
// precedence rule itself: a hook that says working, and is fresh.
func TestOSCDoesNotOverrideFreshHookWorking(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindToolStart, t0, ""))
	for _, st := range []string{"done", "blocked", "error"} {
		if m.OSC(st, "question", t0.Add(2*time.Second)) {
			t.Errorf("osc %s overrode a fresh hook working", st)
		}
	}
	snap(t, m, wire.StateWorking, wire.StateSourceHook)
}

func TestOSCDoesNotOverrideFreshExtensionOrACP(t *testing.T) {
	e := New(t0)
	deliver(e, piKeyed(KindToolStart, "A", 1, t0, t0))
	if e.OSC("idle", "", t0.Add(time.Second)) {
		t.Error("osc overrode a fresh extension report")
	}
	snap(t, e, wire.StateWorking, wire.StateSourceExtension)

	a := New(t0)
	a.Apply(Event{Kind: KindToolStart, Source: wire.StateSourceACP, At: t0})
	a.OSC("working", "", t0)
	if a.OSC("idle", "", t0.Add(time.Second)) {
		t.Error("osc ended an ACP session's state")
	}
	snap(t, a, wire.StateWorking, wire.StateSourceACP)
}

func TestOSCTakesOverStaleHook(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindToolStart, t0, ""))
	if !m.OSC("blocked", "question", t0.Add(HookStaleAfter+time.Second)) {
		t.Fatal("osc did not fill in for a hook silent past HookStaleAfter")
	}
	snap(t, m, wire.StateWaitingInput, wire.StateSourceOSC)
}

// The Esc case. Claude fires no hook on interrupt, so the hook tier is
// fresh and says working; osc idle ends it, and the hook's label stays.
func TestOSCIdleEndsHookWorkingAfterOSCWorking(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindPrompt, t0, "go"))
	m.OSC("working", "", t0)
	if !m.OSC("idle", "", t0.Add(2*time.Second)) {
		t.Fatal("Esc (osc idle) did not end the hook's working")
	}
	snap(t, m, wire.StateIdle, wire.StateSourceHook)
}

func TestOSCIdleWithoutOSCWorkingIsAGap(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindPrompt, t0, "go"))
	if m.OSC("idle", "", t0.Add(time.Second)) {
		t.Error("a startup-style osc idle ended a hook working it never saw begin")
	}
	snap(t, m, wire.StateWorking, wire.StateSourceHook)
}

func TestOSCIdleNeverClearsAWait(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindWaitingPermission, t0, ""))
	m.OSC("working", "", t0)
	m.OSC("idle", "", t0.Add(time.Second))
	snap(t, m, wire.StateWaitingPermission, wire.StateSourceHook)

	b := New(t0)
	b.Bell(t0)
	b.OSC("working", "", t0)
	b.OSC("idle", "", t0.Add(time.Second))
	snap(t, b, wire.StateWaitingInput, wire.StateSourceHeuristic)
}

// Review round 1: a hook process in flight when Esc lands, or fired by
// the interrupt itself, is stamped after it and must not bring the turn
// back (or raise a wait for it).
func TestToolFailureAfterOSCIdleStaysIdle(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindToolStart, t0, ""))
	m.OSC("working", "", t0)
	m.OSC("idle", "", t0.Add(2*time.Second))
	m.Apply(hookEvent(KindToolEnd, t0.Add(3*time.Second), "")) // PostToolUseFailure
	snap(t, m, wire.StateIdle, wire.StateSourceHook)
}

func TestLatePermissionRequestAfterOSCIdleStaysIdle(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindToolStart, t0, ""))
	m.OSC("working", "", t0)
	m.OSC("idle", "", t0.Add(2*time.Second))
	m.Apply(hookEvent(KindWaitingPermission, t0.Add(3*time.Second), ""))
	m.Apply(hookEvent(KindWaitingInput, t0.Add(4*time.Second), ""))
	snap(t, m, wire.StateIdle, wire.StateSourceHook)
}

func TestPromptAfterOSCIdleClearsLatch(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindToolStart, t0, ""))
	m.OSC("working", "", t0)
	m.OSC("idle", "", t0.Add(time.Second))
	m.Apply(hookEvent(KindPrompt, t0.Add(3*time.Second), "again"))
	m.Apply(hookEvent(KindToolStart, t0.Add(4*time.Second), ""))
	snap(t, m, wire.StateWorking, wire.StateSourceHook)
}

func TestInterruptLatchExpires(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindToolStart, t0, ""))
	m.OSC("working", "", t0)
	m.OSC("idle", "", t0.Add(time.Second))
	m.Apply(hookEvent(KindToolStart, t0.Add(time.Second+interruptLatch+time.Second), ""))
	snap(t, m, wire.StateWorking, wire.StateSourceHook)
}

// Esc on a permission dialog: the keystroke that dismisses it must not
// resume the turn the dialog was blocking.
func TestClearWaitingAfterInterruptedPermissionIsIdle(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindWaitingPermission, t0, ""))
	m.OSC("working", "", t0)
	m.OSC("idle", "", t0.Add(time.Second))
	m.ClearWaiting()
	snap(t, m, wire.StateIdle, wire.StateSourceHook)
}

// The same, with Claude's real report order (found by the live probe):
// the dialog is a blocked report inside the turn, and Esc follows it.
func TestEscOnBlockedDialogStillLatchesInterrupt(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindWaitingPermission, t0, ""))
	m.OSC("working", "", t0)
	m.OSC("blocked", "permission", t0.Add(time.Second))
	m.OSC("idle", "", t0.Add(2*time.Second))
	m.ClearWaiting()
	snap(t, m, wire.StateIdle, wire.StateSourceHook)
}

func TestOSCRevisesItsOwnWaits(t *testing.T) {
	m := New(t0)
	m.OSC("working", "", t0)
	m.OSC("done", "", t0.Add(time.Second))
	if !m.OSC("working", "", t0.Add(2*time.Second)) {
		t.Fatal("osc could not leave its own done")
	}
	snap(t, m, wire.StateWorking, wire.StateSourceOSC)
	m.OSC("blocked", "question", t0.Add(3*time.Second))
	if !m.OSC("idle", "", t0.Add(4*time.Second)) {
		t.Fatal("osc could not leave its own blocked")
	}
	snap(t, m, wire.StateIdle, wire.StateSourceOSC)
}

func TestOSCRevisesLayaButNotItsOwnClock(t *testing.T) {
	m := New(t0)
	m.Classify(wire.StateWaitingInput, t0)
	if !m.OSC("working", "", t0.Add(time.Second)) {
		t.Fatal("osc could not replace a Laya guess")
	}
	snap(t, m, wire.StateWorking, wire.StateSourceOSC)
}

func TestLayaDoesNotOverrideFreshOSC(t *testing.T) {
	m := New(t0)
	m.OSC("working", "", t0)
	if m.Classify(wire.StateIdle, t0.Add(time.Second)) {
		t.Error("a screen guess overwrote a fresh exact report")
	}
	if !m.Classify(wire.StateIdle, t0.Add(HookStaleAfter+time.Second)) {
		t.Error("Laya could not classify once osc went quiet")
	}
}

func TestOutputDoesNotUndoFreshOSCIdle(t *testing.T) {
	m := New(t0)
	m.OSC("working", "", t0)
	m.OSC("idle", "", t0.Add(time.Second))
	if m.Output(t0.Add(1500 * time.Millisecond)) { // "Interrupted" repaint
		t.Error("the repaint after Esc flipped idle back to working")
	}
	snap(t, m, wire.StateIdle, wire.StateSourceOSC)
}

func TestStaleOSCFallsThroughToHeuristic(t *testing.T) {
	m := New(t0)
	m.OSC("idle", "", t0)
	if !m.Output(t0.Add(HookStaleAfter + time.Second)) {
		t.Error("output did not take back a state osc stopped reporting")
	}
	snap(t, m, wire.StateWorking, wire.StateSourceHeuristic)
}

func TestTickDemotesOrphanOSCWorking(t *testing.T) {
	m := New(t0)
	m.OSC("working", "", t0)
	if !m.Tick(t0.Add(QuietAfter + time.Second)) {
		t.Error("a lost osc idle would pin working forever")
	}
	snap(t, m, wire.StateIdle, wire.StateSourceHeuristic)
}

func TestHookApplyOverwritesOSC(t *testing.T) {
	m := New(t0)
	m.OSC("done", "", t0)
	m.Apply(hookEvent(KindPrompt, t0.Add(time.Second), "go"))
	snap(t, m, wire.StateWorking, wire.StateSourceHook)
}

func TestOSCDoesNotTouchTierClocks(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindPrompt, t0, "go"))
	hookSeen, order, reported, event := m.hookSeenAt, m.orderAt, m.reportedAt, m.lastEventAt
	m.OSC("working", "", t0.Add(HookStaleAfter+time.Second))
	if m.hookSeenAt != hookSeen || m.orderAt != order || m.reportedAt != reported || m.lastEventAt != event {
		t.Error("OSC moved a tier clock")
	}
}

func TestStaleAtIsNotSetForOSC(t *testing.T) {
	m := New(t0)
	m.OSC("working", "", t0)
	if _, ok := m.StaleAt(); ok {
		t.Error("StaleAt set on an osc-sourced state")
	}
}

// Brain: pi-heartbeat-masks-hook-staleness. A heartbeat refreshes
// hookSeenAt, which must not make a stale osc state look trusted.
func TestOSCDoesNotMaskStaleHeartbeatingPi(t *testing.T) {
	m, ev, now := piStuckWorking(t)
	m.Classify(wire.StateIdle, now)
	m.OSC("working", "", now.Add(time.Second))
	hb := ev
	hb.Now = now.Add(2 * time.Second)
	deliver(m, hb)
	if m.trusted(now.Add(2 * time.Second)) {
		t.Error("an osc-sourced state counted as trusted because a heartbeat refreshed hookSeenAt")
	}
	snap(t, m, wire.StateWorking, wire.StateSourceOSC)
}

func TestOSCIgnoredAfterExit(t *testing.T) {
	m := New(t0)
	m.Exit()
	if m.OSC("working", "", t0) {
		t.Error("osc resurrected an exited session")
	}
}

// Found by the live probe: Claude prints `idle` at startup, after the
// SessionStart ping has made the hook tier live but before any
// state-bearing event. That restates what the session already is and
// must not relabel it "osc".
func TestOSCStartupIdleKeepsPingedHookLabel(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindPing, t0, ""))
	if m.OSC("idle", "", t0.Add(time.Second)) {
		t.Error("a restated idle changed something")
	}
	snap(t, m, wire.StateIdle, wire.StateSourceHook)
	// A change from a tier that has only pinged is a real gap: hooks that
	// never report the prompt still get a working session, and the live
	// tier keeps its label (found by the live probe: relabelling at
	// startup replaced "reported by the agent" before any hook event).
	if !m.OSC("working", "", t0.Add(2*time.Second)) {
		t.Fatal("osc could not fill in for a tier that only pinged")
	}
	snap(t, m, wire.StateWorking, wire.StateSourceHook)
}

// Review round 1 (PR #520): Esc after a long tool run, when the hook
// tier has been silent past HookStaleAfter. The idle must survive the
// repaint that follows.
func TestOSCEscOnStaleTierSurvivesRepaint(t *testing.T) {
	m := New(t0)
	m.Apply(hookEvent(KindPrompt, t0, "go"))
	m.OSC("working", "", t0)
	esc := t0.Add(HookStaleAfter + 5*time.Second)
	m.Output(esc.Add(-time.Second)) // the spinner: heuristic territory by now
	if !m.OSC("idle", "", esc) {
		t.Fatal("Esc did not end working on a stale tier")
	}
	if m.Output(esc.Add(100 * time.Millisecond)) {
		t.Error("the repaint after Esc undid the idle")
	}
	snap(t, m, wire.StateIdle, wire.StateSourceOSC)
	if m.Classifiable(esc.Add(time.Second)) {
		t.Error("a screen guess may overwrite a fresh Esc idle")
	}
}
