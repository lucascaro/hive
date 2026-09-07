package registry

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agentstate"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// resolveAgentCmd is where the opening prompt joins argv, and it is
// reached only from finishCreate. Restart and Revive rebuild argv
// through appendSpawnArgs instead — which never sees a CreateSpec — so
// a restarted session cannot replay the first turn.
func TestPositionalPromptIsAppendedOnlyForItsAgents(t *testing.T) {
	r := freshRegistry(t)
	for _, tc := range []struct {
		agent string
		want  bool
	}{
		{"claude", true},
		{"pi", true},
		{"codex", false},
		{"gemini", false},
		{"copilot", false},
		{"shell", false},
	} {
		t.Run(tc.agent, func(t *testing.T) {
			cmd := r.resolveAgentCmd(wire.CreateSpec{
				Agent: tc.agent, InitialPrompt: "seed me",
			}, "sid-1")
			last := ""
			if len(cmd) > 0 {
				last = cmd[len(cmd)-1]
			}
			if got := last == "seed me"; got != tc.want {
				t.Errorf("argv = %v; prompt appended = %v, want %v", cmd, got, tc.want)
			}
		})
	}
}

// An explicit Cmd is a raw argv from a client that does not speak agent
// IDs. We no more append a prompt to it than resolveAgentCmd injects
// SessionIDFlag into it — the typed path handles that session instead.
func TestPositionalPromptSkipsExplicitCmd(t *testing.T) {
	r := freshRegistry(t)
	spec := wire.CreateSpec{
		Agent: "claude", Cmd: []string{"claude", "--weird"}, InitialPrompt: "seed me",
	}
	if cmd := r.resolveAgentCmd(spec, "sid-1"); len(cmd) != 2 {
		t.Errorf("argv = %v, want the caller's two elements untouched", cmd)
	}
	if takesPositionalPrompt(spec) {
		t.Error("an explicit Cmd must fall to the typed path")
	}
}

// ptyText is everything the child has written to its PTY this run.
// `cat` echoes its input, so a prompt typed INTO the session shows up
// here — which is the only observation that proves the bytes reached
// the child rather than a buffer somewhere on the way.
func ptyText(sess *session.Session) string {
	var out string
	_ = sess.EmitAtomicReplay(func(b []byte) error {
		out = string(b)
		return nil
	})
	return out
}

func pendingPromptOf(r *Registry, id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[id]; ok {
		return e.pendingPrompt
	}
	return ""
}

func ideaStatus(t *testing.T, r *Registry, id string) wire.IdeaInfo {
	t.Helper()
	for _, i := range r.ListIdeas("") {
		if i.ID == id {
			return i
		}
	}
	t.Fatalf("idea %s is gone", id)
	return wire.IdeaInfo{}
}

// The typed path, end to end: the prompt lands on the first idle edge
// AFTER the session has been working, and the idea it came from is not
// claimed until then.
//
// `cat` echoes whatever is written to its PTY, so the delivered text
// shows up on the screen — which is the only observation that proves
// the bytes actually reached the child rather than a buffer.
func TestPendingPromptTypedOnIdleAfterWorking(t *testing.T) {
	skipOnWindows(t)
	r, p := ideaRegistry(t)
	idea, err := r.AddIdea(IdeaSpec{ProjectID: p.ID, Text: "fix the sidebar"})
	if err != nil {
		t.Fatalf("AddIdea: %v", err)
	}

	e, sess := liveSession(t, r, wire.CreateSpec{
		Name: "seeded", ProjectID: p.ID,
		InitialPrompt: "fix the sidebar", IdeaID: idea.ID,
	})

	// t=0. Every machine starts idle (attachSessionHooks), so a naive
	// "first time it is idle" predicate would fire here — before the
	// agent has drawn anything to type into.
	now := time.Now()
	sample(r, e, now)
	sample(r, e, now.Add(agentstate.QuietAfter))
	if got := pendingPromptOf(r, e.ID); got == "" {
		t.Fatal("prompt was delivered at t=0, before the session ever worked")
	}
	if got := ideaStatus(t, r, idea.ID).Status; got != wire.IdeaStatusOpen {
		t.Errorf("idea status = %q before delivery, want %q", got, wire.IdeaStatusOpen)
	}

	// Working, then quiet: the edge the delivery hangs off.
	paint(t, e, sess, "thinking\n")
	sample(r, e, now)
	if got := pendingPromptOf(r, e.ID); got == "" {
		t.Fatal("prompt was delivered on the working edge, not the idle one")
	}
	sample(r, e, now.Add(agentstate.QuietAfter))

	waitFor(t, "the prompt to reach the PTY", func() bool {
		return strings.Contains(ptyText(sess), "fix the sidebar")
	})
	if got := pendingPromptOf(r, e.ID); got != "" {
		t.Errorf("pendingPrompt = %q after delivery, want it cleared", got)
	}
	waitFor(t, "the idea to be linked", func() bool {
		return ideaStatus(t, r, idea.ID).Status == wire.IdeaStatusStarted
	})
	if got := ideaStatus(t, r, idea.ID).SessionID; got != e.ID {
		t.Errorf("idea session_id = %q, want %q", got, e.ID)
	}
}

// Exactly once. The prompt is cleared as it is handed over, so every
// later idle edge — and this session will have many — is a no-op.
func TestPendingPromptDeliveredOnlyOnce(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, sess := liveSession(t, r, wire.CreateSpec{
		Name: "seeded", InitialPrompt: "once",
	})

	now := time.Now()
	paint(t, e, sess, "thinking\n")
	sample(r, e, now)
	sample(r, e, now.Add(agentstate.QuietAfter))
	waitFor(t, "the first delivery", func() bool {
		return strings.Contains(ptyText(sess), "once")
	})
	// Counted, not compared to 1: the tty line discipline echoes the
	// write and `cat` then prints it again, so ONE delivery already
	// shows up twice. The invariant is that the number stops growing.
	delivered := strings.Count(ptyText(sess), "once")

	later := now.Add(2 * agentstate.QuietAfter)
	paint(t, e, sess, "more\n")
	sample(r, e, later)
	sample(r, e, later.Add(agentstate.QuietAfter))
	// Give a second delivery every chance to happen before denying it.
	time.Sleep(200 * time.Millisecond)
	if n := strings.Count(ptyText(sess), "once"); n != delivered {
		t.Errorf("prompt occurrences grew %d -> %d across a second idle edge", delivered, n)
	}
}

// A session that dies before its first idle edge takes the prompt with
// it. The idea stays open, so the note is not lost and the user can
// start it again.
func TestPendingPromptDroppedOnExit(t *testing.T) {
	skipOnWindows(t)
	r, p := ideaRegistry(t)
	idea, err := r.AddIdea(IdeaSpec{ProjectID: p.ID, Text: "never delivered"})
	if err != nil {
		t.Fatalf("AddIdea: %v", err)
	}
	e, err := r.Create(context.Background(), wire.CreateSpec{
		Name: "doomed", ProjectID: p.ID, Cols: 80, Rows: 24,
		Shell:         "/usr/bin/true",
		InitialPrompt: "never delivered", IdeaID: idea.ID,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	waitFor(t, "the session to exit", func() bool {
		return pendingPromptOf(r, e.ID) == ""
	})
	if got := ideaStatus(t, r, idea.ID).Status; got != wire.IdeaStatusOpen {
		t.Errorf("idea status = %q; a session that never received the "+
			"prompt must not claim the idea", got)
	}
}

// The argv path does not wait: the prompt is in the process's own
// command line, so the idea is linked as soon as the process exists.
func TestIdeaLinkedImmediatelyOnTheArgvPath(t *testing.T) {
	skipOnWindows(t)
	r, p := ideaRegistry(t)
	idea, err := r.AddIdea(IdeaSpec{ProjectID: p.ID, Text: "argv path"})
	if err != nil {
		t.Fatalf("AddIdea: %v", err)
	}
	// No InitialPrompt: the same branch, and it avoids spawning a real
	// agent binary just to observe the link.
	e, sess := liveSession(t, r, wire.CreateSpec{
		Name: "linked", ProjectID: p.ID, IdeaID: idea.ID,
	})
	_ = sess
	waitFor(t, "the idea to be linked", func() bool {
		return ideaStatus(t, r, idea.ID).Status == wire.IdeaStatusStarted
	})
	if got := ideaStatus(t, r, idea.ID).SessionID; got != e.ID {
		t.Errorf("idea session_id = %q, want %q", got, e.ID)
	}
}
