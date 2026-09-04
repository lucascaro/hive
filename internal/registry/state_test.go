package registry

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agentstate"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// liveSession creates a session running `cat` — which echoes whatever
// is written to its PTY, so a test can put arbitrary text on the screen
// — and returns the entry and the live session.
func liveSession(t *testing.T, r *Registry, spec wire.CreateSpec) (*Entry, *session.Session) {
	t.Helper()
	spec.Cols, spec.Rows = 80, 24
	if len(spec.Cmd) == 0 {
		spec.Cmd = []string{"cat"}
	}
	e, err := r.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	r.mu.Lock()
	sess := r.entries[e.ID].sess
	ent := r.entries[e.ID]
	r.mu.Unlock()
	if sess == nil {
		t.Fatal("created session has no live process")
	}
	return ent, sess
}

// paint writes text and waits for it to reach the VT, so the next
// sample sees a changed screen rather than racing the PTY.
func paint(t *testing.T, e *Entry, sess *session.Session, text string) {
	t.Helper()
	before := sess.ScreenDigest()
	if _, err := sess.Write([]byte(text)); err != nil {
		t.Fatalf("write: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if sess.ScreenDigest() != before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("screen never changed after writing %q", text)
}

// sample drives one tick of the state sampler at a chosen time.
func sample(r *Registry, e *Entry, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sampleStateLocked(e, now)
}

// The measurement behind the whole design: a session can write
// continuously without changing anything the user sees. An idle Claude
// Code emits ESC[?6n every 200ms forever. Those bytes must not read as
// work, or the session is pinned to "working" and its quiet timer never
// fires — which is exactly what shipped and had to be fixed.
//
// The child emits the queries itself rather than the test writing them
// into the PTY: an inbound write is echoed by the line discipline as
// visible text, which changes the screen and would make this pass or
// fail for a reason that has nothing to do with the rule under test.
func TestTerminalQueriesAreNotWork(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, _ := liveSession(t, r, wire.CreateSpec{
		Name: "poller", Agent: "claude",
		Cmd: []string{"sh", "-c",
			`printf 'hello\n'; i=0; while [ $i -lt 200 ]; do printf '\033[?6n'; sleep 0.05; i=$((i+1)); done`},
	})

	// Wait for the one visible thing the child prints.
	r.mu.Lock()
	sess := r.entries[e.ID].sess
	r.mu.Unlock()
	deadline := time.Now().Add(10 * time.Second)
	start := sess.ScreenDigest()
	for time.Now().Before(deadline) && sess.ScreenDigest() == start {
		time.Sleep(10 * time.Millisecond)
	}

	now := time.Now()
	sample(r, e, now)
	if got := r.Get(e.ID).Info().State; got != wire.StateWorking {
		t.Fatalf("state = %q after real output, want %q", got, wire.StateWorking)
	}

	// The child is now doing nothing but polling the terminal, at five
	// times the rate the quiet window allows. Let real time pass so the
	// queries genuinely stream through the VT, then sample past the
	// window.
	time.Sleep(time.Second)
	sample(r, e, now.Add(agentstate.QuietAfter))

	if got := r.Get(e.ID).Info().State; got != wire.StateIdle {
		t.Errorf("state = %q; a session that only asked the terminal "+
			"questions must go idle, whatever its byte rate", got)
	}
}

// The counterpart: text on the screen is work, for an agent as much as
// a shell. Both tiers, because the reported bug was that agents got
// nothing at all.
func TestVisibleOutputIsWork(t *testing.T) {
	skipOnWindows(t)
	for _, agent := range []string{"", "claude"} {
		name := "shell"
		if agent != "" {
			name = agent
		}
		t.Run(name, func(t *testing.T) {
			r := freshRegistry(t)
			e, sess := liveSession(t, r, wire.CreateSpec{Name: name, Agent: agent})

			now := time.Now()
			sample(r, e, now)
			paint(t, e, sess, "working on it\n")
			sample(r, e, now)
			if got := r.Get(e.ID).Info().State; got != wire.StateWorking {
				t.Fatalf("state = %q, want %q", got, wire.StateWorking)
			}

			// Nothing more painted: the quiet window ends the turn.
			sample(r, e, now.Add(agentstate.QuietAfter))
			if got := r.Get(e.ID).Info().State; got != wire.StateIdle {
				t.Errorf("state = %q after %s of a still screen, want %q",
					got, agentstate.QuietAfter, wire.StateIdle)
			}
		})
	}
}

// A sample that sees an unchanged screen must broadcast nothing. This
// runs for every session every 500ms, so a stray broadcast here is a
// broadcast to every connected client, forever.
func TestUnchangedScreenBroadcastsNothing(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "still"})

	now := time.Now()
	paint(t, e, sess, "settled\n")
	sample(r, e, now)
	sample(r, e, now.Add(agentstate.QuietAfter))

	ch, unsub := r.Subscribe()
	defer unsub()
	drain(ch)
	for i := 0; i < 20; i++ {
		sample(r, e, now.Add(agentstate.QuietAfter+time.Duration(i)*time.Second))
	}
	select {
	case ev := <-ch:
		t.Fatalf("a still screen broadcast %s", ev.Kind)
	default:
	}
}

// The quiet timer is driven by a registry-wide ticker. Asserting
// through it rather than calling the sampler is the point: a sampler
// that is correct but never ticked leaves every session stuck.
func TestQuietGoesIdle(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "quiet"})

	ch, unsub := r.Subscribe()
	defer unsub()
	drain(ch)
	paint(t, e, sess, "one burst\n")

	deadline := time.After(agentstate.QuietAfter + 10*time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Kind == wire.SessionEventState && ev.Session.ID == e.ID &&
				ev.Session.State == wire.StateIdle {
				return
			}
		case <-deadline:
			t.Fatalf("session never went idle after %s of a still screen",
				agentstate.QuietAfter)
		}
	}
}

// A bell puts a session into waiting_input, and only the client
// reporting that the user looked takes it out again. Redrawing must
// not: that was the reported regression, where an agent rang and then
// painted over its own request for attention.
func TestBellWaitsUntilTheUserLooks(t *testing.T) {
	skipOnWindows(t)
	for _, agent := range []string{"", "claude"} {
		name := "shell"
		if agent != "" {
			name = agent
		}
		t.Run(name, func(t *testing.T) {
			r := freshRegistry(t)
			e, sess := liveSession(t, r, wire.CreateSpec{Name: name, Agent: agent})

			r.noteBell(e.ID)
			if got := r.Get(e.ID).Info().State; got != wire.StateWaitingInput {
				t.Fatalf("state = %q, want %q", got, wire.StateWaitingInput)
			}

			// The agent keeps painting. The request must survive it.
			now := time.Now()
			paint(t, e, sess, "still thinking\n")
			sample(r, e, now)
			info := r.Get(e.ID).Info()
			if info.State != wire.StateWaitingInput {
				t.Errorf("state = %q after a redraw; the bell was buried", info.State)
			}
			if !info.NeedsAttention {
				t.Error("NeedsAttention was lost")
			}

			if err := r.SetAttention(e.ID, false); err != nil {
				t.Fatalf("SetAttention: %v", err)
			}
			if got := r.Get(e.ID).Info().State; got != wire.StateIdle {
				t.Errorf("state = %q after the user looked, want %q", got, wire.StateIdle)
			}
		})
	}
}

// The heuristic tier must never raise the attention flag on its own.
// "The screen stopped changing" is true of every finished command, and
// the flag drives desktop notifications.
func TestHeuristicIdleDoesNotRaiseAttention(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "shell"})

	now := time.Now()
	paint(t, e, sess, "ls output\n")
	sample(r, e, now)
	sample(r, e, now.Add(agentstate.QuietAfter))

	if r.Get(e.ID).Info().NeedsAttention {
		t.Error("a session going quiet asked for the user's attention")
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

// End to end through a real PTY, for both tiers: a BEL in the output
// stream must reach NeedsAttention and broadcast an attention event.
//
// This is the test that was missing. The earlier ones call noteBell
// directly, which skips the whole chain the user actually depends on —
// the bell scanner in internal/session, the hook installed at each
// Entry.sess assignment site, and the registry's decision about which
// of those to install. A regression anywhere in that chain, for one
// tier and not the other, is invisible to a test that starts halfway
// along it.
//
// `cat` echoes what is written to the PTY straight back out, so writing
// a BEL byte produces one in the output stream exactly as a real
// program ringing would. Agent is set independently of Cmd, so the
// agent-flagged case runs the same harmless process.
func TestBellReachesAttentionThroughTheRealPTY(t *testing.T) {
	skipOnWindows(t)
	for _, tc := range []struct {
		name  string
		agent string
	}{
		{"shell", ""},
		{"agent", "claude"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := freshRegistry(t)
			e, err := r.Create(context.Background(), wire.CreateSpec{
				Name: "bell-" + tc.name, Cols: 80, Rows: 24,
				Cmd: []string{"cat"}, Agent: tc.agent,
			})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if got := r.Get(e.ID).Agent; got != tc.agent {
				t.Fatalf("entry agent = %q, want %q — the tier split reads this", got, tc.agent)
			}

			listener, unsub := r.Subscribe()
			defer unsub()
			drain(listener)

			r.mu.Lock()
			sess := r.entries[e.ID].sess
			r.mu.Unlock()
			if sess == nil {
				t.Fatal("created session has no live process")
			}

			// Retried for the same reason the title test retries: a
			// single early write can land before the child has exec'd.
			stop := make(chan struct{})
			defer close(stop)
			go func() {
				for {
					select {
					case <-stop:
						return
					default:
					}
					// The newline matters: `cat` is line buffered, so
					// without it the only thing on the output stream is
					// the tty's own "^G" echo — two printable bytes, not
					// a bell. A test that writes a bare \a passes nothing
					// through the scanner and fails for the wrong reason.
					_, _ = sess.Write([]byte("\a\n"))
					time.Sleep(200 * time.Millisecond)
				}
			}()

			deadline := time.After(20 * time.Second)
			for {
				select {
				case ev := <-listener:
					if ev.Session.ID != e.ID {
						continue
					}
					if ev.Kind == wire.SessionEventAttention && ev.Session.NeedsAttention {
						return
					}
				case <-deadline:
					t.Fatalf("no attention event after 20s of bells (agent=%q); "+
						"NeedsAttention is now %v",
						tc.agent, r.Get(e.ID).Info().NeedsAttention)
				}
			}
		})
	}
}
