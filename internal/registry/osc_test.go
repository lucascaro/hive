package registry

import (
	"context"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// OSC 7501 end to end (spec 517): a status report printed by the
// session's program must reach the state machine through the scanner in
// internal/session and the hook installed at each Entry.sess site. Like
// TestBellReachesAttentionThroughTheRealPTY, this starts at the PTY
// rather than halfway down the chain.

// say prints one 7501 report from the child. /bin/cat copies the line
// back with its real ESC and BEL bytes (the tty's own echo spells them
// ^[ and ^G, which the scanner correctly ignores).
func say(t *testing.T, w interface{ Write([]byte) (int, error) }, report string) {
	t.Helper()
	if _, err := w.Write([]byte("\x1b]7501;" + report + "\x07\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func sessionInfo(r *Registry, id string) (wire.SessionInfo, bool) {
	for _, s := range r.List() {
		if s.ID == id {
			return s, true
		}
	}
	return wire.SessionInfo{}, false
}

func waitState(t *testing.T, r *Registry, id, state, source string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s, ok := sessionInfo(r, id); ok && s.State == state && s.StateSource == source {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	s, _ := sessionInfo(r, id)
	t.Fatalf("state = %q/%q, want %q/%q", s.State, s.StateSource, state, source)
}

func TestOSCEscReachesIdleThroughTheRealPTY(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "esc", Shell: "/bin/cat"})

	// Claude's hooks drive the turn; Esc fires none.
	if err := r.ApplyAgentEvent(e.ID, wire.AgentEvent{SessionID: e.ID, Kind: wire.AgentEventPrompt,
		Source: wire.StateSourceHook, At: time.Now().Format(time.RFC3339Nano), Text: "go"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, r, e.ID, wire.StateWorking, wire.StateSourceHook)

	say(t, sess, "state=working:app=claude-code")
	say(t, sess, "state=idle:app=claude-code") // Esc
	waitState(t, r, e.ID, wire.StateIdle, wire.StateSourceHook)
}

func TestOSCQuestionReachesAttentionThroughTheRealPTY(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "ask", Shell: "/bin/cat"})
	listener, unsub := r.Subscribe()
	defer unsub()
	drain(listener)

	// No hook has spoken: osc is the only source. The long msg is the
	// shape Claude sends for a real question.
	say(t, sess, "state=working:app=claude-code")
	say(t, sess, "state=blocked:app=claude-code:kind=question:msg=UGljayByZWQgb3IgYmx1ZT8=")

	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev := <-listener:
			if ev.Session.ID == e.ID && ev.Kind == wire.SessionEventAttention && ev.Session.NeedsAttention {
				if ev.Session.StateSource != wire.StateSourceOSC {
					t.Errorf("attention came from %q, want osc", ev.Session.StateSource)
				}
				return
			}
		case <-deadline:
			s, _ := sessionInfo(r, e.ID)
			t.Fatalf("no attention from a blocked question; state %q/%q", s.State, s.StateSource)
		}
	}
}

// A session whose hook is fresh is not overridden by an osc wait.
func TestOSCDoesNotOverrideFreshHookThroughTheRealPTY(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "fresh", Shell: "/bin/cat"})
	if err := r.ApplyAgentEvent(e.ID, wire.AgentEvent{SessionID: e.ID, Kind: wire.AgentEventPrompt,
		Source: wire.StateSourceHook, At: time.Now().Format(time.RFC3339Nano), Text: "go"}); err != nil {
		t.Fatal(err)
	}
	say(t, sess, "state=blocked:kind=question")
	say(t, sess, "state=done") // the last report: if it were applied, the state would change
	time.Sleep(300 * time.Millisecond)
	s, _ := sessionInfo(r, e.ID)
	if s.State != wire.StateWorking || s.StateSource != wire.StateSourceHook {
		t.Errorf("state = %q/%q, want the hook's working untouched", s.State, s.StateSource)
	}
}

// Review round 1 (PR #520): an agent id is not an agent. The launcher
// sends the catalog's built-in "shell" for a plain shell, and that
// session must not answer an OSC 7501 query — on any spawn path.
func TestStatusQueryAnsweredForAgentsOnly(t *testing.T) {
	skipOnWindows(t)
	for _, c := range []struct {
		agent string
		want  bool
	}{
		{"", false}, {"shell", false}, {"claude", true}, {"codex", true}, {"my-custom-agent", true},
	} {
		if got := answersStatusQuery(c.agent); got != c.want {
			t.Errorf("answersStatusQuery(%q) = %v, want %v", c.agent, got, c.want)
		}

		// Revive/Restart path.
		r := freshRegistry(t)
		var opts session.Options
		r.applyAgentSpawn(&opts, c.agent)
		if opts.AnswerStatusQuery != c.want {
			t.Errorf("applyAgentSpawn(%q): AnswerStatusQuery = %v, want %v", c.agent, opts.AnswerStatusQuery, c.want)
		}

		// Create path, through the real spawn seam.
		var got bool
		restore := SetStartSessionForTest(func(o session.Options) (*session.Session, error) {
			got = o.AnswerStatusQuery
			return session.Start(session.Options{Shell: "/bin/cat", Cols: 80, Rows: 24})
		})
		e, err := r.Create(context.Background(), wire.CreateSpec{Name: "q", Agent: c.agent, Cmd: []string{"/bin/cat"}, Cols: 80, Rows: 24})
		restore()
		if err != nil {
			t.Fatalf("Create(%q): %v", c.agent, err)
		}
		if got != c.want {
			t.Errorf("Create(%q): AnswerStatusQuery = %v, want %v", c.agent, got, c.want)
		}
		_ = r.Kill(e.ID, true)
	}
}

// A burst of reports in one read is one broadcast, not one per report.
func TestOSCBurstIsOneBroadcast(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, _ := liveSession(t, r, wire.CreateSpec{Name: "burst"})
	ch, unsub := r.Subscribe()
	defer unsub()
	drain(ch)
	burst := make([]session.Status, 0, 40)
	for i := 0; i < 20; i++ {
		burst = append(burst, session.Status{State: "blocked", Kind: "question"}, session.Status{State: "working"})
	}
	burst = append(burst, session.Status{State: "done"})
	r.noteOSC(e.ID, burst)
	n := 0
	deadline := time.After(300 * time.Millisecond)
loop:
	for {
		select {
		case ev := <-ch:
			if ev.Session.ID == e.ID && ev.Kind == wire.SessionEventState {
				n++
			}
		case <-deadline:
			break loop
		}
	}
	if n != 1 {
		t.Errorf("state broadcasts = %d, want 1 for the burst", n)
	}
}
