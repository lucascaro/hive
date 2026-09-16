package daemon

import (
	"net"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lucascaro/hive/internal/wire"
)

// reportTool pushes one activity event in over a ModeEvent connection,
// the way `hived hook` does.
func reportTool(t *testing.T, d *Daemon, ev wire.AgentEvent) {
	t.Helper()
	c := dialEvent(t, d)
	defer c.Close()
	ev.Source = wire.StateSourceHook
	if err := wire.WriteJSON(c, wire.FrameAgentEvent, ev); err != nil {
		t.Fatalf("write agent event: %v", err)
	}
}

// awaitActivity reads to the next ACTIVITY frame.
func awaitActivity(t *testing.T, c net.Conn) wire.ActivityMsg {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer func() { _ = c.SetReadDeadline(time.Time{}) }()
	for {
		ft, payload, err := wire.ReadFrame(c)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if ft != wire.FrameActivity {
			continue
		}
		var msg wire.ActivityMsg
		if err := jsonUnmarshal(payload, &msg); err != nil {
			t.Fatalf("unmarshal ACTIVITY: %v", err)
		}
		return msg
	}
}

// TestActivityBroadcastReachesControlClients: a tool event reported on
// the event socket fans out as ACTIVITY to an already-connected
// control client, through the same pub/sub every other event uses.
func TestActivityBroadcastReachesControlClients(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)

	c := dial(t, d)
	defer c.Close()
	handshake(t, c, wire.Hello{Mode: wire.ModeControl})

	reportTool(t, d, wire.AgentEvent{
		SessionID: id, Kind: wire.AgentEventToolStart,
		Tool: "Bash", Target: "npm test", CallID: "call-1",
	})

	msg := awaitActivity(t, c)
	if msg.SessionID != id {
		t.Errorf("session = %q, want %q", msg.SessionID, id)
	}
	if msg.Full {
		t.Errorf("a delta must not be marked Full")
	}
	if len(msg.Events) != 1 {
		t.Fatalf("got %d events, want 1", len(msg.Events))
	}
	if msg.Events[0].Tool != "Bash" || msg.Events[0].Target != "npm test" {
		t.Errorf("event = %+v, want Bash/npm test", msg.Events[0])
	}
}

// TestActivityPlanBroadcast: a plan event fans out the whole plan.
func TestActivityPlanBroadcast(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)

	c := dial(t, d)
	defer c.Close()
	handshake(t, c, wire.Hello{Mode: wire.ModeControl})

	reportTool(t, d, wire.AgentEvent{
		SessionID: id, Kind: wire.AgentEventPlan,
		Items: []wire.PlanItem{
			{Text: "one", Status: wire.PlanStatusDone},
			{Text: "two", Status: wire.PlanStatusActive},
		},
	})

	msg := awaitActivity(t, c)
	if len(msg.Plan) != 2 {
		t.Fatalf("got %d plan items, want 2", len(msg.Plan))
	}
	if msg.Plan[0].Status != wire.PlanStatusDone || msg.Plan[1].Status != wire.PlanStatusActive {
		t.Errorf("plan = %+v", msg.Plan)
	}
}

// TestGetActivityReturnsRing: GET_ACTIVITY answers with the whole
// stored ring and plan, marked Full so a client can tell a snapshot
// from a delta.
func TestGetActivityReturnsRing(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)

	reportTool(t, d, wire.AgentEvent{
		SessionID: id, Kind: wire.AgentEventPlan,
		Items: []wire.PlanItem{{Text: "step", Status: wire.PlanStatusActive}},
	})
	reportTool(t, d, wire.AgentEvent{
		SessionID: id, Kind: wire.AgentEventToolStart,
		Tool: "Edit", Target: "machine.go", CallID: "c1",
	})
	// Each report arrives on its own connection and the daemon serves
	// each on its own goroutine, so the end could otherwise be applied
	// BEFORE the start and never pair. In production the two hooks are
	// separated by however long the tool actually ran; here the order
	// has to be pinned explicitly. CurrentTool going non-empty is the
	// observable proof the start landed.
	waitFor(t, 2*time.Second, func() bool {
		return findSession(d, id).CurrentTool == "Edit"
	})
	reportTool(t, d, wire.AgentEvent{
		SessionID: id, Kind: wire.AgentEventToolEnd,
		Tool: "Edit", CallID: "c1",
	})

	waitFor(t, 2*time.Second, func() bool {
		msg, err := d.Registry().ActivitySnapshot(id)
		return err == nil && len(msg.Events) == 1 && len(msg.Plan) == 1
	})

	c := dial(t, d)
	defer c.Close()
	handshake(t, c, wire.Hello{Mode: wire.ModeControl})
	if err := wire.WriteJSON(c, wire.FrameGetActivity, wire.GetActivityReq{SessionID: id}); err != nil {
		t.Fatalf("write GET_ACTIVITY: %v", err)
	}

	// The connection may still be draining the initial snapshot and any
	// deltas; read to the first Full one.
	deadline := time.Now().Add(3 * time.Second)
	for {
		msg := awaitActivity(t, c)
		if !msg.Full {
			if time.Now().After(deadline) {
				t.Fatal("no Full ACTIVITY arrived")
			}
			continue
		}
		if len(msg.Events) != 1 {
			t.Fatalf("got %d events, want 1", len(msg.Events))
		}
		if msg.Events[0].Target != "machine.go" {
			t.Errorf("target = %q, want machine.go", msg.Events[0].Target)
		}
		if len(msg.Plan) != 1 || msg.Plan[0].Text != "step" {
			t.Errorf("plan = %+v, want one item 'step'", msg.Plan)
		}
		return
	}
}

// TestSessionModeCannotGetActivity is the spec's authorization
// criterion. GET_ACTIVITY is deliberately absent from
// sessionModeFrames, so a ModeSession connection — the one kind a
// program running INSIDE a session can open — is refused outright
// rather than being served its own scoped view. The only in-session
// client today is `hive idea`, nothing there consumes activity, and
// the derived labels are the most sensitive thing this feature
// carries.
func TestSessionModeCannotGetActivity(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)

	reportTool(t, d, wire.AgentEvent{
		SessionID: id, Kind: wire.AgentEventToolStart,
		Tool: "Bash", Target: "secret-ish", CallID: "c1",
	})

	c := dialEvents(t, d)
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "test/0",
		Mode: wire.ModeSession, SessionID: id,
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	// Its OWN session's activity, which is the most permissive thing it
	// could ask for — and still refused.
	if err := wire.WriteJSON(c, wire.FrameGetActivity, wire.GetActivityReq{SessionID: id}); err != nil {
		t.Fatalf("write GET_ACTIVITY: %v", err)
	}
	if err := awaitModeNotAllowed(t, c); err != nil {
		t.Fatalf("want mode_not_allowed: %v", err)
	}
}

// TestSessionModeGetsNoActivityBroadcast: refusing the request verb
// would be pointless if a restricted connection could sit on the
// socket and harvest the deltas instead — the same reasoning that
// scopes idea events.
func TestSessionModeGetsNoActivityBroadcast(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)

	c := dialEvents(t, d)
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "test/0",
		Mode: wire.ModeSession, SessionID: id,
	}); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	reportTool(t, d, wire.AgentEvent{
		SessionID: id, Kind: wire.AgentEventToolStart,
		Tool: "Bash", Target: "npm test", CallID: "c1",
	})

	// Read everything that arrives in a bounded window; none of it may
	// be ACTIVITY.
	_ = c.SetReadDeadline(time.Now().Add(700 * time.Millisecond))
	for {
		ft, _, err := wire.ReadFrame(c)
		if err != nil {
			return // deadline or close: nothing more is coming
		}
		if ft == wire.FrameActivity {
			t.Fatal("a ModeSession connection received an ACTIVITY frame")
		}
	}
}

// TestSessionInfoCarriesPlanSummary: the sidebar renders a row from
// these three fields alone and never reads the ring, so they have to
// ride the snapshot every client already receives.
func TestSessionInfoCarriesPlanSummary(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)

	reportTool(t, d, wire.AgentEvent{
		SessionID: id, Kind: wire.AgentEventPlan,
		Items: []wire.PlanItem{
			{Text: "one", Status: wire.PlanStatusDone},
			{Text: "two", Status: wire.PlanStatusDone},
			{Text: "three", Status: wire.PlanStatusActive},
		},
	})
	reportTool(t, d, wire.AgentEvent{
		SessionID: id, Kind: wire.AgentEventToolStart,
		Tool: "WebFetch", Target: "example.com", CallID: "c1",
	})

	waitFor(t, 2*time.Second, func() bool {
		info := findSession(d, id)
		return info.PlanDone == 2 && info.PlanTotal == 3 && info.CurrentTool == "WebFetch"
	})
}

// TestSessionInfoCarriesSubagentsRunning drives the count over the
// events socket, which proves the registry copies AgentID and
// RunningAgents into the machine: a subagent_start raises it, and a
// turn_end listing no running subagents reconciles it back to 0.
func TestSessionInfoCarriesSubagentsRunning(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	past := time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)

	reportTool(t, d, wire.AgentEvent{SessionID: id, Kind: wire.AgentEventSubagentStart, AgentID: "a1", AgentType: "general-purpose", At: past})
	waitFor(t, 2*time.Second, func() bool { return findSession(d, id).SubagentsRunning == 1 })

	none := []string{}
	reportTool(t, d, wire.AgentEvent{SessionID: id, Kind: wire.AgentEventTurnEnd, RunningAgents: &none})
	waitFor(t, 2*time.Second, func() bool {
		info := findSession(d, id)
		return info.SubagentsRunning == 0 && info.State == wire.StateWaitingInput
	})
}

// TestActivityDurationFromDaemonClock: the daemon times the pair
// itself, so a reporter whose clock is an hour out cannot produce a
// nonsense duration.
func TestActivityDurationFromDaemonClock(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)

	reportTool(t, d, wire.AgentEvent{
		SessionID: id, Kind: wire.AgentEventToolStart,
		Tool: "Bash", CallID: "c1",
		At: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
	})
	// Pin the start before the end: each report is its own connection
	// and goroutine. See TestGetActivityReturnsRing.
	waitFor(t, 2*time.Second, func() bool {
		return findSession(d, id).CurrentTool == "Bash"
	})
	reportTool(t, d, wire.AgentEvent{
		SessionID: id, Kind: wire.AgentEventToolEnd,
		Tool: "Bash", CallID: "c1",
		At: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
	})

	waitFor(t, 2*time.Second, func() bool {
		msg, err := d.Registry().ActivitySnapshot(id)
		if err != nil || len(msg.Events) != 1 {
			return false
		}
		// Real elapsed time here is milliseconds, not an hour.
		return msg.Events[0].DurationMS < 60_000
	})
}

// TestEventModeCapsActivityFields: Text and Target were bounded at the
// daemon, but Tool, CallID and plan item IDs were not, so anything able
// to write to the events socket could store a frame's worth per field in
// every ring entry and have every client receive it again. The daemon is
// the trust boundary; these are bounded there.
func TestEventModeCapsActivityFields(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	huge := strings.Repeat("x", 8192) // well past every cap, well under MaxPayload

	reportTool(t, d, wire.AgentEvent{
		SessionID: id, Kind: wire.AgentEventToolStart,
		Tool: huge, CallID: huge, Target: "t",
	})
	reportTool(t, d, wire.AgentEvent{
		SessionID: id, Kind: wire.AgentEventPlanItem,
		Items: []wire.PlanItem{{ID: huge, Text: "step", Status: wire.PlanStatusActive}},
	})

	waitFor(t, 2*time.Second, func() bool {
		info := findSession(d, id)
		msg, err := d.Registry().ActivitySnapshot(id)
		return info.CurrentTool != "" && err == nil && len(msg.Plan) == 1
	})

	if got := len(findSession(d, id).CurrentTool); got > wire.MaxToolNameLen {
		t.Errorf("CurrentTool is %d bytes, want <= %d", got, wire.MaxToolNameLen)
	}
	msg, _ := d.Registry().ActivitySnapshot(id)
	if got := len(msg.Plan[0].ID); got > wire.MaxActivityIDLen {
		t.Errorf("plan item ID is %d bytes, want <= %d", got, wire.MaxActivityIDLen)
	}

	// The call ID is only observable once the call ends and lands in the
	// ring. End it under the SAME capped ID it was stored with.
	reportTool(t, d, wire.AgentEvent{SessionID: id, Kind: wire.AgentEventToolEnd, Tool: "t", CallID: huge})
	waitFor(t, 2*time.Second, func() bool {
		m, err := d.Registry().ActivitySnapshot(id)
		return err == nil && len(m.Events) == 1
	})
	msg, _ = d.Registry().ActivitySnapshot(id)
	if got := len(msg.Events[0].CallID); got > wire.MaxActivityIDLen {
		t.Errorf("CallID is %d bytes, want <= %d", got, wire.MaxActivityIDLen)
	}
	if msg.Events[0].StartedAt == "" {
		t.Error("the capped end did not pair with the capped start: the cap must be applied identically to both")
	}
}

// TestEventModeCapsSubagentFields: AgentID, AgentType and RunningAgents
// are reporter-supplied like Tool and CallID, and are bounded at the
// same trust boundary for the same reason.
func TestEventModeCapsSubagentFields(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	huge := strings.Repeat("x", 8192)

	reportTool(t, d, wire.AgentEvent{SessionID: id, Kind: wire.AgentEventToolStart, Tool: "Bash", CallID: "c1", AgentID: huge, AgentType: huge})
	reportTool(t, d, wire.AgentEvent{SessionID: id, Kind: wire.AgentEventToolEnd, Tool: "Bash", CallID: "c1", AgentID: huge, AgentType: huge})
	waitFor(t, 2*time.Second, func() bool {
		m, err := d.Registry().ActivitySnapshot(id)
		return err == nil && len(m.Events) == 1
	})
	msg, _ := d.Registry().ActivitySnapshot(id)
	if got := len(msg.Events[0].AgentID); got == 0 || got > wire.MaxActivityIDLen {
		t.Errorf("AgentID is %d bytes, want 1..%d", got, wire.MaxActivityIDLen)
	}
	if got := len(msg.Events[0].AgentType); got == 0 || got > wire.MaxToolNameLen {
		t.Errorf("AgentType is %d bytes, want 1..%d", got, wire.MaxToolNameLen)
	}

	if got := capRunningAgents(nil); got != nil {
		t.Errorf("capRunningAgents(nil) = %v, want nil", *got)
	}
	many := make([]string, 1000)
	for i := range many {
		many[i] = huge
	}
	got := capRunningAgents(&many)
	if got == nil || len(*got) > wire.MaxRunningAgents {
		t.Fatalf("capRunningAgents kept %v entries, want <= %d", got, wire.MaxRunningAgents)
	}
	for _, a := range *got {
		if len(a) > wire.MaxActivityIDLen {
			t.Fatalf("a running agent id is %d bytes, want <= %d", len(a), wire.MaxActivityIDLen)
		}
	}
}

// TestEventModeCapsTargetRuneSafe: Target used to be cut by byte count,
// so a multi-byte label could reach the ring (and every client) ending in
// half a rune.
func TestEventModeCapsTargetRuneSafe(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	// One ASCII byte shifts every rune boundary off the cap.
	target := "x" + strings.Repeat("世", wire.MaxTargetLen)

	reportTool(t, d, wire.AgentEvent{SessionID: id, Kind: wire.AgentEventToolStart, Tool: "Read", CallID: "c1", Target: target})
	reportTool(t, d, wire.AgentEvent{SessionID: id, Kind: wire.AgentEventToolEnd, Tool: "Read", CallID: "c1"})
	waitFor(t, 2*time.Second, func() bool {
		m, err := d.Registry().ActivitySnapshot(id)
		return err == nil && len(m.Events) == 1
	})
	msg, _ := d.Registry().ActivitySnapshot(id)
	got := msg.Events[0].Target
	if len(got) > wire.MaxTargetLen || !utf8.ValidString(got) {
		t.Errorf("Target = %d bytes, valid UTF-8 %v; want <= %d valid bytes", len(got), utf8.ValidString(got), wire.MaxTargetLen)
	}
}

func TestCapBytesRuneSafe(t *testing.T) {
	s := strings.Repeat("世", 100) // 3 bytes per rune
	got := capBytes(s, 10)
	if len(got) > 10 || !utf8.ValidString(got) {
		t.Errorf("capBytes = %q (%d bytes), want <= 10 valid UTF-8 bytes", got, len(got))
	}
	if capBytes("short", 10) != "short" {
		t.Error("a string under the cap must be unchanged")
	}
}
