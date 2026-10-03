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
