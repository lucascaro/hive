package registry

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/acp"
	"github.com/lucascaro/hive/internal/acp/acptest"
	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/agentstate"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// useFakeACP points every ACP spawn at the fake agent (this test binary
// re-executed) with its store in a temp dir, which it returns.
func useFakeACP(t *testing.T, flags ...string) string {
	t.Helper()
	skipOnWindows(t)
	dir := t.TempDir()
	prev := acpCommand
	acpCommand = func(agent.Def) ([]string, []string) {
		return []string{os.Args[0]}, append(acp.AdapterEnv(os.Environ(), ""), acptest.Env(dir, flags...)...)
	}
	t.Cleanup(func() { acpCommand = prev })
	return dir
}

func createACP(t *testing.T, r *Registry, spec wire.CreateSpec) *Entry {
	t.Helper()
	spec.Kind = wire.KindACP
	if spec.Agent == "" {
		spec.Agent = string(agent.IDClaude)
	}
	e, err := r.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("Create acp: %v", err)
	}
	t.Cleanup(func() { _ = r.Kill(e.ID, true) })
	return e
}

func info(r *Registry, id string) wire.SessionInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.entries[id].Info()
}

func transcriptTexts(t *testing.T, r *Registry, id string) []string {
	t.Helper()
	msg, err := r.AcpTranscript(id)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range msg.Items {
		if it.Text != "" {
			out = append(out, it.Kind+":"+it.Text)
		}
	}
	return out
}

func idleAfterTurn(r *Registry, id string) func() bool {
	return func() bool {
		in := info(r, id)
		return in.State == wire.StateWaitingInput && in.StateSource == wire.StateSourceACP
	}
}

func TestCreateACPPersistsKind(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	in := info(r, e.ID)
	if in.Kind != wire.KindACP || !in.Alive {
		t.Errorf("Info = kind %q alive %v, want acp and alive", in.Kind, in.Alive)
	}
	var meta MetaFile
	if err := readJSON(filepath.Join(SessionsDir(r.stateDir), e.ID, "session.json"), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Kind != wire.KindACP || meta.AgentSessionID == "" || !strings.HasPrefix(meta.AgentSessionID, "fake-") {
		t.Errorf("session.json = %+v, want kind acp and the ACP session id", meta)
	}
}

// Every session persisted before spec 496 has no kind; it must load as
// a terminal session and write nothing new.
func TestEmptyKindLoadsAsPTY(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, err := r.Create(context.Background(), wire.CreateSpec{Name: "p", Shell: "/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Kill(e.ID, true)
	b, _ := os.ReadFile(filepath.Join(SessionsDir(r.stateDir), e.ID, "session.json"))
	if strings.Contains(string(b), `"kind"`) {
		t.Errorf("terminal session.json mentions kind: %s", b)
	}
	if k := info(r, e.ID).Kind; k != "" {
		t.Errorf("terminal session Kind = %q, want empty (pty)", k)
	}
}

func TestCreateACPRejectsAgentWithoutSpec(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	for _, spec := range []wire.CreateSpec{
		{Kind: wire.KindACP, Agent: string(agent.IDAider)},
		{Kind: wire.KindACP},
		{Kind: wire.KindACP, Agent: string(agent.IDClaude), Cmd: []string{"x"}},
		{Kind: wire.KindACP, Agent: string(agent.IDClaude), ContinueConversation: true},
		{Kind: "bogus", Agent: string(agent.IDClaude)},
	} {
		if _, err := r.Create(context.Background(), spec); !errors.Is(err, ErrBadKind) {
			t.Errorf("Create(%+v) = %v, want ErrBadKind", spec, err)
		}
	}
	if n := len(r.List()); n != 0 {
		t.Errorf("rejected creates registered %d entries", n)
	}
}

func TestACPStateWorkingPermissionIdle(t *testing.T) {
	useFakeACP(t, acptest.FlagPermission)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.PromptACP(e.ID, "hello", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	var perm *wire.AcpPermission
	waitFor(t, "waiting_permission", func() bool {
		msg, _ := r.AcpTranscript(e.ID)
		perm = msg.Permission
		in := info(r, e.ID)
		return perm != nil && in.State == wire.StateWaitingPermission && in.StateSource == wire.StateSourceACP
	})
	if err := r.AnswerPermission(e.ID, perm.RequestID, "nope"); !errors.Is(err, ErrPermissionStale) {
		t.Errorf("answer with an unoffered option = %v, want ErrPermissionStale", err)
	}
	if err := r.AnswerPermission(e.ID, perm.RequestID+"0", "allow"); !errors.Is(err, ErrPermissionStale) {
		t.Errorf("answer to another request id = %v, want ErrPermissionStale", err)
	}
	if err := r.AnswerPermission(e.ID, perm.RequestID, "allow"); err != nil {
		t.Fatal(err)
	}
	// A second answer to the same request (another window) was not used,
	// and must say so — unless the first already cleared it.
	if err := r.AnswerPermission(e.ID, perm.RequestID, "reject"); !errors.Is(err, ErrPermissionStale) {
		t.Errorf("second answer = %v, want ErrPermissionStale", err)
	}
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	got := transcriptTexts(t, r, e.ID)
	want := []string{"user:hello", "agent:echo: hello [selected:allow]"}
	if !slices.Equal(got, want) {
		t.Errorf("transcript = %q, want %q", got, want)
	}
	if msg, _ := r.AcpTranscript(e.ID); msg.Permission != nil {
		t.Error("permission still pending after the turn ended")
	}
}

// A silent turn longer than HookStaleAfter is still working: the acp
// tier is not demoted by the ticker.
func TestACPNotStaleAfterHookStaleAfter(t *testing.T) {
	dir := useFakeACP(t, acptest.FlagBlock)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.PromptACP(e.ID, "slow", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.entries[e.ID].machine().Tick(time.Now().Add(10 * agentstate.HookStaleAfter))
	_, stale := r.entries[e.ID].machine().StaleAt()
	r.mu.Unlock()
	if in := info(r, e.ID); in.State != wire.StateWorking || in.StateSource != wire.StateSourceACP || stale {
		t.Errorf("after a long silence: %s/%s stale=%v, want working on acp, no deadline", in.State, in.StateSource, stale)
	}
	acptest.Release(dir)
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
}

func TestPromptWhileWorkingRejected(t *testing.T) {
	dir := useFakeACP(t, acptest.FlagBlock)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.PromptACP(e.ID, "one", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	if err := r.PromptACP(e.ID, "two", wire.OriginUser); !errors.Is(err, ErrACPBusy) {
		t.Errorf("second prompt = %v, want ErrACPBusy", err)
	}
	acptest.Release(dir)
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	prompts := 0
	for _, c := range acptest.Calls(dir) {
		if strings.HasPrefix(c, "session/prompt ") {
			prompts++
		}
	}
	if prompts != 1 {
		t.Errorf("agent received %d prompts, want exactly 1", prompts)
	}
}

func TestPromptRecordsOrigin(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.PromptACP(e.ID, "hi", "plugin:wf"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	msg, _ := r.AcpTranscript(e.ID)
	if len(msg.Items) == 0 || msg.Items[0].Kind != wire.AcpItemUser || msg.Items[0].Origin != "plugin:wf" {
		t.Errorf("first item = %+v, want the user turn with origin plugin:wf", msg.Items)
	}
	if err := r.PromptACP(e.ID, "", wire.OriginUser); err == nil {
		t.Error("empty prompt accepted")
	}
}

// The opening prompt of an ACP create is its first turn.
func TestCreateACPSendsInitialPrompt(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a", InitialPrompt: "start here"})
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	if got := transcriptTexts(t, r, e.ID); len(got) != 2 || got[0] != "user:start here" {
		t.Errorf("transcript = %q, want the opening prompt as the first turn", got)
	}
	if in := info(r, e.ID); in.PendingPrompt != "" {
		t.Errorf("PendingPrompt = %q, want none for an ACP session", in.PendingPrompt)
	}
}

func reviveAndCheck(t *testing.T, r *Registry, id, dir string, do func() error) {
	t.Helper()
	r.mu.Lock()
	sid := r.entries[id].AgentSessionID
	r.mu.Unlock()
	if err := do(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "alive", func() bool { return info(r, id).Alive })
	got := transcriptTexts(t, r, id)
	want := []string{"user:one", "agent:echo: one"}
	if !slices.Equal(got, want) {
		t.Errorf("transcript after reload = %q, want %q", got, want)
	}
	msg, _ := r.AcpTranscript(id)
	if msg.Items[0].Origin != wire.OriginReplayed {
		t.Errorf("replayed user turn origin = %q, want replayed", msg.Items[0].Origin)
	}
	// Reopened the same conversation, never a new one.
	var loads, news int
	for _, c := range acptest.Calls(dir) {
		switch {
		case c == "session/load "+sid:
			loads++
		case strings.HasPrefix(c, "session/new"):
			news++
		}
	}
	if loads != 1 || news != 1 {
		t.Errorf("calls = %q, want one session/new (create) and one session/load", acptest.Calls(dir))
	}
	if err := r.PromptACP(id, "two", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "second turn", func() bool { return len(transcriptTexts(t, r, id)) == 4 })
}

func TestRestartACPReloads(t *testing.T) {
	dir := useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	r.PromptACP(e.ID, "one", wire.OriginUser)
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	reviveAndCheck(t, r, e.ID, dir, func() error { return r.Restart(e.ID) })
}

// A daemon restart: a second registry over the same state dir revives
// the entry over session/load, with the transcript replayed.
func TestReviveACPUsesSessionLoadAndReplaysTranscript(t *testing.T) {
	dir := useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	r.PromptACP(e.ID, "one", wire.OriginUser)
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	stateDir := r.stateDir
	r.Close()

	r2, err := Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r2.Close() })
	if k := info(r2, e.ID).Kind; k != wire.KindACP {
		t.Fatalf("reloaded kind = %q", k)
	}
	reviveAndCheck(t, r2, e.ID, dir, func() error {
		_, err := r2.ReviveWithPhase(e.ID, session.Options{})
		return err
	})
}

func TestRestoreACP(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.Kill(e.ID, true); err != nil {
		t.Fatal(err)
	}
	restored, _, err := r.Restore(e.ID, session.Options{})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "alive", func() bool { return info(r, restored.ID).Alive })
	if k := info(r, restored.ID).Kind; k != wire.KindACP {
		t.Errorf("restored kind = %q, want acp", k)
	}
}

func TestHookEventIgnoredForACP(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.ApplyAgentEvent(e.ID, wire.AgentEvent{Kind: wire.AgentEventWaitingPermission, Source: wire.StateSourceHook}); err != nil {
		t.Fatal(err)
	}
	if in := info(r, e.ID); in.State == wire.StateWaitingPermission {
		t.Error("a hook event moved an ACP session's state")
	}
}

func TestACPExitRecordsDeadSession(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	r.mu.Lock()
	a := r.entries[e.ID].acp.agent
	r.mu.Unlock()
	a.Close() // the adapter dies under the registry
	waitFor(t, "dead", func() bool { return !info(r, e.ID).Alive })
	if in := info(r, e.ID); in.State != wire.StateExited {
		t.Errorf("state after adapter exit = %q, want exited", in.State)
	}
	if err := r.PromptACP(e.ID, "x", wire.OriginUser); !errors.Is(err, ErrNoLiveSession) {
		t.Errorf("prompt to a dead ACP session = %v, want ErrNoLiveSession", err)
	}
}

func TestACPOpsRejectTerminalSession(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, err := r.Create(context.Background(), wire.CreateSpec{Name: "p", Shell: "/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Kill(e.ID, true)
	if err := r.PromptACP(e.ID, "x", wire.OriginUser); !errors.Is(err, ErrNotACP) {
		t.Errorf("PromptACP on pty = %v", err)
	}
	if _, err := r.AcpTranscript(e.ID); !errors.Is(err, ErrNotACP) {
		t.Errorf("AcpTranscript on pty = %v", err)
	}
}

// Every transcript change reaches subscribers, in order, as deltas on
// one epoch after the reset that opened it.
func TestSubscribeACPDeltas(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	ch, unsub := r.SubscribeACP()
	defer unsub()
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	r.PromptACP(e.ID, "hi", wire.OriginUser)
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	var msgs []wire.AcpTranscriptMsg
	for len(ch) > 0 {
		msgs = append(msgs, <-ch)
	}
	if len(msgs) < 3 || !msgs[0].Reset {
		b, _ := json.Marshal(msgs)
		t.Fatalf("messages = %s, want a reset then deltas", b)
	}
	for _, m := range msgs {
		if m.Epoch != msgs[0].Epoch || m.SessionID != e.ID {
			t.Errorf("message %+v off the opening epoch %d", m, msgs[0].Epoch)
		}
	}
}

// An ACP session created in a worktree is bound to it, as a terminal
// session is: revive runs there, and kill disposes of it.
func TestCreateACPRecordsWorktree(t *testing.T) {
	useFakeACP(t)
	r, p := freshRegistryWithProject(t)
	e := createACP(t, r, wire.CreateSpec{Name: "w", ProjectID: p.ID, UseWorktree: true})
	in := info(r, e.ID)
	if in.WorktreePath == "" || in.WorktreeBranch == "" {
		t.Fatalf("ACP entry has no worktree: %+v", in)
	}
	var meta MetaFile
	if err := readJSON(filepath.Join(SessionsDir(r.stateDir), e.ID, "session.json"), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.WorktreePath != in.WorktreePath {
		t.Errorf("persisted worktree = %q, want %q", meta.WorktreePath, in.WorktreePath)
	}
}

// A real adapter's tool calls (array content) reach the transcript and
// the state machine's activity.
func TestACPToolCallsReachTranscriptAndActivity(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	r.PromptACP(e.ID, "go", wire.OriginUser)
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	msg, _ := r.AcpTranscript(e.ID)
	var tool *wire.AcpItem
	for i := range msg.Items {
		if msg.Items[i].Kind == wire.AcpItemTool {
			tool = &msg.Items[i]
		}
	}
	if tool == nil || tool.Status != "completed" || tool.Title != "Read file" {
		t.Fatalf("tool item = %+v, want the completed Read file call", tool)
	}
	act, err := r.ActivitySnapshot(e.ID)
	if err != nil || len(act.Events) == 0 {
		t.Errorf("activity = %+v, %v; want the tool call recorded", act, err)
	}
}

// A reload replays history without one broadcast per chunk: a long
// history would overflow a listener. Subscribers get one reset carrying
// the whole replayed transcript instead.
func TestReplayBroadcastsOneReset(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	for _, p := range []string{"one", "two", "three"} {
		r.PromptACP(e.ID, p, wire.OriginUser)
		waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	}
	ch, unsub := r.SubscribeACP()
	defer unsub()
	if err := r.Restart(e.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "alive", func() bool { return info(r, e.ID).Alive })
	var deltas, resets int
	var last wire.AcpTranscriptMsg
	for len(ch) > 0 {
		m := <-ch
		if m.Reset {
			resets++
			last = m
		} else if len(m.Items) > 0 {
			deltas++
		}
	}
	if deltas != 0 {
		t.Errorf("replay sent %d item deltas, want none", deltas)
	}
	if n := len(last.Items); n != 6 {
		t.Errorf("final reset carries %d items, want the 6 replayed (resets seen: %d)", n, resets)
	}
}

// A permission request id must name one request of one adapter process:
// a late answer to the previous process's request must not satisfy the
// new process's request, even though both counters start at one.
func TestStalePermissionIDRefusedAfterRestart(t *testing.T) {
	useFakeACP(t, acptest.FlagPermission)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	pending := func() *wire.AcpPermission {
		var p *wire.AcpPermission
		waitFor(t, "permission request", func() bool {
			msg, _ := r.AcpTranscript(e.ID)
			p = msg.Permission
			return p != nil
		})
		return p
	}
	r.PromptACP(e.ID, "one", wire.OriginUser)
	old := pending()
	if err := r.Restart(e.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "alive", func() bool { return info(r, e.ID).Alive })
	if err := r.PromptACP(e.ID, "two", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	cur := pending()
	if err := r.AnswerPermission(e.ID, old.RequestID, "allow"); !errors.Is(err, ErrPermissionStale) {
		t.Fatalf("answer with the previous adapter's id %q (current %q) = %v, want ErrPermissionStale", old.RequestID, cur.RequestID, err)
	}
	if err := r.AnswerPermission(e.ID, cur.RequestID, "allow"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
}

// Every startACP failure leaves a dead session with a reason, never a
// live-looking one or a hang.
func TestStartACPFailuresLeaveDeadSessionWithReason(t *testing.T) {
	expectDead := func(t *testing.T, r *Registry, id, want string) {
		t.Helper()
		waitFor(t, "dead", func() bool { return !info(r, id).Alive })
		if got := info(r, id).LastError; !strings.Contains(got, want) {
			t.Errorf("LastError = %q, want it to mention %q", got, want)
		}
	}

	t.Run("spawn fails", func(t *testing.T) {
		skipOnWindows(t)
		prev := acpCommand
		acpCommand = func(agent.Def) ([]string, []string) {
			return []string{filepath.Join(t.TempDir(), "no-such-adapter")}, os.Environ()
		}
		t.Cleanup(func() { acpCommand = prev })
		r := freshRegistry(t)
		e, err := r.Create(context.Background(), wire.CreateSpec{Name: "a", Kind: wire.KindACP, Agent: string(agent.IDClaude)})
		if err == nil {
			t.Fatal("Create succeeded with a missing adapter")
		}
		expectDead(t, r, e.ID, "no-such-adapter")
	})

	t.Run("load of an unknown conversation", func(t *testing.T) {
		useFakeACP(t)
		r := freshRegistry(t)
		e := createACP(t, r, wire.CreateSpec{Name: "a"})
		r.mu.Lock()
		r.entries[e.ID].AgentSessionID = "fake-gone"
		r.mu.Unlock()
		if err := r.Restart(e.ID); err == nil {
			t.Fatal("Restart reloading an unknown conversation succeeded")
		}
		expectDead(t, r, e.ID, "session/load")
	})

	t.Run("adapter cannot load sessions", func(t *testing.T) {
		useFakeACP(t, acptest.FlagNoLoad)
		r := freshRegistry(t)
		e := createACP(t, r, wire.CreateSpec{Name: "a"})
		if err := r.Restart(e.ID); err == nil {
			t.Fatal("Restart succeeded against an adapter without loadSession")
		}
		expectDead(t, r, e.ID, "loadSession")
	})
}

// GET_ACP_TRANSCRIPT's snapshot is queued on the caller's own listener,
// so a client that applies the stream in order — the reset, then every
// delta after it — ends with exactly the daemon's transcript, even when
// the snapshot is taken mid-turn while chunks stream.
func TestSendAcpTranscriptOrderedWithDeltas(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	ch, unsub := r.SubscribeACP()
	defer unsub()
	if err := r.PromptACP(e.ID, "hello there", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	if err := r.SendAcpTranscript(e.ID, ch); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "turn idle", idleAfterTurn(r, e.ID))

	var folded []wire.AcpItem
	seenReset := false
	for drained := false; !drained; {
		select {
		case msg := <-ch:
			if msg.SessionID != e.ID {
				continue
			}
			if msg.Reset {
				seenReset, folded = true, slices.Clone(msg.Items)
				continue
			}
			if !seenReset {
				continue // before the snapshot: already in it
			}
			for _, it := range msg.Items {
				i := slices.IndexFunc(folded, func(x wire.AcpItem) bool { return x.ID == it.ID })
				switch {
				case i < 0:
					folded = append(folded, it)
				case it.Append:
					folded[i].Text += it.Text
				default:
					folded[i] = it
				}
			}
		default:
			drained = true
		}
	}
	if !seenReset {
		t.Fatal("SendAcpTranscript queued no reset on the listener")
	}
	want, _ := r.AcpTranscript(e.ID)
	got := make([]string, len(folded))
	for i, it := range folded {
		got[i] = it.Kind + ":" + it.Text + ":" + it.Status
	}
	exp := make([]string, len(want.Items))
	for i, it := range want.Items {
		exp[i] = it.Kind + ":" + it.Text + ":" + it.Status
	}
	if !slices.Equal(got, exp) {
		t.Errorf("reset + later deltas = %q, want the transcript %q", got, exp)
	}
}

func TestSendAcpTranscriptErrorsAndUnsubscribed(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	ch, unsub := r.SubscribeACP()
	unsub() // closed: sending on it would panic
	if err := r.SendAcpTranscript(e.ID, ch); err != nil {
		t.Errorf("unsubscribed listener: err %v, want nil and nothing sent", err)
	}
	if err := r.SendAcpTranscript("nope", nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: err %v, want ErrNotFound", err)
	}
}

// A listener that stops reading is dropped once its buffer is full: its
// channel is closed (so the daemon hangs up and the client reconnects
// and refetches) and it gets nothing more.
func TestACPSlowListenerDropped(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	ch, unsub := r.SubscribeACP()
	defer unsub()
	for i := 0; i <= cap(ch); i++ {
		if err := r.SendAcpTranscript(e.ID, ch); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	for range ch { // ends only if the channel was closed
		n++
	}
	if n != cap(ch) {
		t.Errorf("got %d messages before the close, want the buffer's %d", n, cap(ch))
	}
	r.mu.Lock()
	_, still := r.acpListeners[ch]
	r.mu.Unlock()
	if still {
		t.Error("slow listener still subscribed")
	}
}
