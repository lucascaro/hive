package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// addPin sends ADD_PIN on c and returns the reply frame.
func addPin(t *testing.T, c net.Conn, title, body string) (wire.FrameType, []byte) {
	t.Helper()
	if err := wire.WriteJSON(c, wire.FrameAddPin, wire.AddPinReq{Title: title, Body: body}); err != nil {
		t.Fatal(err)
	}
	return awaitFrame(t, c, wire.FrameAddPinOK, wire.FrameError)
}

func errCode(t *testing.T, payload []byte) string {
	t.Helper()
	var e wire.Error
	if err := json.Unmarshal(payload, &e); err != nil {
		t.Fatal(err)
	}
	return e.Code
}

// getPins collects every PINS page for id on c.
func getPins(t *testing.T, c net.Conn, id string) (pages []wire.PinsMsg) {
	t.Helper()
	if err := wire.WriteJSON(c, wire.FrameGetPins, wire.GetPinsReq{SessionID: id}); err != nil {
		t.Fatal(err)
	}
	for {
		ft, payload := awaitFrame(t, c, wire.FramePins, wire.FrameError)
		if ft == wire.FrameError {
			t.Fatalf("GET_PINS: %s", payload)
		}
		var m wire.PinsMsg
		if err := json.Unmarshal(payload, &m); err != nil {
			t.Fatal(err)
		}
		if m.SessionID != id {
			continue
		}
		pages = append(pages, m)
		if m.Done {
			return pages
		}
	}
}

func secondSession(t *testing.T, d *Daemon) string {
	t.Helper()
	e, err := d.Registry().Create(context.Background(), wire.CreateSpec{Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return e.ID
}

// The payload names no session; the pin lands in the HELLO's session,
// whatever else the client tries to smuggle in.
func TestAddPinOnSessionModeUsesHelloSession(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	a := bootstrapSessionID(t, d)
	b := secondSession(t, d)

	c := sessionConn(t, d, a)
	defer c.Close()
	// A forged session_id in the payload is ignored by the decoder.
	raw := fmt.Sprintf(`{"session_id":%q,"title":"t","body":"b"}`, b)
	if err := wire.WriteFrame(c, wire.FrameAddPin, []byte(raw)); err != nil {
		t.Fatal(err)
	}
	if ft, p := awaitFrame(t, c, wire.FrameAddPinOK, wire.FrameError); ft != wire.FrameAddPinOK {
		t.Fatalf("ADD_PIN = %s %s", ft, p)
	}
	if pins, _ := d.Registry().Pins(a); len(pins) != 1 {
		t.Errorf("caller's session has %d pins, want 1", len(pins))
	}
	if pins, _ := d.Registry().Pins(b); len(pins) != 0 {
		t.Errorf("other session got %d pins", len(pins))
	}
}

func TestAddPinRejectsOversizeWithCode(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	c := sessionConn(t, d, id)
	defer c.Close()
	ft, p := addPin(t, c, "t", strings.Repeat("x", wire.MaxPinBodyLen+1))
	if ft != wire.FrameError || errCode(t, p) != wire.ErrCodePinTooLong {
		t.Fatalf("oversize = %s %s", ft, p)
	}
	ft, p = addPin(t, c, "", "b")
	if ft != wire.FrameError || errCode(t, p) != wire.ErrCodePinInvalid {
		t.Fatalf("empty title = %s %s", ft, p)
	}
}

func TestAddPinAtLimitIsPinLimit(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	for range wire.MaxPinsPerSession {
		if _, err := d.Registry().AddPin(id, "t", "b"); err != nil {
			t.Fatal(err)
		}
	}
	c := sessionConn(t, d, id)
	defer c.Close()
	if ft, p := addPin(t, c, "one too many", "b"); ft != wire.FrameError || errCode(t, p) != wire.ErrCodePinLimit {
		t.Fatalf("pin past the limit = %s %s", ft, p)
	}
}

func TestAddPinRequiresValidToken(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	c := dialEvents(t, d)
	defer c.Close()
	if err := wire.WriteJSON(c, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION, Client: "test/0",
		Mode: wire.ModeSession, SessionID: id, SessionToken: "forged",
	}); err != nil {
		t.Fatal(err)
	}
	var e wire.Error
	if ft, err := wire.ReadJSON(c, &e); err != nil || ft != wire.FrameError || e.Code != wire.ErrCodeSessionAuthFailed {
		t.Fatalf("forged token: %s %v %+v", ft, err, e)
	}
	if pins, _ := d.Registry().Pins(id); len(pins) != 0 {
		t.Fatal("a forged token pinned")
	}
}

func TestAddPinRefusedOnControl(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	c := controlConn(t, d)
	ft, p := addPin(t, c, "t", "b")
	if ft != wire.FrameError || errCode(t, p) != wire.ErrCodeModeNotAllowed {
		t.Fatalf("ADD_PIN on control = %s %s", ft, p)
	}
}

func TestSessionModeRefusesPinControlVerbs(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	if _, err := d.Registry().AddPin(id, "t", "b"); err != nil {
		t.Fatal(err)
	}
	pins, _ := d.Registry().Pins(id)
	cases := []struct {
		ft      wire.FrameType
		payload any
	}{
		{wire.FrameGetPins, wire.GetPinsReq{SessionID: id}},
		{wire.FrameGetPinBody, wire.GetPinBodyReq{SessionID: id, ID: pins[0].ID}},
		{wire.FrameUnpin, wire.UnpinReq{SessionID: id, ID: pins[0].ID}},
		{wire.FrameUnpinAll, wire.UnpinAllReq{SessionID: id}},
	}
	for _, tc := range cases {
		c := sessionConn(t, d, id)
		if err := wire.WriteJSON(c, tc.ft, tc.payload); err != nil {
			t.Fatal(err)
		}
		if err := awaitModeNotAllowed(t, c); err != nil {
			t.Errorf("%s on a session connection: %v", tc.ft, err)
		}
		_ = c.Close()
	}
	if got, _ := d.Registry().Pins(id); len(got) != 1 {
		t.Fatalf("pins changed through a session connection: %d", len(got))
	}
}

func TestPinMutationsRefusedForPlugins(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	if _, err := d.Registry().AddPin(id, "t", "b"); err != nil {
		t.Fatal(err)
	}
	pins, _ := d.Registry().Pins(id)
	c, _ := pluginConn(t, d, "p1", "plugin:p1")
	for _, tc := range []struct {
		ft      wire.FrameType
		payload any
	}{
		{wire.FrameUnpin, wire.UnpinReq{SessionID: id, ID: pins[0].ID}},
		{wire.FrameUnpinAll, wire.UnpinAllReq{SessionID: id}},
		{wire.FrameAddPin, wire.AddPinReq{Title: "t", Body: "b"}},
	} {
		if err := wire.WriteJSON(c, tc.ft, tc.payload); err != nil {
			t.Fatal(err)
		}
		ft, p := awaitFrame(t, c, wire.FrameError)
		if ft != wire.FrameError || errCode(t, p) != wire.ErrCodeModeNotAllowed {
			t.Errorf("%s from a plugin = %s %s", tc.ft, ft, p)
		}
	}
	// Reading stays available, as for the app.
	if pages := getPins(t, c, id); len(pages) != 1 || len(pages[0].Pins) != 1 {
		t.Errorf("plugin GET_PINS = %+v", pages)
	}
	if got, _ := d.Registry().Pins(id); len(got) != 1 {
		t.Fatal("a plugin removed a pin")
	}
}

// A session at the pin limit is answered in pages of at most PinsPageSize
// headers, done on the last, every page under MaxPayload.
func TestGetPinsPagesUnderMaxPayload(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	const n = wire.MaxPinsPerSession
	title := strings.Repeat("t", wire.MaxPinTitleLen)
	for range n {
		if _, err := d.Registry().AddPin(id, title, "b"); err != nil {
			t.Fatal(err)
		}
	}
	c := controlConn(t, d)
	pages := getPins(t, c, id)
	if len(pages) != n/wire.PinsPageSize {
		t.Fatalf("%d pages, want %d", len(pages), n/wire.PinsPageSize)
	}
	var ids []string
	for i, p := range pages {
		if len(p.Pins) > wire.PinsPageSize {
			t.Errorf("page %d has %d headers", i, len(p.Pins))
		}
		if b, _ := json.Marshal(p); len(b) > wire.MaxPayload {
			t.Errorf("page %d is %d bytes", i, len(b))
		}
		if p.Done != (i == len(pages)-1) {
			t.Errorf("page %d done=%v", i, p.Done)
		}
		for _, h := range p.Pins {
			ids = append(ids, h.ID)
		}
	}
	if len(ids) != n {
		t.Fatalf("%d headers, want %d", len(ids), n)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] <= ids[i] {
			t.Fatalf("not newest first at %d", i)
		}
	}
}

func TestPinPagesExactMultipleAndEmpty(t *testing.T) {
	mk := func(n int) []wire.Pin { return make([]wire.Pin, n) }
	if p := pinPages("s", mk(2*wire.PinsPageSize)); len(p) != 2 || !p[1].Done || p[0].Done || len(p[1].Pins) != wire.PinsPageSize {
		t.Errorf("exact multiple: %d pages", len(p))
	}
	if p := pinPages("s", nil); len(p) != 1 || !p[0].Done || p[0].Pins == nil {
		t.Errorf("empty: %+v", p)
	}
	if p := pinPages("s", mk(1)); len(p) != 1 || !p[0].Done {
		t.Errorf("one: %+v", p)
	}
}

func TestPinsChangedBroadcastToControlNotSession(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	ctl := controlConn(t, d)
	sess := sessionConn(t, d, id)
	defer sess.Close()
	other := sessionConn(t, d, id)
	defer other.Close()

	if ft, p := addPin(t, sess, "t", "b"); ft != wire.FrameAddPinOK {
		t.Fatalf("ADD_PIN = %s %s", ft, p)
	}
	_, payload := awaitFrame(t, ctl, wire.FramePinsChanged)
	var m wire.PinsChangedMsg
	_ = json.Unmarshal(payload, &m)
	if m.SessionID != id || m.Added == nil || m.Added.Title != "t" {
		t.Fatalf("delta = %+v", m)
	}
	// The other session connection hears nothing of it.
	_ = other.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	for {
		ft, _, err := wire.ReadFrame(other)
		if err != nil {
			break
		}
		if ft == wire.FramePinsChanged {
			t.Fatal("a session connection received PINS_CHANGED")
		}
	}
}

func TestSessionCannotReadOtherSessionPins(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	a := bootstrapSessionID(t, d)
	b := secondSession(t, d)
	pa, err := d.Registry().AddPin(a, "a's", "secret")
	if err != nil {
		t.Fatal(err)
	}
	ctl := controlConn(t, d)
	// Asking for a's pin through session b is not found, never a's body.
	if err := wire.WriteJSON(ctl, wire.FrameGetPinBody, wire.GetPinBodyReq{SessionID: b, ID: pa.ID}); err != nil {
		t.Fatal(err)
	}
	ft, p := awaitFrame(t, ctl, wire.FramePinBody, wire.FrameError)
	if ft != wire.FrameError || errCode(t, p) != wire.ErrCodeNoSuchPin {
		t.Fatalf("cross-session body = %s %s", ft, p)
	}
	if err := wire.WriteJSON(ctl, wire.FrameUnpin, wire.UnpinReq{SessionID: b, ID: pa.ID}); err != nil {
		t.Fatal(err)
	}
	if ft, p := awaitFrame(t, ctl, wire.FrameError); errCode(t, p) != wire.ErrCodeNoSuchPin {
		t.Fatalf("cross-session unpin = %s %s", ft, p)
	}
	if pins, _ := d.Registry().Pins(a); len(pins) != 1 {
		t.Fatal("a's pin was removed through b")
	}
	if pages := getPins(t, ctl, b); len(pages[0].Pins) != 0 {
		t.Fatalf("b lists a's pins: %+v", pages)
	}
}

func TestUnpinAndUnpinAllOverControl(t *testing.T) {
	skipOnWindows(t)
	d := startTestDaemon(t)
	id := bootstrapSessionID(t, d)
	p1, _ := d.Registry().AddPin(id, "one", "b")
	_, _ = d.Registry().AddPin(id, "two", "b")
	ctl := controlConn(t, d)
	if err := wire.WriteJSON(ctl, wire.FrameUnpin, wire.UnpinReq{SessionID: id, ID: p1.ID}); err != nil {
		t.Fatal(err)
	}
	awaitFrame(t, ctl, wire.FramePinsChanged)
	if pins, _ := d.Registry().Pins(id); len(pins) != 1 || pins[0].Title != "two" {
		t.Fatalf("after UNPIN: %+v", pins)
	}
	if err := wire.WriteJSON(ctl, wire.FrameUnpinAll, wire.UnpinAllReq{SessionID: id}); err != nil {
		t.Fatal(err)
	}
	_, payload := awaitFrame(t, ctl, wire.FramePinsChanged)
	var m wire.PinsChangedMsg
	_ = json.Unmarshal(payload, &m)
	if !m.Cleared {
		t.Fatalf("delta = %+v", m)
	}
}

func TestPinsSurviveDaemonRestart(t *testing.T) {
	skipOnWindows(t)
	tmp := shortTempDir(t)
	cfg := Config{
		SocketPath:       filepath.Join(tmp, "s"),
		StateDir:         filepath.Join(tmp, "state"),
		BootstrapSession: session.Options{Shell: "/bin/bash", Cols: 80, Rows: 24},
	}
	start := func() (*Daemon, context.CancelFunc) {
		d, err := New(cfg)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		go func() { _ = d.Run(ctx) }()
		return d, cancel
	}
	d1, cancel1 := start()
	id := bootstrapSessionID(t, d1)
	c := sessionConn(t, d1, id)
	if ft, p := addPin(t, c, "survivor", "the body"); ft != wire.FrameAddPinOK {
		t.Fatalf("ADD_PIN = %s %s", ft, p)
	}
	_ = c.Close()
	cancel1()
	_ = d1.Close()

	d2, cancel2 := start()
	t.Cleanup(func() { cancel2(); _ = d2.Close() })
	ctl := controlConn(t, d2)
	pages := getPins(t, ctl, id)
	if len(pages) != 1 || len(pages[0].Pins) != 1 || pages[0].Pins[0].Title != "survivor" {
		t.Fatalf("after restart: %+v", pages)
	}
	if err := wire.WriteJSON(ctl, wire.FrameGetPinBody, wire.GetPinBodyReq{SessionID: id, ID: pages[0].Pins[0].ID}); err != nil {
		t.Fatal(err)
	}
	_, payload := awaitFrame(t, ctl, wire.FramePinBody)
	var b wire.PinBodyMsg
	_ = json.Unmarshal(payload, &b)
	if b.Body != "the body" {
		t.Fatalf("body after restart = %q", b.Body)
	}
	for _, s := range d2.Registry().List() {
		if s.ID == id && s.PinCount != 1 {
			t.Errorf("pin_count after restart = %d", s.PinCount)
		}
	}
}
