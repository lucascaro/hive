// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/acp"
	"github.com/lucascaro/hive/internal/acp/acptest"
	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
)

// Workflow-engine prerequisites (spec 528): create split in two, typed
// results with a schema, ACP cancel, stop reason and usage.

const findingsSchema = `{"type":"object","properties":{"findings":{"type":"array","items":{"type":"object","properties":{"severity":{"type":"string","enum":["high","low"]}},"required":["severity"],"additionalProperties":false}}},"required":["findings"],"additionalProperties":false}`

func TestBeginFinishCreate_EqualsCreate(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	ch, unsub := r.Subscribe()
	defer unsub()

	e, plan, err := r.BeginCreate(wire.CreateSpec{Kind: wire.KindACP, Agent: string(agent.IDClaude), Name: "split"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Kill(e.ID, true) })
	// The entry exists and `added` is out before the slow half starts.
	if got := info(r, e.ID); got.Alive || got.Phase != wire.PhaseStarting {
		t.Errorf("after BeginCreate: alive %v phase %q, want not alive and starting", got.Alive, got.Phase)
	}
	select {
	case ev := <-ch:
		if ev.Kind != wire.SessionEventAdded || ev.Session.ID != e.ID {
			t.Errorf("first event = %+v, want added for %s", ev, e.ID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no added event after BeginCreate")
	}
	if err := r.FinishCreate(context.Background(), e, wire.CreateSpec{Kind: wire.KindACP, Agent: string(agent.IDClaude), Name: "split"}, plan); err != nil {
		t.Fatal(err)
	}
	whole := createACP(t, r, wire.CreateSpec{Name: "whole"})
	a, b := info(r, e.ID), info(r, whole.ID)
	if a.Kind != b.Kind || a.Alive != b.Alive || a.Phase != b.Phase || a.Agent != b.Agent {
		t.Errorf("split create = %+v, Create = %+v; want the same shape", a, b)
	}
}

func TestCreate_InvalidResultSchemaRefusedBeforeEntry(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	ch, unsub := r.Subscribe()
	defer unsub()
	for _, schema := range []string{`{"type":"string"}`, `{"type":"object"}`, `{`} {
		_, _, err := r.BeginCreate(wire.CreateSpec{Kind: wire.KindACP, Agent: string(agent.IDClaude), ResultSchema: json.RawMessage(schema)})
		if !errors.Is(err, acp.ErrResultSchema) {
			t.Errorf("schema %s: err = %v, want ErrResultSchema", schema, err)
		}
	}
	if n := len(r.List()); n != 0 {
		t.Errorf("refused creates registered %d entries", n)
	}
	select {
	case ev := <-ch:
		t.Errorf("a refused create broadcast %+v", ev)
	default:
	}
}

func TestCreate_ResultSchemaOnPTYRefused(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	_, err := r.Create(context.Background(), wire.CreateSpec{Shell: "/bin/sh", ResultSchema: json.RawMessage(findingsSchema)})
	if !errors.Is(err, ErrNotACP) {
		t.Errorf("err = %v, want ErrNotACP", err)
	}
	if n := len(r.List()); n != 0 {
		t.Errorf("refused create registered %d entries", n)
	}
}

func submitFindings(r *Registry, id, body string) error {
	return r.SubmitResult(id, nonceOf(r, id), json.RawMessage(body))
}

func TestSubmitResult_ValidatesAgainstSchema(t *testing.T) {
	useFakeACP(t, acptest.FlagBlock)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a", ResultSchema: json.RawMessage(findingsSchema)})
	startBlockedTurn(t, r, e.ID)

	err := submitFindings(r, e.ID, `{"status":"ok","data":{"findings":[{"severity":"high"},{"severity":"low"},{"severity":"urgent"}]}}`)
	if !errors.Is(err, ErrSubmitRejected) || !strings.Contains(err.Error(), `data.findings[2].severity: "urgent" is not one of high|low; call submit_result again`) {
		t.Fatalf("err = %v, want a path-qualified rejection that says to call again", err)
	}
	if got := result(t, r, e.ID); got.ResultStatus != "" {
		t.Errorf("a rejected submit recorded %q", got.ResultStatus)
	}
	if err := submitFindings(r, e.ID, `{"status":"ok"}`); !errors.Is(err, ErrSubmitRejected) || !strings.Contains(err.Error(), "data: required") {
		t.Errorf("ok without data: err = %v, want data required", err)
	}
	if err := submitFindings(r, e.ID, `{"status":"ok","data":{"findings":[],"extra":1}}`); !errors.Is(err, ErrSubmitRejected) {
		t.Errorf("extra field: err = %v, want rejection", err)
	}
}

func TestSubmitResult_RetryAfterRejection(t *testing.T) {
	dir := useFakeACP(t, acptest.FlagBlock)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a", ResultSchema: json.RawMessage(findingsSchema)})
	startBlockedTurn(t, r, e.ID)
	if err := submitFindings(r, e.ID, `{"status":"ok","data":{"findings":[{"severity":"bad"}]}}`); err == nil {
		t.Fatal("bad data accepted")
	}
	if err := submitFindings(r, e.ID, `{"status":"ok","summary":"s","data":{"findings":[{"severity":"low"}]}}`); err != nil {
		t.Fatalf("retry: %v", err)
	}
	acptest.Release(dir)
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	if got := result(t, r, e.ID); got.ResultStatus != wire.AcpResultSubmitted || !strings.Contains(string(got.Result), `"low"`) {
		t.Errorf("result = %q %s, want the retried one", got.ResultStatus, got.Result)
	}
}

func TestSubmitResult_ErrorStatusSkipsSchema(t *testing.T) {
	useFakeACP(t, acptest.FlagBlock)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a", ResultSchema: json.RawMessage(findingsSchema)})
	startBlockedTurn(t, r, e.ID)
	if err := submitFindings(r, e.ID, `{"status":"error","summary":"could not"}`); err != nil {
		t.Fatalf("error status without data: %v", err)
	}
}

func TestSubmitResult_NoSchemaUnchanged(t *testing.T) {
	useFakeACP(t, acptest.FlagBlock)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	startBlockedTurn(t, r, e.ID)
	if err := submitFindings(r, e.ID, `{"status":"ok","data":{"anything":[1,"x"]}}`); err != nil {
		t.Fatalf("no schema must accept any data: %v", err)
	}
}

func TestResultSchema_SurvivesRestart(t *testing.T) {
	useFakeACP(t, acptest.FlagBlock)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a", ResultSchema: json.RawMessage(findingsSchema)})
	var meta MetaFile
	if err := readJSON(filepath.Join(SessionsDir(r.stateDir), e.ID, "session.json"), &meta); err != nil {
		t.Fatal(err)
	}
	var persisted bytes.Buffer // session.json is indented
	if err := json.Compact(&persisted, meta.ResultSchema); err != nil || persisted.String() != findingsSchema {
		t.Errorf("session.json result_schema = %s (%v)", meta.ResultSchema, err)
	}
	if err := r.Restart(e.ID); err != nil {
		t.Fatal(err)
	}
	startBlockedTurn(t, r, e.ID)
	if err := submitFindings(r, e.ID, `{"status":"ok","data":{"findings":[{"severity":"nope"}]}}`); !errors.Is(err, ErrSubmitRejected) {
		t.Errorf("after restart: err = %v, want the schema still enforced", err)
	}
}

func TestResultSchema_ReachesSubmitServerEnv(t *testing.T) {
	dir := useFakeACP(t)
	r := freshRegistry(t)
	r.hivedPath = "/bin/true" // an ACP start needs a submit server to name
	createACP(t, r, wire.CreateSpec{Name: "a", ResultSchema: json.RawMessage(findingsSchema)})
	// The fake logs "mcp <sid> <name> <command> <args…> <env names…>".
	var line string
	for _, l := range acptest.Calls(dir) {
		if strings.HasPrefix(l, "mcp ") {
			line = l
		}
	}
	if !strings.Contains(line, "HIVE_SUBMIT_SCHEMA") || !strings.Contains(line, "HIVE_SUBMIT_NONCE") {
		t.Errorf("mcp server line = %q, want HIVE_SUBMIT_SCHEMA beside the nonce", line)
	}
}

func TestPromptACPTurn_ReturnsTurnIDs(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	pid, epoch, err := r.PromptACPTurn(e.ID, "hello", wire.OriginUser)
	if err != nil {
		t.Fatal(err)
	}
	msg := result(t, r, e.ID)
	if pid == 0 || pid != msg.PromptID || epoch != msg.Epoch {
		t.Errorf("turn = (%d, %d), transcript says (%d, %d)", pid, epoch, msg.PromptID, msg.Epoch)
	}
}

func TestStopReason_EndTurnAndClearedOnPrompt(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.PromptACP(e.ID, "one", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	if got := result(t, r, e.ID); got.StopReason != "end_turn" {
		t.Fatalf("stop_reason = %q, want end_turn", got.StopReason)
	}
	// The next prompt clears it: the first message that carries the new
	// user item must not still report the last turn's end.
	ch, unsub := r.SubscribeACP()
	defer unsub()
	if err := r.PromptACP(e.ID, "two", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	for msg := range ch {
		if len(msg.Items) > 0 && msg.Items[0].Kind == wire.AcpItemUser && msg.Items[0].Text == "two" {
			if msg.StopReason != "" || len(msg.Usage) != 0 {
				t.Errorf("new prompt carries stop_reason %q usage %s, want both cleared", msg.StopReason, msg.Usage)
			}
			return
		}
	}
	t.Fatal("never saw the second prompt")
}

func TestUsage_ForwardedForTheTurn(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.PromptACP(e.ID, "hello", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	got := result(t, r, e.ID)
	var u struct {
		Used int `json:"used"`
		Size int `json:"size"`
	}
	if err := json.Unmarshal(got.Usage, &u); err != nil || u.Size != 1000 || u.Used != len("hello") {
		t.Errorf("usage = %s (%v), want the adapter's usage_update", got.Usage, err)
	}
}

func TestCancelACP_StopReasonCancelled(t *testing.T) {
	useFakeACP(t, acptest.FlagBlock)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	startBlockedTurn(t, r, e.ID)
	busy, err := r.CancelACP(e.ID)
	if err != nil || !busy {
		t.Fatalf("CancelACP = %v, %v; want busy", busy, err)
	}
	waitFor(t, "cancelled stop reason", func() bool { return result(t, r, e.ID).StopReason == "cancelled" })
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	// A cancelled turn has no result; it is never read as success (F3).
	if got := result(t, r, e.ID); got.ResultStatus != wire.AcpResultNone {
		t.Errorf("result_status = %q, want none", got.ResultStatus)
	}
}

func TestCancelACP_AnswersPendingPermissionCancelled(t *testing.T) {
	useFakeACP(t, acptest.FlagPermission)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if err := r.PromptACP(e.ID, "go", wire.OriginUser); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "permission card", func() bool { return result(t, r, e.ID).Permission != nil })
	if busy, err := r.CancelACP(e.ID); err != nil || !busy {
		t.Fatalf("CancelACP = %v, %v", busy, err)
	}
	waitFor(t, "cancelled stop reason", func() bool { return result(t, r, e.ID).StopReason == "cancelled" })
	if got := result(t, r, e.ID); got.Permission != nil {
		t.Errorf("permission still pending after cancel: %+v", got.Permission)
	}
}

func TestCancelACP_IdleIsNoopWasBusyFalse(t *testing.T) {
	useFakeACP(t)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	if busy, err := r.CancelACP(e.ID); err != nil || busy {
		t.Errorf("idle CancelACP = %v, %v; want false, nil", busy, err)
	}
	if _, err := r.CancelACP("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown session: err = %v, want ErrNotFound", err)
	}
	skipOnWindows(t)
	p, err := r.Create(context.Background(), wire.CreateSpec{Shell: "/bin/sh"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Kill(p.ID, true)
	if _, err := r.CancelACP(p.ID); !errors.Is(err, ErrNotACP) {
		t.Errorf("terminal session: err = %v, want ErrNotACP", err)
	}
}

func TestCancelACP_Twice(t *testing.T) {
	useFakeACP(t, acptest.FlagBlock)
	r := freshRegistry(t)
	e := createACP(t, r, wire.CreateSpec{Name: "a"})
	startBlockedTurn(t, r, e.ID)
	for range 2 {
		if _, err := r.CancelACP(e.ID); err != nil {
			t.Fatalf("CancelACP: %v", err)
		}
	}
	waitFor(t, "cancelled stop reason", func() bool { return result(t, r, e.ID).StopReason == "cancelled" })
	waitFor(t, "turn end", idleAfterTurn(r, e.ID))
	// After the turn is over a cancel is the idempotent no-op.
	if busy, err := r.CancelACP(e.ID); err != nil || busy {
		t.Errorf("late CancelACP = %v, %v; want false, nil", busy, err)
	}
	// And the session is usable again.
	if err := r.PromptACP(e.ID, "again", wire.OriginUser); err != nil {
		t.Errorf("prompt after cancel: %v", err)
	}
}
