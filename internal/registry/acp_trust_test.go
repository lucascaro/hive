package registry

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/acp/acptest"
	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
)

// Trust (spec 496, criterion 5): every ACP session's mode is set
// explicitly, never above the user's ceiling; Pi runs only unattended.

// useSettings points agent settings at a temp dir holding st.
func useSettings(t *testing.T, st agent.Settings) {
	t.Helper()
	agent.SetCustomDir(t.TempDir())
	t.Cleanup(func() { agent.SetCustomDir("") })
	if err := agent.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
}

func withCeiling(ceilings map[string]string) agent.Settings {
	st := agent.DefaultSettings()
	st.ACPModeCeiling = ceilings
	return st
}

// setModes returns the modes the fake was set to, in order — every
// call, including one sent with an empty session id.
func setModes(dir string) []string {
	var out []string
	for _, c := range acptest.Calls(dir) {
		if f := strings.Fields(c); len(f) >= 2 && f[0] == "session/set_mode" {
			out = append(out, f[len(f)-1])
		}
	}
	return out
}

func TestModeSetToCeilingAfterNew(t *testing.T) {
	dir := useFakeACP(t)
	useSettings(t, agent.DefaultSettings())
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	createACP(t, r, wire.CreateSpec{Name: "c", Agent: string(agent.IDCodex)})
	// Claude's default ceiling is Manual; Codex's is read-only, never its
	// adapter's own default "agent" (F4).
	if got := setModes(dir); !slices.Equal(got, []string{"default", "read-only"}) {
		t.Fatalf("set_mode calls = %q, want default (claude) then read-only (codex)", got)
	}
	// session/load comes back in the adapter's default mode; the mode is
	// set again after every load.
	if err := r.Restart(e.ID); err != nil {
		t.Fatal(err)
	}
	if got := setModes(dir); !slices.Equal(got, []string{"default", "read-only", "default"}) {
		t.Errorf("set_mode calls after restart = %q, want default re-sent after the load", got)
	}
}

func TestCreateACPModeRequestedBelowCeiling(t *testing.T) {
	dir := useFakeACP(t)
	useSettings(t, withCeiling(map[string]string{"claude": "acceptEdits"}))
	r := freshRegistry(t)
	createACP(t, r, wire.CreateSpec{Name: "a", ACPMode: "plan"})
	createACP(t, r, wire.CreateSpec{Name: "b", ACPMode: "acceptEdits"})
	if got := setModes(dir); !slices.Equal(got, []string{"plan", "acceptEdits"}) {
		t.Errorf("set_mode calls = %q, want the requested plan and acceptEdits", got)
	}
}

func TestCreateACPModeAboveCeilingRefused(t *testing.T) {
	dir := useFakeACP(t)
	useSettings(t, agent.DefaultSettings())
	r := freshRegistry(t)
	_, err := r.Create(context.Background(), wire.CreateSpec{Kind: wire.KindACP, Agent: "claude", ACPMode: "bypassPermissions"})
	if !errors.Is(err, ErrACPModeAboveCeiling) {
		t.Errorf("create above the ceiling = %v, want ErrACPModeAboveCeiling", err)
	}
	_, err = r.Create(context.Background(), wire.CreateSpec{Kind: wire.KindACP, Agent: "claude", ACPMode: "no-such-mode"})
	if !errors.Is(err, ErrBadKind) {
		t.Errorf("create with an unknown mode = %v, want ErrBadKind", err)
	}
	if len(r.List()) != 0 || len(acptest.Calls(dir)) != 0 {
		t.Errorf("a refused create left sessions %v or started the adapter %q", r.List(), acptest.Calls(dir))
	}
}

// Lowering the ceiling applies from the next start: a session created at
// acceptEdits comes back at the new, lower ceiling.
func TestRestartClampsModeToLoweredCeiling(t *testing.T) {
	dir := useFakeACP(t)
	useSettings(t, withCeiling(map[string]string{"claude": "acceptEdits"}))
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a", ACPMode: "acceptEdits"})
	if err := agent.SaveSettings(agent.DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	if err := r.Restart(e.ID); err != nil {
		t.Fatal(err)
	}
	if got := setModes(dir); !slices.Equal(got, []string{"acceptEdits", "default"}) {
		t.Errorf("set_mode calls = %q, want acceptEdits then, after restart, the lowered ceiling", got)
	}
}

func TestSetModeFailureKillsSession(t *testing.T) {
	useFakeACP(t, acptest.FlagSetModeFails)
	useSettings(t, agent.DefaultSettings())
	r := freshRegistry(t)
	e, err := r.Create(context.Background(), wire.CreateSpec{Kind: wire.KindACP, Agent: "claude"})
	if err == nil {
		defer r.Kill(e.ID, true)
	}
	if e == nil {
		t.Fatalf("Create = %v; want the entry kept, dead, with the reason", err)
	}
	in := info(r, e.ID)
	waitFor(t, "adapter gone", func() bool { in = info(r, e.ID); return !in.Alive })
	if !strings.Contains(in.LastError, "set_mode") {
		t.Errorf("LastError = %q, want it to name set_mode", in.LastError)
	}
}

// An adapter switching itself above the ceiling is switched back.
func TestEscalationResetToCeiling(t *testing.T) {
	dir := useFakeACP(t, acptest.FlagEscalate)
	useSettings(t, agent.DefaultSettings())
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.PromptACP(e.ID, "go", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the reset", func() bool { return slices.Equal(setModes(dir), []string{"default", "default"}) })
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	if !info(r, e.ID).Alive {
		t.Error("a successful reset closed the session")
	}
}

// resets is how many escalations id's running adapter was reset from.
func resets(r *Registry, id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if as := r.entries[id].acp; as != nil {
		return as.resets
	}
	return -1
}

// answerAndFinish starts a turn, answers its permission request with
// optionID, and waits for the turn to end.
func answerAndFinish(t *testing.T, r *Registry, id, optionID string) {
	t.Helper()
	if err := r.PromptACP(id, "go", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	var perm *wire.AcpPermission
	waitFor(t, "permission", func() bool { perm = result(t, r, id).Permission; return perm != nil })
	if err := r.AnswerPermission(id, perm.RequestID, optionID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "turn end", idleAfterTurn(r, id))
}

// The switch the user chose on the exit-plan card stands: the fake
// switches to acceptEdits, which is what that option sets.
func TestUserChosenModeSwitchKept(t *testing.T) {
	dir := useFakeACP(t, acptest.FlagPermission, acptest.FlagEscalateAfterAnswer)
	useSettings(t, agent.DefaultSettings())
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	answerAndFinish(t, r, e.ID, "exit-plan-accept-edits")
	// Settled before the turn ended: updates are handled in order, ahead
	// of the prompt's reply.
	if n := resets(r, e.ID); n != 0 {
		t.Errorf("resets = %d, want 0: the user chose acceptEdits", n)
	}
	if got := setModes(dir); !slices.Equal(got, []string{"default"}) {
		t.Errorf("set_mode calls = %q, want only the initial default", got)
	}
}

// Any other answer — a plain allow on an unrelated card, or a deny —
// opens no window: the switch that follows it is reset.
func TestOtherAnswersOpenNoEscalationWindow(t *testing.T) {
	for _, option := range []string{"allow", "reject"} {
		t.Run(option, func(t *testing.T) {
			dir := useFakeACP(t, acptest.FlagPermission, acptest.FlagEscalateAfterAnswer)
			useSettings(t, agent.DefaultSettings())
			r := freshRegistry(t)
			e := createACP(t, r, wire.CreateSpec{Name: "a"})
			answerAndFinish(t, r, e.ID, option)
			waitFor(t, "the reset", func() bool { return slices.Equal(setModes(dir), []string{"default", "default"}) })
		})
	}
}

// The window is for exactly the mode the user chose: a different
// above-ceiling switch after it is still reset.
func TestUserChosenModeOnlyExactly(t *testing.T) {
	dir := useFakeACP(t)
	useSettings(t, agent.DefaultSettings())
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	r.mu.Lock()
	ent := r.entries[e.ID]
	ent.acp.userMode = "acceptEdits"
	r.onACPModeLocked(e.ID, ent, ent.acp, "bypassPermissions")
	left := ent.acp.userMode
	r.mu.Unlock()
	waitFor(t, "the reset", func() bool { return slices.Equal(setModes(dir), []string{"default", "default"}) })
	if left != "" {
		t.Errorf("userMode = %q after a switch, want it consumed", left)
	}
}

// A switch the adapter reports while session/new is still running is
// not policed: the session id is not known yet, and startACP's own
// set_mode follows. Before the guard, the reset went out with an empty
// session id.
func TestModeSwitchDuringStartupNotPoliced(t *testing.T) {
	dir := useFakeACP(t, acptest.FlagModeOnNew)
	useSettings(t, agent.DefaultSettings())
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	// The switch arrived before session/new returned, so Create has
	// seen it handled.
	if n := resets(r, e.ID); n != 0 {
		t.Errorf("resets = %d, want 0 during startup", n)
	}
	if got := setModes(dir); !slices.Equal(got, []string{"default"}) {
		t.Errorf("set_mode calls = %q, want only startACP's own", got)
	}
	if !info(r, e.ID).Alive {
		t.Error("the session died during startup")
	}
}

// An adapter that does not offer the ceiling mode is never run in one
// it does offer.
func TestAdapterWithoutCeilingModeRefused(t *testing.T) {
	dir := useFakeACP(t, acptest.FlagFewModes)
	useSettings(t, agent.DefaultSettings())
	r := freshRegistry(t)
	e, _ := r.Create(context.Background(), wire.CreateSpec{Kind: wire.KindACP, Agent: "claude"})
	if e == nil {
		t.Fatal("Create returned no entry; want it kept, dead, with the reason")
	}
	defer r.Kill(e.ID, true)
	var in wire.SessionInfo
	waitFor(t, "adapter gone", func() bool { in = info(r, e.ID); return !in.Alive })
	if !strings.Contains(in.LastError, `does not offer mode "default"`) {
		t.Errorf("LastError = %q, want it to name the missing mode", in.LastError)
	}
	if got := setModes(dir); len(got) != 0 {
		t.Errorf("set_mode calls = %q, want none", got)
	}
}

// An adapter that will not switch back is closed, with the reason.
func TestFailedResetClosesAdapter(t *testing.T) {
	useFakeACP(t, acptest.FlagEscalate, acptest.FlagSetModeFailsAfterFirst)
	useSettings(t, agent.DefaultSettings())
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.PromptACP(e.ID, "go", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	var in wire.SessionInfo
	waitFor(t, "adapter closed", func() bool { in = info(r, e.ID); return !in.Alive })
	if !strings.Contains(in.LastError, "above your ceiling") {
		t.Errorf("LastError = %q, want it to say the agent went above the ceiling", in.LastError)
	}
}

func TestPiRefusedWithoutUnattended(t *testing.T) {
	dir := useFakeACP(t)
	useSettings(t, agent.DefaultSettings())
	r := freshRegistry(t)
	if _, err := r.Create(context.Background(), wire.CreateSpec{Kind: wire.KindACP, Agent: string(agent.IDPi)}); !errors.Is(err, ErrACPRefused) {
		t.Fatalf("Pi ACP create by default = %v, want ErrACPRefused", err)
	}
	if len(acptest.Calls(dir)) != 0 {
		t.Errorf("a refused Pi create started the adapter: %q", acptest.Calls(dir))
	}
	useSettings(t, withCeiling(map[string]string{"pi": agent.ACPModeUnattended}))
	e := createACP(t, r, wire.CreateSpec{Name: "p", Agent: string(agent.IDPi)})
	if !info(r, e.ID).Alive {
		t.Fatal("Pi with unattended allowed did not start")
	}
	// Pi's modes are thinking levels: Hive sets none.
	if got := setModes(dir); len(got) != 0 {
		t.Errorf("set_mode sent to Pi: %q", got)
	}
	// Turning unattended off again applies from the next start.
	useSettings(t, agent.DefaultSettings())
	if err := r.Restart(e.ID); err == nil || !errors.Is(err, ErrACPRefused) {
		t.Errorf("Pi restart after unattended was turned off = %v, want ErrACPRefused", err)
	}
}

// Pi ignores MCP servers (F5): its submit transport is Hive's extension,
// loaded through the pi-acp shim, with the session's own HIVE_* values
// put back on the adapter's env for it — and no MCP server sent.
func TestPiACPGetsShimAndSubmitEnv(t *testing.T) {
	dir := useFakeACP(t)
	useSettings(t, withCeiling(map[string]string{"pi": agent.ACPModeUnattended}))
	r := freshRegistry(t)
	r.SetHivedPath("/opt/hive/hived")
	if err := agent.EnsurePiExtension(r.stateDir); err != nil {
		t.Fatal(err)
	}
	createACP(t, r, wire.CreateSpec{Name: "p", Agent: string(agent.IDPi)})
	var env []string
	for _, c := range acptest.Calls(dir) {
		if strings.HasPrefix(c, "mcp ") {
			t.Errorf("an MCP server was sent to Pi: %q", c)
		}
		if f := strings.Fields(c); len(f) > 0 && f[0] == "env" {
			env = f[1:]
		}
	}
	want := []string{"HIVE_PI_PLAN_REVIEW", "HIVE_PI_TODO_TOOL", "HIVE_SESSION_ID", "HIVE_SOCKET", "HIVE_SUBMIT_NONCE", "PI_ACP_PI_COMMAND"}
	if !slices.Equal(env, want) {
		t.Errorf("Pi adapter env = %q, want %q", env, want)
	}
	// Every other agent's adapter gets no HIVE_* at all: its submit
	// server carries them instead.
	createACP(t, r, wire.CreateSpec{Name: "c"})
	var last []string
	for _, c := range acptest.Calls(dir) {
		if f := strings.Fields(c); len(f) > 0 && f[0] == "env" {
			last = f[1:]
		}
	}
	if len(last) != 0 {
		t.Errorf("Claude adapter env carries %q, want no HIVE_* or PI_ACP_*", last)
	}
}
