package registry

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/lucascaro/hive/internal/acp/acptest"
	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// Takeover and hand-back (spec 496, criterion 6): a session moves
// between ACP and a terminal in the same conversation.

// fakeCLI stands in for the agent's own CLI during a takeover: it
// records each PTY argv and, like a user typing in the terminal, adds
// turn to the conversation the fake ACP agent keeps in dir, then runs a
// benign process as the terminal.
type fakeCLI struct {
	mu    sync.Mutex
	argvs [][]string
	// failFrom, when above 0, makes the failFrom-th launch and every one
	// after it fail, as a CLI missing from PATH would.
	failFrom int
}

func useFakeCLI(t *testing.T, dir string, turn *acptest.Turn) *fakeCLI {
	t.Helper()
	f := &fakeCLI{}
	t.Cleanup(SetStartSessionForTest(func(opts session.Options) (*session.Session, error) {
		f.mu.Lock()
		f.argvs = append(f.argvs, opts.Cmd)
		fail := f.failFrom > 0 && len(f.argvs) >= f.failFrom
		f.mu.Unlock()
		if fail {
			return nil, errors.New("fake CLI: not found")
		}
		if turn != nil && len(opts.Cmd) > 2 {
			if err := acptest.AppendTurn(dir, opts.Cmd[2], *turn); err != nil {
				return nil, err
			}
		}
		opts.Cmd = []string{"sleep", "60"}
		return session.Start(opts)
	}))
	return f
}

func (f *fakeCLI) calls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.argvs)
}

// transcriptOK makes the takeover precheck find (ok) or miss the CLI's
// on-disk transcript, and Claude's ResumeArgs agree with it.
func transcriptOK(t *testing.T, ok bool) {
	t.Helper()
	prev := takeoverTranscriptExists
	takeoverTranscriptExists = func(agent.Def, string, string) bool { return ok }
	t.Cleanup(func() { takeoverTranscriptExists = prev })
	t.Cleanup(agent.SetClaudeSessionExistsForTest(func(string, string) bool { return ok }))
}

func persistedKind(t *testing.T, r *Registry, id string) string {
	t.Helper()
	var meta MetaFile
	if err := readJSON(filepath.Join(SessionsDir(r.stateDir), id, "session.json"), &meta); err != nil {
		t.Fatal(err)
	}
	return meta.Kind
}

func agentSessionID(r *Registry, id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.entries[id].AgentSessionID
}

// The automated no-lost-turn proof: two ACP turns, a third typed in the
// terminal during the takeover, and after the hand-back Hive's own
// transcript holds all three exactly once, reloaded (not restarted) in
// the original conversation.
func TestTakeoverHandBackLosesNoTurn(t *testing.T) {
	dir := useFakeACP(t)
	transcriptOK(t, true)
	cli := useFakeCLI(t, dir, &acptest.Turn{User: "three", Agent: "echo: three"})
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	for i, text := range []string{"one", "two"} {
		if err := r.PromptACP(e.ID, text, wire.OriginUser); err != nil {
			t.Fatal(err)
		}
		n := 2 * (i + 1)
		waitFor(t, "turn "+text, func() bool { return len(transcriptTexts(t, r, e.ID)) == n && idleAfterTurn(r, e.ID)() })
	}
	sid := agentSessionID(r, e.ID)
	before, _ := r.AcpTranscript(e.ID)

	if err := r.SetKind(e.ID, wire.KindPTY); err != nil {
		t.Fatalf("take over: %v", err)
	}
	waitFor(t, "terminal alive", func() bool { return info(r, e.ID).Alive })
	if in := info(r, e.ID); in.Kind != "" || persistedKind(t, r, e.ID) != "" {
		t.Fatalf("after takeover kind = %q (persisted %q), want a terminal", in.Kind, persistedKind(t, r, e.ID))
	}
	if got := cli.calls(); len(got) != 1 || !slices.Equal(got[0][:3], []string{"claude", "--resume", sid}) {
		t.Fatalf("terminal argv = %q, want claude --resume %s", got, sid)
	}

	if err := r.SetKind(e.ID, wire.KindACP); err != nil {
		t.Fatalf("hand back: %v", err)
	}
	waitFor(t, "acp alive", func() bool { return info(r, e.ID).Alive && info(r, e.ID).Kind == wire.KindACP })
	want := []string{"user:one", "agent:echo: one", "user:two", "agent:echo: two", "user:three", "agent:echo: three"}
	if got := transcriptTexts(t, r, e.ID); !slices.Equal(got, want) {
		t.Errorf("transcript after hand-back = %q, want %q", got, want)
	}
	after, _ := r.AcpTranscript(e.ID)
	if after.Epoch <= before.Epoch {
		t.Errorf("epoch %d after hand-back, want above %d", after.Epoch, before.Epoch)
	}
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
		t.Errorf("calls = %q, want the create's session/new and one session/load of %s", acptest.Calls(dir), sid)
	}
	if k := persistedKind(t, r, e.ID); k != wire.KindACP {
		t.Errorf("persisted kind = %q, want acp", k)
	}
	if err := r.PromptACP(e.ID, "four", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "fourth turn", func() bool { return len(transcriptTexts(t, r, e.ID)) == 8 })
}

// Claude falls back to a fresh conversation when its transcript is
// missing, so a takeover then is refused before anything is stopped.
func TestTakeoverRefusedWithoutTranscript(t *testing.T) {
	useFakeACP(t)
	transcriptOK(t, false)
	cli := useFakeCLI(t, "", nil)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.SetKind(e.ID, wire.KindPTY); !errors.Is(err, ErrTakeoverRefused) {
		t.Fatalf("SetKind = %v, want ErrTakeoverRefused", err)
	}
	if in := info(r, e.ID); in.Kind != wire.KindACP || !in.Alive || persistedKind(t, r, e.ID) != wire.KindACP {
		t.Errorf("refused takeover changed the session: kind %q alive %v", in.Kind, in.Alive)
	}
	if n := len(cli.calls()); n != 0 {
		t.Errorf("refused takeover spawned %d terminals", n)
	}
}

// Codex keeps no transcript Hive reads, and `codex resume <id>` has no
// fresh-session fallback, so the conversation id alone is enough.
func TestTakeoverCodexLikeNeedsOnlySessionID(t *testing.T) {
	useFakeACP(t)
	transcriptOK(t, false) // never consulted for Codex
	cli := useFakeCLI(t, "", nil)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "c", Agent: string(agent.IDCodex)})
	if err := r.SetKind(e.ID, wire.KindPTY); err != nil {
		t.Fatalf("SetKind = %v", err)
	}
	if got := cli.calls(); len(got) != 1 || !slices.Equal(got[0], []string{"codex", "resume", agentSessionID(r, e.ID)}) {
		t.Errorf("terminal argv = %q, want codex resume <id>", got)
	}
}

// F2: Codex's CLI leaves a daemon holding the thread, so the adapter's
// session/load fails. The user gets a clear error and the terminal back.
func TestHandBackWriterLockedClearErrorAndRevertsToPTY(t *testing.T) {
	useFakeACP(t, acptest.FlagWriterLocked)
	cli := useFakeCLI(t, "", nil)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "c", Agent: string(agent.IDCodex)})
	if err := r.SetKind(e.ID, wire.KindPTY); err != nil {
		t.Fatal(err)
	}
	err := r.SetKind(e.ID, wire.KindACP)
	if !errors.Is(err, ErrACPWriterLocked) || !strings.Contains(err.Error(), "Codex") {
		t.Fatalf("hand-back = %v, want ErrACPWriterLocked naming Codex", err)
	}
	waitFor(t, "terminal back", func() bool { return info(r, e.ID).Alive })
	if in := info(r, e.ID); in.Kind != "" || persistedKind(t, r, e.ID) != "" {
		t.Errorf("after a failed hand-back kind = %q, want a terminal", in.Kind)
	}
	if n := len(cli.calls()); n != 2 {
		t.Errorf("terminal spawns = %d, want 2 (takeover, then the revert)", n)
	}
}

func TestKindFlipPersisted(t *testing.T) {
	useFakeACP(t)
	transcriptOK(t, true)
	useFakeCLI(t, "", nil)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.SetKind(e.ID, wire.KindPTY); err != nil {
		t.Fatal(err)
	}
	stateDir := r.stateDir
	r.Close()
	r2, err := Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r2.Close() })
	if k := info(r2, e.ID).Kind; k != "" {
		t.Errorf("reloaded kind = %q, want a terminal", k)
	}
	if err := r2.SetKind(e.ID, "bogus"); !errors.Is(err, ErrBadKind) {
		t.Errorf("SetKind(bogus) = %v, want ErrBadKind", err)
	}
	if err := r2.SetKind(e.ID, wire.KindPTY); err != nil {
		t.Errorf("SetKind to the current kind = %v, want a no-op", err)
	}
}

// A takeover mid-turn would kill the turn.
func TestTakeoverWhileWorkingRejected(t *testing.T) {
	dir := useFakeACP(t, acptest.FlagBlock)
	transcriptOK(t, true)
	useFakeCLI(t, "", nil)
	t.Cleanup(func() { _ = acptest.Release(dir) })
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.PromptACP(e.ID, "one", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	if err := r.SetKind(e.ID, wire.KindPTY); !errors.Is(err, ErrACPBusy) {
		t.Errorf("SetKind mid-turn = %v, want ErrACPBusy", err)
	}
}

// Pi resumes by the id pi-acp mapped the ACP session to; an ACP id
// missing from that map is refused rather than guessed.
func TestTakeoverPiUnmappedRefused(t *testing.T) {
	useFakeACP(t)
	useSettings(t, withCeiling(map[string]string{"pi": agent.ACPModeUnattended}))
	transcriptOK(t, true)
	cli := useFakeCLI(t, "", nil)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "p", Agent: string(agent.IDPi)})
	if err := r.SetKind(e.ID, wire.KindPTY); !errors.Is(err, ErrTakeoverRefused) {
		t.Fatalf("SetKind = %v, want ErrTakeoverRefused", err)
	}
	if n := len(cli.calls()); n != 0 {
		t.Errorf("refused takeover spawned %d terminals", n)
	}
}

func TestTakeoverPiResolvesSessionMap(t *testing.T) {
	useFakeACP(t)
	useSettings(t, withCeiling(map[string]string{"pi": agent.ACPModeUnattended}))
	transcriptOK(t, true)
	cli := useFakeCLI(t, "", nil)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "p", Agent: string(agent.IDPi)})
	sid := agentSessionID(r, e.ID)
	writePiMap(t, home, sid, sid)
	if err := r.SetKind(e.ID, wire.KindPTY); err != nil {
		t.Fatalf("SetKind = %v", err)
	}
	if got := cli.calls(); len(got) != 1 || !slices.Equal(got[0][:3], []string{"pi", "--session-id", sid}) {
		t.Errorf("terminal argv = %q, want pi --session-id %s", got, sid)
	}
}

func TestSetKindUnknownSession(t *testing.T) {
	r := freshRegistry(t)
	if err := r.SetKind("nope", wire.KindPTY); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetKind(unknown) = %v, want ErrNotFound", err)
	}
}

// pi-acp mapping the ACP id to a different Pi id is refused: Hive keeps
// one id for both kinds, so a hand-back would load the wrong one.
func TestTakeoverPiMappedToOtherIDRefused(t *testing.T) {
	useFakeACP(t)
	useSettings(t, withCeiling(map[string]string{"pi": agent.ACPModeUnattended}))
	transcriptOK(t, true)
	cli := useFakeCLI(t, "", nil)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "p", Agent: string(agent.IDPi)})
	sid := agentSessionID(r, e.ID)
	writePiMap(t, home, sid, "other-id")
	if err := r.SetKind(e.ID, wire.KindPTY); !errors.Is(err, ErrTakeoverRefused) {
		t.Fatalf("SetKind = %v, want ErrTakeoverRefused", err)
	}
	if in := info(r, e.ID); in.Kind != wire.KindACP || persistedKind(t, r, e.ID) != wire.KindACP {
		t.Errorf("refused takeover changed the kind to %q", in.Kind)
	}
	if n := len(cli.calls()); n != 0 {
		t.Errorf("refused takeover spawned %d terminals", n)
	}
}

// writePiMap writes pi-acp's session map under home, mapping ACP id
// acpID to a Pi session file for piID.
func writePiMap(t *testing.T, home, acpID, piID string) {
	t.Helper()
	mapDir := filepath.Join(home, ".pi", "pi-acp")
	if err := os.MkdirAll(mapDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"sessions":{"` + acpID + `":{"sessionFile":"/s/2026-10-04T10-00-00-000Z_` + piID + `.jsonl"}}}`
	if err := os.WriteFile(filepath.Join(mapDir, "session-map.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeCLI) failFromLaunch(n int) {
	f.mu.Lock()
	f.failFrom = n
	f.mu.Unlock()
}

// A second switch, or a new turn, while one switch runs is refused, so
// a double click cannot interleave two restarts and a prompt cannot be
// accepted only to be killed by the restart.
func TestSwitchInProgressRefusesSwitchAndPrompt(t *testing.T) {
	useFakeACP(t)
	transcriptOK(t, true)
	cli := useFakeCLI(t, "", nil)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	r.mu.Lock()
	r.entries[e.ID].switching = true
	r.mu.Unlock()
	if err := r.SetKind(e.ID, wire.KindPTY); !errors.Is(err, ErrACPBusy) {
		t.Errorf("SetKind during a switch = %v, want ErrACPBusy", err)
	}
	if err := r.PromptACP(e.ID, "hi", wire.OriginUser); !errors.Is(err, ErrACPBusy) {
		t.Errorf("PromptACP during a switch = %v, want ErrACPBusy", err)
	}
	r.mu.Lock()
	r.entries[e.ID].switching = false
	r.mu.Unlock()
	if n := len(cli.calls()); n != 0 {
		t.Errorf("a refused switch spawned %d terminals", n)
	}
	// The flag is released when a switch ends, refused or not.
	if err := r.SetKind(e.ID, wire.KindPTY); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	stuck := r.entries[e.ID].switching
	r.mu.Unlock()
	if stuck {
		t.Error("switching still set after SetKind returned")
	}
}

// A permission card waiting on the user is a turn in flight.
func TestTakeoverWithPendingPermissionRejected(t *testing.T) {
	useFakeACP(t, acptest.FlagPermission)
	transcriptOK(t, true)
	useFakeCLI(t, "", nil)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.PromptACP(e.ID, "one", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "permission", func() bool { return info(r, e.ID).State == wire.StateWaitingPermission })
	if err := r.SetKind(e.ID, wire.KindPTY); !errors.Is(err, ErrACPBusy) {
		t.Errorf("SetKind with a pending permission = %v, want ErrACPBusy", err)
	}
}

// A hand-back that fails for any reason, not only the writer lock,
// reverts to a terminal and restarts it.
func TestHandBackLoadFailsRevertsToPTY(t *testing.T) {
	useFakeACP(t, acptest.FlagLoadFails)
	transcriptOK(t, true)
	cli := useFakeCLI(t, "", nil)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.SetKind(e.ID, wire.KindPTY); err != nil {
		t.Fatal(err)
	}
	err := r.SetKind(e.ID, wire.KindACP)
	if err == nil || errors.Is(err, ErrACPWriterLocked) || !strings.Contains(err.Error(), "session/load") {
		t.Fatalf("hand-back = %v, want the load error, not a writer lock", err)
	}
	waitFor(t, "terminal back", func() bool { return info(r, e.ID).Alive })
	if in := info(r, e.ID); in.Kind != "" || persistedKind(t, r, e.ID) != "" {
		t.Errorf("after a failed hand-back kind = %q, want a terminal", in.Kind)
	}
	if n := len(cli.calls()); n != 2 {
		t.Errorf("terminal spawns = %d, want 2 (takeover, then the revert)", n)
	}
}

// When the revert's restart fails too, the caller hears both.
func TestHandBackRevertRestartFailureReported(t *testing.T) {
	useFakeACP(t, acptest.FlagLoadFails)
	transcriptOK(t, true)
	cli := useFakeCLI(t, "", nil)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.SetKind(e.ID, wire.KindPTY); err != nil {
		t.Fatal(err)
	}
	cli.failFromLaunch(2)
	err := r.SetKind(e.ID, wire.KindACP)
	if err == nil || !strings.Contains(err.Error(), "session/load") || !strings.Contains(err.Error(), "fake CLI: not found") {
		t.Fatalf("hand-back = %v, want both the load and the restart failure", err)
	}
	if k := persistedKind(t, r, e.ID); k != "" {
		t.Errorf("persisted kind = %q, want the terminal it reverted to", k)
	}
}

// A takeover whose terminal fails to start goes back to ACP rather than
// leaving the session down as a terminal.
func TestTakeoverLaunchFailsRevertsToACP(t *testing.T) {
	useFakeACP(t)
	transcriptOK(t, true)
	cli := useFakeCLI(t, "", nil)
	cli.failFromLaunch(1)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.SetKind(e.ID, wire.KindPTY); err == nil || !strings.Contains(err.Error(), "fake CLI: not found") {
		t.Fatalf("takeover = %v, want the launch error", err)
	}
	waitFor(t, "acp back", func() bool { return info(r, e.ID).Alive })
	if in := info(r, e.ID); in.Kind != wire.KindACP || persistedKind(t, r, e.ID) != wire.KindACP {
		t.Errorf("after a failed takeover kind = %q, want acp", in.Kind)
	}
}

func TestTakeoverRefusedWithoutSessionID(t *testing.T) {
	useFakeACP(t)
	transcriptOK(t, true)
	useFakeCLI(t, "", nil)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	r.mu.Lock()
	r.entries[e.ID].AgentSessionID = ""
	r.mu.Unlock()
	if err := r.SetKind(e.ID, wire.KindPTY); !errors.Is(err, ErrTakeoverRefused) {
		t.Errorf("SetKind = %v, want ErrTakeoverRefused", err)
	}
}

// A terminal session of an agent with no ACP adapter cannot be handed
// to ACP.
func TestHandBackRefusedForAgentWithoutACP(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, err := r.Create(t.Context(), wire.CreateSpec{Name: "p", Shell: "/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Kill(e.ID, true) })
	if err := r.SetKind(e.ID, wire.KindACP); !errors.Is(err, ErrBadKind) {
		t.Errorf("SetKind = %v, want ErrBadKind", err)
	}
}

// Settings that forbid ACP for the agent now refuse the hand-back
// before the terminal is stopped.
func TestHandBackRefusedBySettings(t *testing.T) {
	useFakeACP(t)
	useSettings(t, withCeiling(map[string]string{"pi": agent.ACPModeUnattended}))
	transcriptOK(t, true)
	cli := useFakeCLI(t, "", nil)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "p", Agent: string(agent.IDPi)})
	sid := agentSessionID(r, e.ID)
	writePiMap(t, home, sid, sid)
	if err := r.SetKind(e.ID, wire.KindPTY); err != nil {
		t.Fatal(err)
	}
	useSettings(t, withCeiling(map[string]string{"pi": agent.ACPModeOff}))
	if err := r.SetKind(e.ID, wire.KindACP); !errors.Is(err, ErrACPRefused) {
		t.Errorf("SetKind = %v, want ErrACPRefused", err)
	}
	if n := len(cli.calls()); n != 1 {
		t.Errorf("terminal spawns = %d, want 1: a refused hand-back must not restart the terminal", n)
	}
}

// An agent whose CLI cannot resume by id cannot be taken over.
func TestTakeoverPrecheckNeedsResumeArgs(t *testing.T) {
	def, _ := agent.Get(agent.IDCodex)
	def.ResumeArgs = nil
	if err := takeoverPrecheck(def, "sid", "/repo"); !errors.Is(err, ErrTakeoverRefused) {
		t.Errorf("takeoverPrecheck = %v, want ErrTakeoverRefused", err)
	}
}

// Restart Session, takeover and hand-back each replace the process, so
// none may report the session dead on the way (see
// TestRestartNeverReportsDeadWhileRespawning).
func TestKindSwitchAndRestartNeverReportDead(t *testing.T) {
	dir := useFakeACP(t)
	transcriptOK(t, true)
	useFakeCLI(t, dir, nil)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.PromptACP(e.ID, "one", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))

	log, stop := watch(t, r)
	defer stop()
	for _, step := range []struct {
		name string
		run  func() error
		kind string
	}{
		{"restart", func() error { return r.Restart(e.ID) }, wire.KindACP},
		{"take over", func() error { return r.SetKind(e.ID, wire.KindPTY) }, ""},
		{"hand back", func() error { return r.SetKind(e.ID, wire.KindACP) }, wire.KindACP},
	} {
		if err := step.run(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		waitFor(t, step.name, func() bool {
			in := info(r, e.ID)
			return in.Alive && in.Kind == step.kind && in.Phase == wire.PhaseReady
		})
	}
	if dead := log.deadWhileReady(e.ID); len(dead) > 0 {
		t.Errorf("reported the session dead %d time(s): %s (all events %s)", len(dead), joined(dead), joined(log.phasesFor(e.ID)))
	}
}
