package registry

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/acp/acptest"
	"github.com/lucascaro/hive/internal/wire"
)

// Typed results (spec 496, phase 3): the submit transport, the result
// lifecycle on the wire, and the one auto-allowed permission.

func nonceOf(r *Registry, id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if as := r.entries[id].acp; as != nil {
		return as.nonce
	}
	return ""
}

func result(t *testing.T, r *Registry, id string) wire.AcpTranscriptMsg {
	t.Helper()
	msg, err := r.AcpTranscript(id)
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

// startBlockedTurn sends a prompt the fake holds until released, so a
// result can be submitted while the turn runs.
func startBlockedTurn(t *testing.T, r *Registry, id string) {
	t.Helper()
	if err := r.PromptACP(id, "task", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitResultRecordedForRunningTurn(t *testing.T) {
	dir := useFakeACP(t, acptest.FlagBlock)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	startBlockedTurn(t, r, e.ID)
	if err := r.SubmitResult(e.ID, nonceOf(r, e.ID), json.RawMessage(`{ "status": "ok", "data": {"n": 1} }`)); err != nil {
		t.Fatal(err)
	}
	acptest.Release(dir)
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	msg := result(t, r, e.ID)
	// The turn ending must not overwrite a submitted result with "none".
	if msg.ResultStatus != wire.AcpResultSubmitted || string(msg.Result) != `{"status":"ok","data":{"n":1}}` {
		t.Errorf("result = %q %s, want submitted and the compacted object", msg.ResultStatus, msg.Result)
	}
	if msg.PromptID == 0 || msg.Items[0].ID != msg.PromptID || msg.Items[0].Kind != wire.AcpItemUser {
		t.Errorf("prompt_id = %d, want the id of the user item %+v", msg.PromptID, msg.Items[0])
	}
}

func TestSubmitResultWrongNonceRejected(t *testing.T) {
	dir := useFakeACP(t, acptest.FlagBlock)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	other := createACP(t, r, wire.CreateSpec{Name: "b"})
	startBlockedTurn(t, r, e.ID)
	obj := json.RawMessage(`{"status":"ok"}`)
	for name, nonce := range map[string]string{
		"empty":                 "",
		"guessed":               strings.Repeat("0", 64),
		"another session's own": nonceOf(r, other.ID),
	} {
		if err := r.SubmitResult(e.ID, nonce, obj); !errors.Is(err, ErrSubmitRejected) {
			t.Errorf("%s nonce: SubmitResult = %v, want ErrSubmitRejected", name, err)
		}
	}
	if err := r.SubmitResult(e.ID, nonceOf(r, e.ID), json.RawMessage(`[1]`)); !errors.Is(err, ErrSubmitRejected) {
		t.Errorf("non-object result = %v, want ErrSubmitRejected", err)
	}
	big := json.RawMessage(`{"s":"` + strings.Repeat("x", wire.MaxAcpResult) + `"}`)
	if err := r.SubmitResult(e.ID, nonceOf(r, e.ID), big); !errors.Is(err, ErrSubmitRejected) {
		t.Errorf("oversized result = %v, want ErrSubmitRejected", err)
	}
	if msg := result(t, r, e.ID); msg.ResultStatus != "" || msg.Result != nil {
		t.Errorf("a rejected submit changed the result: %q %s", msg.ResultStatus, msg.Result)
	}
	acptest.Release(dir)
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	// After the turn: nothing to attach a result to.
	if err := r.SubmitResult(e.ID, nonceOf(r, e.ID), obj); !errors.Is(err, ErrSubmitRejected) {
		t.Errorf("submit after the turn ended = %v, want ErrSubmitRejected", err)
	}
}

// F3: a turn that ends without a submit is reported as having none.
func TestTurnEndWithoutResultReportsNone(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	l, cleanup := r.SubscribeACP()
	defer cleanup()
	if err := r.PromptACP(e.ID, "hi", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	if msg := result(t, r, e.ID); msg.ResultStatus != wire.AcpResultNone || msg.Result != nil || msg.PromptID == 0 {
		t.Errorf("result after a turn with no submit = %q %s prompt %d, want none", msg.ResultStatus, msg.Result, msg.PromptID)
	}
	// And it was announced, not only stored.
	for {
		select {
		case m := <-l:
			if m.SessionID == e.ID && m.ResultStatus == wire.AcpResultNone {
				return
			}
		default:
			t.Fatal("no ACP_TRANSCRIPT message carried result_status none")
		}
	}
}

func TestResultClearedOnNextPrompt(t *testing.T) {
	dir := useFakeACP(t, acptest.FlagBlock)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	startBlockedTurn(t, r, e.ID)
	if err := r.SubmitResult(e.ID, nonceOf(r, e.ID), json.RawMessage(`{"status":"ok"}`)); err != nil {
		t.Fatal(err)
	}
	acptest.Release(dir)
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	first := result(t, r, e.ID).PromptID
	if err := r.PromptACP(e.ID, "again", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	msg := result(t, r, e.ID)
	if msg.PromptID == first || msg.Result != nil || msg.ResultStatus == wire.AcpResultSubmitted {
		t.Errorf("after a new prompt: prompt %d (was %d) status %q result %s; want a new prompt with no result", msg.PromptID, first, msg.ResultStatus, msg.Result)
	}
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
}

// The submit tool is the one permission the daemon answers itself;
// every other request in the same turn still waits for the user.
func TestSubmitAutoAllowedOtherPermissionWaits(t *testing.T) {
	useFakeACP(t, acptest.FlagSubmit, acptest.FlagPermission)
	r := freshRegistry(t)
	r.SetHivedPath("/opt/hive/hived")
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.PromptACP(e.ID, "go", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	var perm *wire.AcpPermission
	waitFor(t, "the other permission request", func() bool {
		perm = result(t, r, e.ID).Permission
		return perm != nil
	})
	if perm.ToolCallID != "t1" {
		t.Fatalf("pending request = %+v, want the Read file tool (t1), not the submit tool", perm)
	}
	if err := r.AnswerPermission(e.ID, perm.RequestID, "allow"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	got := transcriptTexts(t, r, e.ID)
	want := "agent:echo: go [submit selected:allow-once] [selected:allow]"
	if !slices.Contains(got, want) {
		t.Errorf("transcript = %q, want %q", got, want)
	}
}

// With no hived path there is no submit server, so nothing is
// auto-allowed: a user's own server named like Hive's gets no pass.
func TestSubmitNotAutoAllowedWithoutServer(t *testing.T) {
	useFakeACP(t, acptest.FlagSubmit)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.PromptACP(e.ID, "go", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the submit request reaches the user", func() bool { return result(t, r, e.ID).Permission != nil })
	perm := result(t, r, e.ID).Permission
	if err := r.AnswerPermission(e.ID, perm.RequestID, "reject"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
}

// The adapter forgets its MCP servers across processes, so every start
// — session/new and every session/load — sends the submit server, with
// the session's own HIVE_* values in its env and nowhere else.
func TestMCPServersResentOnLoad(t *testing.T) {
	dir := useFakeACP(t)
	r := freshRegistry(t)
	r.SetHivedPath("/opt/hive/hived")
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.Restart(e.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "reload", func() bool { return info(r, e.ID).Alive })
	var lines []string
	for _, c := range acptest.Calls(dir) {
		if strings.HasPrefix(c, "mcp ") {
			lines = append(lines, c)
		}
	}
	if len(lines) != 2 {
		t.Fatalf("mcp server lines = %q, want one for session/new and one for session/load", lines)
	}
	for _, l := range lines {
		f := strings.Fields(l)
		// mcp <sid> <name> <command> <args…> <env names…>
		if len(f) != 8 || !strings.HasPrefix(f[2], "hive-") || f[3] != "/opt/hive/hived" || f[4] != "mcp-submit" ||
			!slices.Equal(f[5:], []string{"HIVE_SESSION_ID", "HIVE_SOCKET", "HIVE_SUBMIT_NONCE"}) {
			t.Errorf("mcp server = %q, want hive-<id> running `hived mcp-submit` with the session's HIVE_* env", l)
		}
	}
}
