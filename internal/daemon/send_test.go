package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// asyncOps is a controlOps whose replies land on a channel, because
// SEND_TO_SESSION answers from a runOp goroutine and recordOps is not
// safe to read from the test while that goroutine appends.
func asyncOps(principal string) (controlOps, chan wire.Error, chan wire.FrameType) {
	errs := make(chan wire.Error, 64)
	frames := make(chan wire.FrameType, 64)
	return controlOps{
		principal: principal,
		writeJSON: func(t wire.FrameType, v any) error {
			if e, ok := v.(wire.Error); ok && t == wire.FrameError {
				errs <- e
				return nil
			}
			frames <- t
			return nil
		},
		sendError: func(code, msg string) { errs <- wire.Error{Code: code, Message: msg} },
	}, errs, frames
}

func sendReq(t *testing.T, id, text string) []byte {
	t.Helper()
	b, err := json.Marshal(wire.SendToSessionReq{SessionID: id, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestSendToSessionRefusedForPlugin: typing into a PTY is the
// confused-deputy case, so a plugin principal is refused outright and
// nothing reaches the registry.
func TestSendToSessionRefusedForPlugin(t *testing.T) {
	d := newFrameTestDaemon(t)
	ops, errs, frames := asyncOps("plugin:acme")
	d.handleControlFrame(context.Background(), ops, wire.FrameSendToSession, sendReq(t, "any", "hi"))
	select {
	case e := <-errs:
		if e.Code != wire.ErrCodeModeNotAllowed {
			t.Fatalf("code = %q, want %q", e.Code, wire.ErrCodeModeNotAllowed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no refusal")
	}
	select {
	case f := <-frames:
		t.Fatalf("a plugin got %v back", f)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestSendToSessionNotInSessionModeFrames: a ModeSession connection (an
// agent inside a session) must not be offered SEND_TO_SESSION; that is
// spec 389's grant, not this spec's.
func TestSendToSessionNotInSessionModeFrames(t *testing.T) {
	if sessionModeFrames[wire.FrameSendToSession] {
		t.Fatal("SEND_TO_SESSION is in sessionModeFrames")
	}
	d := newFrameTestDaemon(t)
	rec := &recordOps{}
	o := rec.ops()
	o.restricted = true
	d.handleControlFrame(context.Background(), o, wire.FrameSendToSession, sendReq(t, "any", "hi"))
	if len(rec.errs) != 1 || rec.errs[0].Code != wire.ErrCodeModeNotAllowed {
		t.Fatalf("restricted connection got %+v, want one mode_not_allowed", rec.errs)
	}
}

// syncBuf is a log sink safe for the runOp goroutine to write while the
// test reads.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// TestSendToSessionErrorsAndLogLine: an unknown target answers
// no_such_session AND leaves the one greppable evidence line the
// orchestration phase gates count.
func TestSendToSessionErrorsAndLogLine(t *testing.T) {
	d := newFrameTestDaemon(t)
	var buf syncBuf
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })

	ops, errs, _ := asyncOps("")
	d.handleControlFrame(context.Background(), ops, wire.FrameSendToSession, sendReq(t, "ghost", "hi"))
	select {
	case e := <-errs:
		if e.Code != "no_such_session" || e.SessionID != "ghost" {
			t.Fatalf("got %+v, want no_such_session for ghost", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no answer")
	}
	want := "send_to_session caller=control from=user to=ghost delivery=no_such_session"
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(buf.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("log line %q not found in:\n%s", want, buf.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSendToSessionRejectsBadText: empty / control-only text is a
// bad_payload before anything is written.
func TestSendToSessionRejectsBadText(t *testing.T) {
	d := newFrameTestDaemon(t)
	id := bootstrapSessionID(t, d)
	ops, errs, _ := asyncOps("")
	d.handleControlFrame(context.Background(), ops, wire.FrameSendToSession, sendReq(t, id, "\x1b\x07  "))
	select {
	case e := <-errs:
		if e.Code != "bad_payload" {
			t.Fatalf("code = %q, want bad_payload", e.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no answer")
	}
}

// TestSendToSessionDoesNotBlockClose: the delivery runs through runOp, so
// a Close racing an in-flight send must neither hang nor panic.
func TestSendToSessionDoesNotBlockClose(t *testing.T) {
	d := newFrameTestDaemon(t)
	id := bootstrapSessionID(t, d)
	ops, _, _ := asyncOps("")
	for i := 0; i < 5; i++ {
		d.handleControlFrame(context.Background(), ops, wire.FrameSendToSession,
			sendReq(t, id, "echo hello"))
	}
	done := make(chan struct{})
	go func() { _ = d.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close hung with sends in flight")
	}
}
