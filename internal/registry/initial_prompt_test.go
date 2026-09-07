package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
			sep := ""
			if n := len(cmd); n > 0 {
				last = cmd[n-1]
				if n > 1 {
					sep = cmd[n-2]
				}
			}
			if got := last == "seed me"; got != tc.want {
				t.Errorf("argv = %v; prompt appended = %v, want %v", cmd, got, tc.want)
			}
			// Guarded by "--", or a prompt starting with "-" is a flag.
			if tc.want && sep != "--" {
				t.Errorf("argv = %v; want the prompt behind a -- separator", cmd)
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
	if got := deliveryFor(spec); got != promptNone {
		t.Errorf("deliveryFor = %v, want promptNone: raw argv from a client "+
			"that does not speak agent IDs is not a prompt box", got)
	}
}

// deliveryFor is the one decision about where an opening prompt goes,
// and its DEFAULT is the security-relevant half: anything not known to
// present a prompt box gets nothing typed at it.
func TestPromptDeliveryMatrix(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec wire.CreateSpec
		want promptDelivery
	}{
		{"claude takes argv", wire.CreateSpec{Agent: "claude", InitialPrompt: "p"}, promptArgv},
		{"pi takes argv", wire.CreateSpec{Agent: "pi", InitialPrompt: "p"}, promptArgv},
		{"codex is typed", wire.CreateSpec{Agent: "codex", InitialPrompt: "p"}, promptTyped},
		{"gemini is typed", wire.CreateSpec{Agent: "gemini", InitialPrompt: "p"}, promptTyped},
		{"copilot is typed", wire.CreateSpec{Agent: "copilot", InitialPrompt: "p"}, promptTyped},
		{"aider is typed", wire.CreateSpec{Agent: "aider", InitialPrompt: "p"}, promptTyped},
		// The whole reason the default is promptNone: a shell is not a
		// prompt box, it is a command interpreter.
		{"the shell agent gets nothing", wire.CreateSpec{Agent: "shell", InitialPrompt: "p"}, promptNone},
		{"an unknown agent gets nothing", wire.CreateSpec{Agent: "not-an-agent", InitialPrompt: "p"}, promptNone},
		{"no agent gets nothing", wire.CreateSpec{InitialPrompt: "p"}, promptNone},
		{"raw argv gets nothing", wire.CreateSpec{Agent: "claude", Cmd: []string{"x"}, InitialPrompt: "p"}, promptNone},
		{"no prompt is no delivery", wire.CreateSpec{Agent: "claude"}, promptNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := deliveryFor(tc.spec); got != tc.want {
				t.Errorf("deliveryFor = %v, want %v", got, tc.want)
			}
		})
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

// queuePrompt puts an entry into the state beginCreate would have left
// it in for a promptTyped agent, without spawning that agent's binary.
// Codex and friends are not installed on CI, and a delivery-mechanics
// test has no business depending on whether they are: WHICH agents get
// a typed prompt is deliveryFor's decision and is table-tested in
// TestPromptDeliveryMatrix.
func queuePrompt(r *Registry, id, prompt, ideaID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.entries[id]; ok {
		e.pendingPrompt = prompt
		e.ideaID = ideaID
	}
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

	e, sess := liveSession(t, r, wire.CreateSpec{Name: "seeded", ProjectID: p.ID})
	queuePrompt(r, e.ID, "fix the sidebar", idea.ID)

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
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "seeded"})
	queuePrompt(r, e.ID, "once", "")

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

// A session that goes away before its first idle edge never receives
// the prompt, and the idea it came from stays open — the note is not
// lost, and the user can start it again.
//
// Killed rather than left to exit on its own, and that is not a
// convenience: a child that exits by itself is not detected on Linux
// at all. internal/session's read loop closes Done() only when the PTY
// master read fails, and on Linux it never does — measured on the CI
// runner and again under docker golang:1.27.1, against origin/main
// with no part of this feature in the tree, for both an instant
// `/usr/bin/true` and a 0.3s sleeper. That is a pre-existing platform
// gap this feature neither introduces nor can paper over (it is filed
// separately); Kill reaches the same "gone before delivery" state
// through a path that works on every platform.
func TestPendingPromptDroppedWhenSessionGoesAway(t *testing.T) {
	skipOnWindows(t)
	r, p := ideaRegistry(t)
	idea, err := r.AddIdea(IdeaSpec{ProjectID: p.ID, Text: "never delivered"})
	if err != nil {
		t.Fatalf("AddIdea: %v", err)
	}
	e, _ := liveSession(t, r, wire.CreateSpec{Name: "doomed", ProjectID: p.ID})
	queuePrompt(r, e.ID, "never delivered", idea.ID)
	// Never sampled, so the session has not reached an idle edge and
	// the prompt is still pending.
	if got := pendingPromptOf(r, e.ID); got == "" {
		t.Fatal("prompt was already delivered before any idle edge")
	}

	if err := r.Kill(e.ID, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	// The entry is gone, so nothing can deliver the prompt later...
	r.mu.Lock()
	_, stillThere := r.entries[e.ID]
	r.mu.Unlock()
	if stillThere {
		t.Fatal("killed session still in the registry")
	}
	// ...and the idea was never claimed by a session that did no work.
	if got := ideaStatus(t, r, idea.ID).Status; got != wire.IdeaStatusOpen {
		t.Errorf("idea status = %q; a session that never received the "+
			"prompt must not claim the idea", got)
	}
	if got := ideaStatus(t, r, idea.ID).SessionID; got != "" {
		t.Errorf("idea session_id = %q, want it unset", got)
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

// The opening prompt is a trust boundary. It arrives on the wire, and
// the text behind it is authored by a user OR by an agent (`hive idea
// add` runs inside sessions), so it is never assumed safe. On the
// typed path it is written straight into a PTY, where a bare ESC is a
// sequence the terminal executes and a stray \r submits a half-formed
// turn.
func TestPromptSanitization(t *testing.T) {
	for _, tc := range []struct {
		name, in, wantArgv, wantTyped string
	}{
		{
			name:      "control characters are stripped on both paths",
			in:        "fix \x1b[31mthe\x07 grid\x00",
			wantArgv:  "fix [31mthe grid",
			wantTyped: "fix [31mthe grid",
		},
		{
			name: "a carriage return cannot submit an early turn",
			in:   "first\rsecond",
			// \r is a control character: gone before it can reach the
			// PTY as a submit.
			wantArgv:  "firstsecond",
			wantTyped: "firstsecond",
		},
		{
			name: "argv keeps the note's layout; the typed path flattens it",
			in:   "line one\nline two\tindented",
			// argv is not a terminal, so a newline is just a newline.
			wantArgv: "line one\nline two\tindented",
			// The PTY write appends \r, so an embedded newline would
			// submit the turn early and land the rest in the next one.
			wantTyped: "line one line two indented",
		},
		{
			name:      "DEL is not a printable character either",
			in:        "oops\x7f",
			wantArgv:  "oops",
			wantTyped: "oops",
		},
		{
			// Nothing left to deliver. finishCreate branches on the
			// empty result, so the idea links at create rather than
			// waiting forever for a delivery that cannot happen.
			name:      "a note of nothing but control characters empties",
			in:        "\x00\x01\x02",
			wantArgv:  "",
			wantTyped: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizePrompt(tc.in); got != tc.wantArgv {
				t.Errorf("sanitizePrompt = %q, want %q", got, tc.wantArgv)
			}
			if got := typedPrompt(tc.in); got != tc.wantTyped {
				t.Errorf("typedPrompt = %q, want %q", got, tc.wantTyped)
			}
		})
	}
}

// CreateSpec.InitialPrompt is its own entry point — it arrives on the
// wire and never has to have been an idea, so AddIdea's 4 KiB cap does
// not cover it.
func TestPromptIsBounded(t *testing.T) {
	long := strings.Repeat("x", wire.MaxIdeaText+500)
	if got := len(sanitizePrompt(long)); got != wire.MaxIdeaText {
		t.Errorf("len = %d, want %d", got, wire.MaxIdeaText)
	}
	// The cut lands on a rune boundary, so the agent never receives an
	// invalid UTF-8 tail. "é" is two bytes, so a byte-wise cut at the
	// cap is guaranteed to split one.
	multi := strings.Repeat("é", wire.MaxIdeaText)
	got := sanitizePrompt(multi)
	if len(got) > wire.MaxIdeaText {
		t.Errorf("len = %d, over the cap", len(got))
	}
	if !utf8.ValidString(got) {
		t.Error("truncation split a codepoint")
	}
}

// A kill that lands between the spawn and the link must not leave the
// idea pointing at a session id no client can resolve — an inbox row
// reading "in <gone>", with the idea out of the open list and no way
// back to it.
func TestIdeaNotLinkedToAKilledSession(t *testing.T) {
	skipOnWindows(t)
	r, p := ideaRegistry(t)
	idea, err := r.AddIdea(IdeaSpec{ProjectID: p.ID, Text: "raced"})
	if err != nil {
		t.Fatalf("AddIdea: %v", err)
	}
	e, _ := liveSession(t, r, wire.CreateSpec{Name: "raced", ProjectID: p.ID})
	if err := r.Kill(e.ID, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	// The state finishCreate's caller would reach after a kill landed
	// in the window between the spawn and the link.
	r.linkIdeaToSession(idea.ID, e.ID)

	got := ideaStatus(t, r, idea.ID)
	if got.Status != wire.IdeaStatusOpen || got.SessionID != "" {
		t.Errorf("idea = {status:%q session:%q}, want it untouched",
			got.Status, got.SessionID)
	}
}

// The regression test for a real command execution, not a theoretical
// one: before Def.TypedPrompt existed, the shell agent fell through to
// the typed path and `prompt + "\r"` was written into a bare shell —
// which ran it. `$(…)` in a note therefore executed, and notes are
// agent-authored too (ADD_IDEA is reachable on the session-mode
// socket), so one agent could plant text another user's shell ran.
//
// Sanitizing was never the fix. Stripping control characters does
// nothing when the receiver is a command interpreter; the fix is not
// typing at it at all.
func TestShellNeverReceivesATypedPrompt(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	marker := filepath.Join(t.TempDir(), "executed")
	e, sess := liveSession(t, r, wire.CreateSpec{
		Name: "shell", Agent: "shell", Shell: "/bin/sh",
		InitialPrompt: "An idea was captured. The idea: $(touch " + marker + ")",
	})

	// Nothing was even queued — the guard is at create, not at delivery.
	if got := pendingPromptOf(r, e.ID); got != "" {
		t.Fatalf("queued %q for a shell session", got)
	}

	// Drive the edge that WOULD have delivered it.
	now := time.Now()
	paint(t, e, sess, "\n")
	sample(r, e, now)
	sample(r, e, now.Add(agentstate.QuietAfter))

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("the shell executed the note: %s was created", marker)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// A session started from an idea that can receive no prompt still links
// the idea: there is no delivery to wait for, so holding the note open
// forever would strand it.
func TestIdeaLinksWhenThereIsNoDeliveryToWaitFor(t *testing.T) {
	skipOnWindows(t)
	// The shell agent, because it is the case that reaches a user: it
	// is the launcher's default selection, and it can receive no prompt
	// at all. (The other no-delivery route — a note that sanitizes away
	// to nothing — is covered by TestPromptSanitization; both land on an
	// empty pendingPrompt, which is what finishCreate branches on.)
	r, p := ideaRegistry(t)
	idea, err := r.AddIdea(IdeaSpec{ProjectID: p.ID, Text: "note"})
	if err != nil {
		t.Fatalf("AddIdea: %v", err)
	}
	e, _ := liveSession(t, r, wire.CreateSpec{
		Name: "s", ProjectID: p.ID, Agent: "shell", Shell: "/bin/sh",
		InitialPrompt: "An idea was captured. The idea: x", IdeaID: idea.ID,
	})
	waitFor(t, "the idea to be linked", func() bool {
		return ideaStatus(t, r, idea.ID).Status == wire.IdeaStatusStarted
	})
	if got := ideaStatus(t, r, idea.ID).SessionID; got != e.ID {
		t.Errorf("idea session_id = %q, want %q", got, e.ID)
	}
}

// The drop-on-exit clause in watchSessionExit. Reached by closing the
// PTY rather than by Kill: Kill removes the entry, so watchSessionExit
// returns at its `!ok` guard before the clause runs — which is why the
// branch had no coverage. Close makes the master read fail, which is
// the one exit signal internal/session has on every platform (see
// issue #379).
func TestPendingPromptClearedWhenTheSessionEnds(t *testing.T) {
	skipOnWindows(t)
	r, p := ideaRegistry(t)
	idea, err := r.AddIdea(IdeaSpec{ProjectID: p.ID, Text: "never delivered"})
	if err != nil {
		t.Fatalf("AddIdea: %v", err)
	}
	e, sess := liveSession(t, r, wire.CreateSpec{Name: "dying", ProjectID: p.ID})
	queuePrompt(r, e.ID, "seeded", idea.ID)
	if got := pendingPromptOf(r, e.ID); got == "" {
		t.Fatal("nothing queued to drop")
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	waitFor(t, "the pending prompt to be dropped", func() bool {
		return pendingPromptOf(r, e.ID) == ""
	})
	// The entry outlives the process here, so this is the real
	// assertion: the idea was never claimed by a session that did no
	// work with it.
	if got := ideaStatus(t, r, idea.ID).Status; got != wire.IdeaStatusOpen {
		t.Errorf("idea status = %q, want %q", got, wire.IdeaStatusOpen)
	}
}

// A prompt that begins with "-" must reach the agent as text, not as a
// flag. CreateSpec.InitialPrompt is a wire field: our own prompts start
// with a word, but nothing makes another client's do so.
func TestLeadingDashPromptIsNotAFlag(t *testing.T) {
	r := freshRegistry(t)
	cmd := r.resolveAgentCmd(wire.CreateSpec{
		Agent: "claude", InitialPrompt: "--dangerously-skip-permissions",
	}, "sid-1")
	sep := -1
	for i, a := range cmd {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 || sep != len(cmd)-2 || cmd[len(cmd)-1] != "--dangerously-skip-permissions" {
		t.Fatalf("argv = %v; want the prompt as the sole argument after --", cmd)
	}
}

// The idea and the session must belong to the same project. The
// launcher pins the idea's project, so a mismatch means a client sent
// an idea_id that does not go with this session — and idea_id is
// reachable by any client, not just ours.
func TestIdeaNotLinkedAcrossProjects(t *testing.T) {
	skipOnWindows(t)
	r, p := ideaRegistry(t)
	other, err := r.CreateProject(wire.CreateProjectReq{Name: "other", Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	idea, err := r.AddIdea(IdeaSpec{ProjectID: other.ID, Text: "belongs elsewhere"})
	if err != nil {
		t.Fatalf("AddIdea: %v", err)
	}
	e, _ := liveSession(t, r, wire.CreateSpec{Name: "s", ProjectID: p.ID})

	r.linkIdeaToSession(idea.ID, e.ID)

	got := ideaStatus(t, r, idea.ID)
	if got.Status != wire.IdeaStatusOpen || got.SessionID != "" {
		t.Errorf("idea = {status:%q session:%q}, want it untouched",
			got.Status, got.SessionID)
	}
}
