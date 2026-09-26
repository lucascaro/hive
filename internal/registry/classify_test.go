package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agentstate"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// fakeLaya records every call and answers with whatever it is set to.
type fakeLaya struct {
	mu     sync.Mutex
	calls  []string
	answer agentstate.State
	err    error
	// block, when non-nil, holds every call until it is closed or the
	// call's context ends.
	block chan struct{}
}

func (f *fakeLaya) classify(ctx context.Context, screen string) (agentstate.State, error) {
	f.mu.Lock()
	f.calls = append(f.calls, screen)
	block, answer, err := f.block, f.answer, f.err
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return answer, err
}

func (f *fakeLaya) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeLaya) set(answer agentstate.State, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answer, f.err = answer, err
}

// classifierRig is a registry whose state ticker and classifier loop are
// both stopped, so the test is the only thing driving time: sample for
// the screen, classifyCycle for Laya.
func classifierRig(t *testing.T, answer agentstate.State) (*Registry, *Entry, *session.Session, *fakeLaya) {
	t.Helper()
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	r.stopClassifier()
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "laya"})
	f := &fakeLaya{answer: answer}
	r.SetClassifier(f.classify)
	return r, e, sess, f
}

// settle paints text and records it as the screen's last change at now.
func settle(t *testing.T, r *Registry, e *Entry, sess *session.Session, text string, now time.Time) {
	t.Helper()
	paint(t, e, sess, text)
	sample(r, e, now)
}

func cycle(r *Registry, now time.Time) { r.classifyCycle(context.Background(), now) }

func stateOf(r *Registry, e *Entry) (string, string) {
	info := r.Get(e.ID).Info()
	return info.State, info.StateSource
}

func TestClassifierRunsOnceAfterQuiet(t *testing.T) {
	r, e, sess, f := classifierRig(t, wire.StateWaitingInput)
	ch, unsub := r.Subscribe()
	defer unsub()
	base := time.Now()
	settle(t, r, e, sess, "Continue? [y/N]", base)
	drain(ch)

	cycle(r, base.Add(agentstate.ClassifyQuietAfter/2))
	if f.count() != 0 {
		t.Fatalf("classified a screen quiet for only %s", agentstate.ClassifyQuietAfter/2)
	}
	cycle(r, base.Add(agentstate.ClassifyQuietAfter))
	cycle(r, base.Add(2*agentstate.ClassifyQuietAfter))
	cycle(r, base.Add(10*agentstate.ClassifyQuietAfter))
	if got := f.count(); got != 1 {
		t.Fatalf("calls = %d, want exactly 1 for one settled screen", got)
	}
	if st, src := stateOf(r, e); st != wire.StateWaitingInput || src != wire.StateSourceLaya {
		t.Errorf("state = %q/%q, want waiting_input/laya", st, src)
	}
	if !strings.Contains(f.calls[0], "Continue? [y/N]") {
		t.Errorf("screen sent = %q, want the visible text", f.calls[0])
	}
	sawState := false
	for len(ch) > 0 {
		if ev := <-ch; ev.Kind == wire.SessionEventState && ev.Session.ID == e.ID {
			sawState = true
		}
	}
	if !sawState {
		t.Error("no state event broadcast for the classification")
	}
}

// Spec criterion 2: a changed screen's state is reflected within 3s. The
// loop wakes every classifyInterval; with an answer that comes back at
// once, the state lands by ClassifyQuietAfter plus one interval.
func TestClassifierAppliesWithin3s(t *testing.T) {
	r, e, sess, _ := classifierRig(t, wire.StateWaitingPermission)
	base := time.Now()
	settle(t, r, e, sess, "Allow this command? (y/n)", base)
	var landed time.Duration = -1
	for at := classifyInterval; at <= 3*time.Second; at += classifyInterval {
		cycle(r, base.Add(at))
		if st, _ := stateOf(r, e); st == wire.StateWaitingPermission {
			landed = at
			break
		}
	}
	if landed < 0 {
		t.Fatal("state not applied within 3s of the screen changing")
	}
	if max := agentstate.ClassifyQuietAfter + classifyInterval; landed > max {
		t.Errorf("state landed at %s, want by %s", landed, max)
	}
}

func TestClassifierNotCalledWhileStreaming(t *testing.T) {
	r, e, sess, f := classifierRig(t, wire.StateIdle)
	base := time.Now()
	for i := 0; i < 8; i++ {
		now := base.Add(time.Duration(i) * classifyInterval)
		settle(t, r, e, sess, "token ", now)
		cycle(r, now)
	}
	if got := f.count(); got != 0 {
		t.Errorf("calls = %d while the screen changed every cycle, want 0", got)
	}
}

func TestClassifierNotCalledForTrustedHook(t *testing.T) {
	r, e, sess, f := classifierRig(t, wire.StateIdle)
	base := time.Now()
	settle(t, r, e, sess, "hooked agent", base)
	r.mu.Lock()
	r.entries[e.ID].machine().Apply(agentstate.Event{Kind: agentstate.KindPrompt,
		Source: wire.StateSourceHook, At: base, Now: base})
	r.mu.Unlock()
	cycle(r, base.Add(5*time.Second))
	if got := f.count(); got != 0 {
		t.Errorf("calls = %d for a hook that reported 5s ago, want 0", got)
	}
}

// The spec's Pi case end to end through the loop: the extension's last
// real report was a tool_start, the heartbeat keeps repeating it, and
// the screen shows a question.
func TestClassifierCalledForHeartbeatingButEventStalePi(t *testing.T) {
	r, e, sess, f := classifierRig(t, wire.StateWaitingInput)
	base := time.Now()
	settle(t, r, e, sess, "Which file? >", base)
	ev := agentstate.Event{Kind: agentstate.KindToolStart, Source: wire.StateSourceExtension,
		At: base, Now: base, Instance: "A", Seq: 1}
	r.mu.Lock()
	m := r.entries[e.ID].machine()
	m.Apply(ev)
	var now time.Time
	for s := 5 * time.Second; s <= agentstate.HookStaleAfter+5*time.Second; s += 5 * time.Second {
		now = base.Add(s)
		hb := ev
		hb.Now = now
		m.Replay(hb)
	}
	r.mu.Unlock()

	cycle(r, now)
	if got := f.count(); got != 1 {
		t.Fatalf("calls = %d, want 1 for an event-stale Pi", got)
	}
	if st, src := stateOf(r, e); st != wire.StateWaitingInput || src != wire.StateSourceLaya {
		t.Errorf("state = %q/%q, want waiting_input/laya", st, src)
	}
	if !r.Get(e.ID).Info().NeedsAttention {
		t.Error("a Laya-detected wait did not raise attention")
	}
}

func TestClassifierRechecksLayaWorkingAfterTTL(t *testing.T) {
	r, e, sess, f := classifierRig(t, wire.StateWorking)
	base := time.Now()
	settle(t, r, e, sess, "npm test", base)
	for at := time.Second; at <= 2*time.Minute; at += classifyInterval {
		cycle(r, base.Add(at))
	}
	// First call at 1s, then one per LayaRecheckAfter: 31s, 61s, 91s.
	if got, want := f.count(), 1+int((2*time.Minute-time.Second)/agentstate.LayaRecheckAfter); got != want {
		t.Errorf("calls = %d over 2m of a static laya-working screen, want %d", got, want)
	}
}

func TestClassifierDoesNotRecheckIdleOrWaiting(t *testing.T) {
	for _, answer := range []agentstate.State{wire.StateIdle, wire.StateWaitingInput} {
		r, e, sess, f := classifierRig(t, answer)
		base := time.Now()
		settle(t, r, e, sess, "$ ", base)
		for at := time.Second; at <= 2*time.Minute; at += 5 * time.Second {
			cycle(r, base.Add(at))
		}
		if got := f.count(); got != 1 {
			t.Errorf("answer %q: calls = %d, want 1", answer, got)
		}
	}
}

func TestClassifierErrorKeepsHeuristic(t *testing.T) {
	r, e, sess, f := classifierRig(t, "")
	f.set("", errors.New("connection refused"))
	base := time.Now()
	settle(t, r, e, sess, "output", base)
	sample(r, e, base.Add(agentstate.QuietAfter))
	want, wantSrc := stateOf(r, e)
	cycle(r, base.Add(agentstate.QuietAfter))
	if st, src := stateOf(r, e); st != want || src != wantSrc {
		t.Errorf("state = %q/%q after a failed call, want the heuristic's %q/%q", st, src, want, wantSrc)
	}
}

func TestClassifierSameScreenNotRetriedAfterError(t *testing.T) {
	r, e, sess, f := classifierRig(t, "")
	f.set("", errors.New("boom"))
	base := time.Now()
	settle(t, r, e, sess, "static", base)
	for at := time.Second; at <= 5*time.Minute; at += classifyInterval {
		cycle(r, base.Add(at))
	}
	if got := f.count(); got != 1 {
		t.Errorf("calls = %d for one failed static screen, want 1", got)
	}
}

// Backoff, not the per-screen record, is what is under test: the screen
// changes before every cycle, so without backoff every cycle would call.
func TestClassifierErrorDoesNotHammer(t *testing.T) {
	r, e, sess, f := classifierRig(t, "")
	f.set("", errors.New("connection refused"))
	base := time.Now()
	var calledAt []time.Duration
	for i := 0; i < 20; i++ {
		// A new screen, settled for ClassifyQuietAfter by the cycle.
		at := time.Duration(i) * 500 * time.Millisecond
		settle(t, r, e, sess, "x", base.Add(at-agentstate.ClassifyQuietAfter))
		before := f.count()
		cycle(r, base.Add(at))
		if f.count() > before {
			calledAt = append(calledAt, at)
		}
	}
	want := []time.Duration{0, 2 * time.Second, 6 * time.Second}
	if len(calledAt) != len(want) {
		t.Fatalf("calls at %v, want exactly at the backoff edges %v", calledAt, want)
	}
	for i := range want {
		if calledAt[i] != want[i] {
			t.Errorf("call %d at %s, want %s", i, calledAt[i], want[i])
		}
	}
}

func TestClassifierBackoffResetsOnSuccess(t *testing.T) {
	r, e, sess, f := classifierRig(t, "")
	f.set("", errors.New("down"))
	base := time.Now()
	settle(t, r, e, sess, "a", base)
	cycle(r, base.Add(time.Second)) // fails; backoff until 3s
	f.set(wire.StateIdle, nil)
	settle(t, r, e, sess, "b", base.Add(2*time.Second))
	cycle(r, base.Add(3*time.Second)) // first call after the window: succeeds
	settle(t, r, e, sess, "c", base.Add(3*time.Second))
	cycle(r, base.Add(4*time.Second)) // next screen: called at once
	if got := f.count(); got != 3 {
		t.Errorf("calls = %d, want 3 — success must clear the backoff", got)
	}
}

func TestClassifierDisabledMakesNoCalls(t *testing.T) {
	r, e, sess, f := classifierRig(t, wire.StateIdle)
	r.SetClassifier(nil)
	base := time.Now()
	settle(t, r, e, sess, "anything", base)
	cycle(r, base.Add(5*time.Second))
	if f.count() != 0 {
		t.Error("a nil classifier was called")
	}
}

// An answer about a screen that has since changed is about the past.
func TestClassifierDropsResultIfScreenChanged(t *testing.T) {
	r, e, sess, f := classifierRig(t, wire.StateWaitingInput)
	f.block = make(chan struct{})
	base := time.Now()
	settle(t, r, e, sess, "Proceed?", base)
	done := make(chan struct{})
	go func() { cycle(r, base.Add(time.Second)); close(done) }()
	waitCalls(t, f, 1)
	settle(t, r, e, sess, " working again", base.Add(1500*time.Millisecond))
	close(f.block)
	<-done
	if st, src := stateOf(r, e); src == wire.StateSourceLaya {
		t.Errorf("state = %q/%q — an answer about a stale screen was applied", st, src)
	}
}

// An agent event landing mid-call puts the session back under a live
// tier, and Classify's own check refuses the answer.
func TestClassifierDropsResultIfAgentSpokeMeanwhile(t *testing.T) {
	r, e, sess, f := classifierRig(t, wire.StateIdle)
	f.block = make(chan struct{})
	base := time.Now()
	settle(t, r, e, sess, "thinking", base)
	done := make(chan struct{})
	go func() { cycle(r, base.Add(time.Second)); close(done) }()
	waitCalls(t, f, 1)
	r.mu.Lock()
	r.entries[e.ID].machine().Apply(agentstate.Event{Kind: agentstate.KindPrompt,
		Source: wire.StateSourceHook, At: base.Add(time.Second), Now: base.Add(time.Second)})
	r.mu.Unlock()
	close(f.block)
	<-done
	if st, src := stateOf(r, e); st != wire.StateWorking || src != wire.StateSourceHook {
		t.Errorf("state = %q/%q, want the hook's working", st, src)
	}
}

func TestClassifierNotHeldUnderLock(t *testing.T) {
	r, e, sess, f := classifierRig(t, wire.StateIdle)
	f.block = make(chan struct{})
	defer close(f.block)
	base := time.Now()
	settle(t, r, e, sess, "slow server", base)
	go cycle(r, base.Add(time.Second))
	waitCalls(t, f, 1)
	got := make(chan int, 1)
	go func() { got <- len(r.List()) }()
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("List blocked while a classification was in flight — the call holds r.mu")
	}
}

func TestCloseCancelsInflightClassify(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	manualClock(t, r)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "laya"})
	f := &fakeLaya{answer: wire.StateWaitingInput, block: make(chan struct{})}
	r.SetClassifier(f.classify)
	settle(t, r, e, sess, "Proceed?", time.Now().Add(-time.Minute))
	// The real loop, not a test-driven cycle: Close must stop it.
	waitCalls(t, f, 1)
	closed := make(chan error, 1)
	go func() { closed <- r.Close() }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close hung on an in-flight classification")
	}
	if !r.classifierStopped() {
		t.Error("classifier loop still running after Close")
	}
}

func TestClassifierCapturesScreens(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "captures")
	t.Setenv(classifyCaptureDirEnv, dir)
	r, e, sess, _ := classifierRig(t, wire.StateWaitingInput)
	base := time.Now()
	settle(t, r, e, sess, "Save changes? (y/n)", base)
	cycle(r, base.Add(time.Second))
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("captures = %v (err %v), want 1 file", files, err)
	}
	name := files[0].Name()
	if !strings.HasPrefix(name, "shell-") || !strings.HasSuffix(name, "-waiting_input.txt") {
		t.Errorf("capture name = %q, want shell-<ts>-waiting_input.txt", name)
	}
	info, _ := files[0].Info()
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("capture mode = %o, want 600 — screens can hold secrets", perm)
	}
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Errorf("capture dir mode = %o, want 700", st.Mode().Perm())
	}
}

func TestClassifierCapturesFailedAttempts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "captures")
	t.Setenv(classifyCaptureDirEnv, dir)
	r, e, sess, f := classifierRig(t, "")
	f.set("", errors.New("no route to Laya"))
	base := time.Now()
	settle(t, r, e, sess, "Pick one:", base)
	cycle(r, base.Add(time.Second))
	files, _ := os.ReadDir(dir)
	if len(files) != 1 || !strings.HasSuffix(files[0].Name(), "-unclassified.txt") {
		t.Fatalf("captures = %v, want one <agent>-<ts>-unclassified.txt", files)
	}
}

// Switched off is not a failure: no backoff, no capture, and the screen
// is asked about as soon as it is switched back on.
func TestClassifierOffIsNotAFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "captures")
	t.Setenv(classifyCaptureDirEnv, dir)
	r, e, sess, f := classifierRig(t, "")
	f.set("", ErrClassifierOff)
	base := time.Now()
	settle(t, r, e, sess, "Proceed?", base)
	cycle(r, base.Add(time.Second))
	if _, err := os.Stat(dir); err == nil {
		t.Error("a switched-off classifier captured a screen")
	}
	f.set(wire.StateWaitingInput, nil)
	cycle(r, base.Add(1500*time.Millisecond))
	if st, src := stateOf(r, e); st != wire.StateWaitingInput || src != wire.StateSourceLaya {
		t.Errorf("state = %q/%q after switching on, want waiting_input/laya at once", st, src)
	}
}

// The end-to-end finding behind non-sticky Laya waits: a misread wait
// must not hide the next screen.
func TestLayaWaitDoesNotHideNextScreen(t *testing.T) {
	r, e, sess, f := classifierRig(t, wire.StateWaitingInput)
	base := time.Now()
	settle(t, r, e, sess, "Last login: banner", base)
	cycle(r, base.Add(time.Second))
	if !r.Get(e.ID).Info().NeedsAttention {
		t.Fatal("precondition: the misread wait raised attention")
	}
	f.set(wire.StateWaitingPermission, nil)
	settle(t, r, e, sess, "Allow command? [y]es / [n]o", base.Add(2*time.Second))
	if info := r.Get(e.ID).Info(); info.NeedsAttention || info.State != wire.StateWorking {
		t.Errorf("after the screen changed: state=%q attention=%v, want working and no attention", info.State, info.NeedsAttention)
	}
	cycle(r, base.Add(3*time.Second))
	if st, src := stateOf(r, e); st != wire.StateWaitingPermission || src != wire.StateSourceLaya {
		t.Errorf("state = %q/%q, want the new screen classified: waiting_permission/laya", st, src)
	}
}

func waitCalls(t *testing.T, f *fakeLaya, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for f.count() < n {
		if time.Now().After(deadline) {
			t.Fatalf("classifier called %d times, want %d", f.count(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
