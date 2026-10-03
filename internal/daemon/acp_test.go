package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/acp"
	"github.com/lucascaro/hive/internal/acp/acptest"
	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
)

func useFakeACP(t *testing.T, flags ...string) {
	t.Helper()
	skipOnWindows(t) // the fake agent is this binary re-executed; daemon tests are POSIX-only
	dir := t.TempDir()
	t.Cleanup(registry.SetACPCommandForTest(func(agent.Def) ([]string, []string) {
		return []string{os.Args[0]}, append(acp.AdapterEnv(os.Environ(), ""), acptest.Env(dir, flags...)...)
	}))
}

// createACPVia sends CREATE_SESSION for an ACP session through the
// frame handler with the given ops, and returns the new entry.
func createACPVia(t *testing.T, d *Daemon, ops controlOps, payload string) *registry.Entry {
	t.Helper()
	before := map[string]bool{}
	for _, s := range d.reg.List() {
		before[s.ID] = true
	}
	d.handleControlFrame(t.Context(), ops, wire.FrameCreateSession, []byte(payload))
	d.ops.Wait()
	for _, s := range d.reg.List() {
		if !before[s.ID] {
			return d.reg.Get(s.ID)
		}
	}
	t.Fatal("CREATE_SESSION created nothing")
	return nil
}

// SpawnedBy is the daemon's to stamp: a plugin connection's create
// carries plugin:<id>, and a spawned_by in the client's JSON is ignored
// because CreateSpec never decodes it.
func TestSpawnedByStampedFromPluginSocketNotClient(t *testing.T) {
	useFakeACP(t)
	d := newFrameTestDaemon(t)
	rec := &recordOps{}
	ops := rec.ops()
	ops.principal = principalOf(&pluginTag{id: "wf"})
	e := createACPVia(t, d, ops, `{"kind":"acp","agent":"claude","spawned_by":"session:forged"}`)
	if len(rec.errs) > 0 {
		t.Fatalf("errors: %+v", rec.errs)
	}
	var info wire.SessionInfo
	for _, s := range d.reg.List() {
		if s.ID == e.ID {
			info = s
		}
	}
	if info.SpawnedBy != "plugin:wf" || info.Kind != wire.KindACP {
		t.Errorf("SpawnedBy %q kind %q, want plugin:wf and acp", info.SpawnedBy, info.Kind)
	}

	// The user's own connection stamps nothing.
	e2 := createACPVia(t, d, (&recordOps{}).ops(), `{"kind":"acp","agent":"claude","spawned_by":"plugin:forged"}`)
	for _, s := range d.reg.List() {
		if s.ID == e2.ID && s.SpawnedBy != "" {
			t.Errorf("user create SpawnedBy = %q, want empty", s.SpawnedBy)
		}
	}
}

func TestGetAcpTranscriptUnknownSession(t *testing.T) {
	d := newFrameTestDaemon(t)
	rec := &recordOps{}
	d.handleControlFrame(t.Context(), rec.ops(), wire.FrameGetAcpTranscript, []byte(`{"session_id":"nope"}`))
	// The refusal names the session, so the client can act on its own
	// copy of it (stop showing the snapshot as loading).
	if len(rec.errs) != 1 || rec.errs[0].Code != "no_such_session" || rec.errs[0].SessionID != "nope" {
		t.Errorf("errors = %+v, want no_such_session for session nope", rec.errs)
	}
	rec = &recordOps{}
	d.handleControlFrame(t.Context(), rec.ops(), wire.FramePromptAcp, []byte(`{"session_id":"nope","text":"x"}`))
	if len(rec.errs) != 1 || rec.errs[0].Code != "no_such_session" || rec.errs[0].SessionID != "nope" {
		t.Errorf("prompt errors = %+v, want no_such_session for session nope", rec.errs)
	}
}

// Every ACP refusal carries a code the GUI's ACP_REFUSALS set (store/acp.ts)
// matches on, and names the session it was for.
func TestSendACPErrorCodes(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{registry.ErrNotFound, "no_such_session"},
		{registry.ErrNotACP, "not_acp_session"},
		{registry.ErrACPBusy, "acp_busy"},
		{registry.ErrPermissionStale, "permission_stale"},
		{registry.ErrNoLiveSession, "session_dead"},
		{errors.New("boom"), "acp_failed"},
	}
	for _, c := range cases {
		rec := &recordOps{}
		sendACPError(rec.ops(), fmt.Errorf("wrapped: %w", c.err), "s1")
		if len(rec.errs) != 1 || rec.errs[0].Code != c.code || rec.errs[0].SessionID != "s1" {
			t.Errorf("%v: errors = %+v, want %s for session s1", c.err, rec.errs, c.code)
		}
	}
	// ANSWER_PERMISSION names its session too.
	d := newFrameTestDaemon(t)
	rec := &recordOps{}
	d.handleControlFrame(t.Context(), rec.ops(), wire.FrameAnswerPermission, []byte(`{"session_id":"nope","request_id":"r","option_id":"o"}`))
	if len(rec.errs) != 1 || rec.errs[0].Code != "no_such_session" || rec.errs[0].SessionID != "nope" {
		t.Errorf("answer errors = %+v, want no_such_session for session nope", rec.errs)
	}
}

// GET_ACP_TRANSCRIPT answers through the connection's own fan-out
// listener, as a reset, and writes nothing directly.
func TestGetAcpTranscriptQueuesResetOnListener(t *testing.T) {
	useFakeACP(t)
	d := newFrameTestDaemon(t)
	e := createACPVia(t, d, (&recordOps{}).ops(), `{"kind":"acp","agent":"claude"}`)
	ch, unsub := d.reg.SubscribeACP()
	defer unsub()
	rec := &recordOps{}
	ops := rec.ops()
	ops.acpListener = ch
	d.handleControlFrame(t.Context(), ops, wire.FrameGetAcpTranscript, []byte(`{"session_id":"`+e.ID+`"}`))
	if len(rec.errs) > 0 {
		t.Fatalf("errors: %+v", rec.errs)
	}
	for {
		select {
		case msg := <-ch:
			if msg.SessionID == e.ID && msg.Reset {
				return
			}
		default:
			t.Fatal("GET_ACP_TRANSCRIPT queued no reset on the connection's listener")
		}
	}
}

// The prompt's origin is the connection's principal, or "user".
func TestPromptAcpOriginFromConnection(t *testing.T) {
	useFakeACP(t)
	d := newFrameTestDaemon(t)
	e := createACPVia(t, d, (&recordOps{}).ops(), `{"kind":"acp","agent":"claude"}`)
	ops := (&recordOps{}).ops()
	ops.principal = "plugin:wf"
	d.handleControlFrame(t.Context(), ops, wire.FramePromptAcp, []byte(`{"session_id":"`+e.ID+`","text":"hi"}`))
	msg, err := d.reg.AcpTranscript(e.ID)
	if err != nil || len(msg.Items) == 0 || msg.Items[0].Origin != "plugin:wf" {
		t.Errorf("transcript = %+v, %v; want the turn with origin plugin:wf", msg.Items, err)
	}
}

// ANSWER_PERMISSION reaches the registry and maps its errors to wire
// codes: an unknown session, an answer with nothing pending, and a
// wrong request id are refused; the pending request's id and an
// offered option clear it.
func TestAnswerPermissionFrame(t *testing.T) {
	useFakeACP(t, acptest.FlagPermission)
	d := newFrameTestDaemon(t)
	answer := func(sid, rid, opt string) []wire.Error {
		rec := &recordOps{}
		d.handleControlFrame(t.Context(), rec.ops(), wire.FrameAnswerPermission,
			[]byte(`{"session_id":"`+sid+`","request_id":"`+rid+`","option_id":"`+opt+`"}`))
		return rec.errs
	}
	if errs := answer("nope", "1", "allow"); len(errs) != 1 || errs[0].Code != "no_such_session" {
		t.Errorf("unknown session errors = %+v, want no_such_session", errs)
	}
	e := createACPVia(t, d, (&recordOps{}).ops(), `{"kind":"acp","agent":"claude"}`)
	if errs := answer(e.ID, "1", "allow"); len(errs) != 1 || errs[0].Code != wire.ErrCodePermissionStale {
		t.Errorf("nothing pending errors = %+v, want %s", errs, wire.ErrCodePermissionStale)
	}
	d.handleControlFrame(t.Context(), (&recordOps{}).ops(), wire.FramePromptAcp, []byte(`{"session_id":"`+e.ID+`","text":"hi"}`))
	var perm *wire.AcpPermission
	for deadline := time.Now().Add(5 * time.Second); perm == nil && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		msg, _ := d.reg.AcpTranscript(e.ID)
		perm = msg.Permission
	}
	if perm == nil {
		t.Fatal("no permission request became pending")
	}
	if errs := answer(e.ID, perm.RequestID+"0", "allow"); len(errs) != 1 || errs[0].Code != wire.ErrCodePermissionStale {
		t.Errorf("wrong request id errors = %+v, want %s", errs, wire.ErrCodePermissionStale)
	}
	if errs := answer(e.ID, perm.RequestID, "allow"); len(errs) != 0 {
		t.Errorf("valid answer errors = %+v, want none", errs)
	}
}

// An ACP session has no terminal: attach is refused with its own code,
// not reported as a dead session, and a create HELLO cannot make one.
func TestAttachRefusesACPSession(t *testing.T) {
	useFakeACP(t)
	d := startTestDaemon(t)
	e := createACPVia(t, d, (&recordOps{}).ops(), `{"kind":"acp","agent":"claude"}`)

	for _, hello := range []wire.Hello{
		{Mode: wire.ModeAttach, SessionID: e.ID},
		{Mode: wire.ModeCreate, Create: &wire.CreateSpec{Kind: wire.KindACP, Agent: "claude"}},
	} {
		c := dial(t, d)
		hello.Version = wire.PROTOCOL_VERSION
		if err := wire.WriteJSON(c, wire.FrameHello, hello); err != nil {
			t.Fatal(err)
		}
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		ft, payload, err := wire.ReadFrame(c)
		c.Close()
		if err != nil {
			t.Fatal(err)
		}
		var werr wire.Error
		_ = json.Unmarshal(payload, &werr)
		if ft != wire.FrameError || werr.Code != wire.ErrCodeACPSession {
			t.Errorf("%s: got %s %+v, want ERROR %s", hello.Mode, ft, werr, wire.ErrCodeACPSession)
		}
	}
}
