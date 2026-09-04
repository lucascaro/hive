package registry

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agentstate"
	"github.com/lucascaro/hive/internal/wire"
)

// nextStateEvent reads events until one carries the state kind for id,
// failing rather than hanging. A real /bin/bash session emits title and
// phase events at times no assertion can predict, so every test here
// filters rather than indexes.
func nextStateEvent(t *testing.T, ch Listener, id string) wire.SessionEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Kind == wire.SessionEventState && ev.Session.ID == id {
				return ev
			}
		case <-deadline:
			t.Fatal("no state event arrived")
			return wire.SessionEvent{}
		}
	}
}

// A burst of PTY output must cost one broadcast, not one per chunk. The
// session hook is deliberately unthrottled — the machine needs every
// timestamp to know when output stopped — so the coalescing has to
// happen here, on the "did the visible state change" test.
func TestOutputBurstBroadcastsOnce(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e := mustCreate(t, r, wire.CreateSpec{Name: "busy"})

	// Settle first: the shell has already produced its prompt, which is
	// itself a legitimate idle → working transition.
	time.Sleep(100 * time.Millisecond)
	ch, unsub := r.Subscribe()
	defer unsub()
	r.mu.Lock()
	r.entries[e.ID].machine().Tick(time.Now().Add(time.Hour)) // force idle
	r.mu.Unlock()
	drain(ch)

	for range 50 {
		r.noteOutput(e.ID)
	}

	ev := nextStateEvent(t, ch, e.ID)
	if ev.Session.State != wire.StateWorking {
		t.Errorf("state = %q, want %q", ev.Session.State, wire.StateWorking)
	}
	// Nothing else: 49 more broadcasts to every connected client is the
	// cost this guard exists to avoid.
	for {
		select {
		case extra := <-ch:
			if extra.Kind == wire.SessionEventState && extra.Session.ID == e.ID {
				t.Fatalf("a second state broadcast for one burst: %+v", extra.Session.State)
			}
		default:
			return
		}
	}
}

// The quiet timer is driven by a registry-wide ticker. Asserting through
// it rather than calling Tick directly is the point: a machine that is
// correct but never ticked leaves every session stuck at "working".
func TestQuietGoesIdle(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e := mustCreate(t, r, wire.CreateSpec{Name: "quiet"})

	ch, unsub := r.Subscribe()
	defer unsub()
	drain(ch)
	r.noteOutput(e.ID)

	deadline := time.After(agentstate.QuietAfter + 5*time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Kind == wire.SessionEventState && ev.Session.ID == e.ID &&
				ev.Session.State == wire.StateIdle {
				return
			}
		case <-deadline:
			t.Fatalf("session never went idle after %s of silence", agentstate.QuietAfter)
		}
	}
}

// A bell puts a heuristic session into waiting_input, and the client
// reporting that the user looked resolves it. This is the entire
// user-facing loop of the heuristic tier.
func TestBellWaitsThenClientLookResolves(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e := mustCreate(t, r, wire.CreateSpec{Name: "bell"})

	r.noteBell(e.ID)
	if got := r.Get(e.ID).Info().State; got != wire.StateWaitingInput {
		t.Fatalf("state = %q, want %q", got, wire.StateWaitingInput)
	}
	if err := r.SetAttention(e.ID, false); err != nil {
		t.Fatalf("SetAttention: %v", err)
	}
	if got := r.Get(e.ID).Info().State; got != wire.StateIdle {
		t.Errorf("state = %q after the user looked, want %q", got, wire.StateIdle)
	}
}

// The heuristic tier must never raise the attention flag on its own.
// "No bytes for two seconds" is true of every `ls` in every shell, and
// the flag drives desktop notifications.
func TestHeuristicIdleDoesNotRaiseAttention(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e := mustCreate(t, r, wire.CreateSpec{Name: "shell"})

	r.noteOutput(e.ID)
	r.mu.Lock()
	prev := r.entries[e.ID].stateSnapshot()
	r.entries[e.ID].machine().Tick(time.Now().Add(time.Hour))
	r.announceStateLocked(r.entries[e.ID], prev)
	r.mu.Unlock()

	if r.Get(e.ID).Info().NeedsAttention {
		t.Error("a shell going quiet asked for the user's attention")
	}
}

// An agent-reported transition, by contrast, is exactly what the flag
// is for. Driven through the machine directly: the socket that carries
// these events arrives in phase 2.
func TestAgentReportedWaitingRaisesAttention(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e := mustCreate(t, r, wire.CreateSpec{Name: "agent"})

	ch, unsub := r.Subscribe()
	defer unsub()
	drain(ch)

	r.mu.Lock()
	ent := r.entries[e.ID]
	prev := ent.stateSnapshot()
	ent.machine().Apply(agentstate.Event{
		Kind:   agentstate.KindWaitingPermission,
		Source: wire.StateSourceHook,
		At:     time.Now(),
		Text:   "",
	})
	r.announceStateLocked(ent, prev)
	r.mu.Unlock()

	info := r.Get(e.ID).Info()
	if info.State != wire.StateWaitingPermission {
		t.Errorf("state = %q, want %q", info.State, wire.StateWaitingPermission)
	}
	if !info.NeedsAttention {
		t.Error("an agent blocked on a permission prompt did not ask for the user")
	}
	if info.StateSource != wire.StateSourceHook {
		t.Errorf("source = %q, want %q", info.StateSource, wire.StateSourceHook)
	}
}

// An agent session is off the heuristic tier entirely. Measured cause:
// an idle Claude Code session writes an ESC[?6n cursor-position query
// every 200ms forever — it renders nothing, but it means "no bytes for
// two seconds" never happens, so the session would read as permanently
// working and every bell would be overwritten by the next poll.
func TestAgentSessionIsOffTheHeuristicTier(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e := mustCreate(t, r, wire.CreateSpec{Name: "agent", Agent: "claude"})

	// The output hook is not even installed, so this is what the PTY
	// hammering away would do if it were.
	r.noteOutput(e.ID)
	if got := r.Get(e.ID).Info().State; got != wire.StateIdle {
		t.Errorf("state = %q after output; an agent must report nothing "+
			"until it can report the truth", got)
	}

	// The bell keeps doing exactly what it did before the state model:
	// it raises attention, and does not invent a state.
	r.noteBell(e.ID)
	info := r.Get(e.ID).Info()
	if !info.NeedsAttention {
		t.Error("a bell on an agent session must still ask for the user")
	}
	if info.State != wire.StateIdle {
		t.Errorf("state = %q after a bell on an agent, want none", info.State)
	}
}

// A shell is the one thing the heuristic tier is honest about: it stops
// writing when it is done.
func TestShellSessionIsOnTheHeuristicTier(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e := mustCreate(t, r, wire.CreateSpec{Name: "shell"})

	r.noteOutput(e.ID)
	if got := r.Get(e.ID).Info().State; got != wire.StateWorking {
		t.Errorf("state = %q, want %q", got, wire.StateWorking)
	}
	r.noteBell(e.ID)
	info := r.Get(e.ID).Info()
	if info.State != wire.StateWaitingInput || !info.NeedsAttention {
		t.Errorf("bell gave state=%q attention=%v, want waiting_input + attention",
			info.State, info.NeedsAttention)
	}
}

// The state is in-memory only. If it ever reached session.json, a
// daemon restart would resurrect a stale "working" for a session whose
// process is long gone.
func TestMetaFileUnchangedByState(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e := mustCreate(t, r, wire.CreateSpec{Name: "persisted"})

	path := filepath.Join(SessionsDir(r.stateDir), e.ID, "session.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read meta: %v", err)
	}

	r.mu.Lock()
	ent := r.entries[e.ID]
	ent.machine().Output(time.Now())
	ent.machine().Apply(agentstate.Event{
		Kind: agentstate.KindPrompt, Source: wire.StateSourceHook,
		At: time.Now(), Text: "something the agent was asked",
	})
	ent.machine().Bell(time.Now())
	ent.machine().Exit()
	r.persistEntryLoggedLocked(ent, "test")
	r.mu.Unlock()

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read meta: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("session.json changed after a full state tour:\n%s\nvs\n%s", before, after)
	}
}

// Restart replaces the machine, so a session that had been through
// every state comes back idle, on the heuristic tier, remembering
// nothing. Same code path as Revive and as a daemon restart.
func TestRestartStartsIdleHeuristicEmptyText(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := mustCreate(t, r, wire.CreateSpec{Name: "restarted"})

	r.mu.Lock()
	ent := r.entries[e.ID]
	ent.machine().Apply(agentstate.Event{
		Kind: agentstate.KindPrompt, Source: wire.StateSourceHook,
		At: time.Now(), Text: "port the parser",
	})
	r.mu.Unlock()
	if got := r.Get(e.ID).Info().LastPrompt; got == "" {
		t.Fatal("test setup: the prompt was not recorded")
	}

	if err := r.Restart(e.ID); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	info := r.Get(e.ID).Info()
	if info.LastPrompt != "" || info.LastSummary != "" {
		t.Errorf("restarted session remembers %q / %q", info.LastPrompt, info.LastSummary)
	}
	if info.StateSource != wire.StateSourceHeuristic {
		t.Errorf("source = %q, want the heuristic tier back", info.StateSource)
	}
}

// An entry that has never been observed reports the same thing as one
// that has just been created: idle, heuristic, no text. That is what
// lets Info() read the state without taking a lock to create a machine.
func TestUnobservedEntryReportsIdle(t *testing.T) {
	e := &Entry{ID: "never-started"}
	info := e.Info()
	if info.State != wire.StateIdle || info.StateSource != wire.StateSourceHeuristic {
		t.Errorf("state = %q/%q, want idle on the heuristic tier",
			info.State, info.StateSource)
	}
	if e.state != nil {
		t.Error("Info() created a machine; it must stay read-only")
	}
}
