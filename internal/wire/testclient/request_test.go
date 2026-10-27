// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package testclient

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// Two requests in flight at once, answered in the opposite order: each
// gets its own reply, and neither consumes the other's.
func TestClient_Request_ConcurrentOutOfOrder(t *testing.T) {
	fd := newFakeDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cli, err := Dial(ctx, fd.sockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	fd.waitAccept(t)

	go func() {
		var hello wire.Hello
		_, _ = wire.ReadJSON(fd.conn, &hello)
		_ = wire.WriteJSON(fd.conn, wire.FrameWelcome, wire.Welcome{Version: wire.PROTOCOL_VERSION})
		var first, second wire.AckMsg
		read := func(into *wire.AckMsg) {
			_, p, err := wire.ReadFrame(fd.conn)
			if err != nil {
				return
			}
			var req struct {
				ReqID     int    `json:"req_id"`
				SessionID string `json:"session_id"`
			}
			_ = json.Unmarshal(p, &req)
			*into = wire.AckMsg{ReqID: req.ReqID, SessionID: req.SessionID}
		}
		read(&first)
		read(&second)
		_ = wire.WriteJSON(fd.conn, wire.FrameAck, second) // the later request first
		_ = wire.WriteJSON(fd.conn, wire.FrameAck, first)
	}()
	if _, err := cli.Handshake(wire.Hello{Mode: wire.ModeControl}); err != nil {
		t.Fatal(err)
	}

	type res struct {
		want string
		got  wire.AckMsg
		err  error
	}
	out := make(chan res, 2)
	ask := func(sid string) {
		_, p, err := cli.Request(wire.FrameKillSession, wire.KillSessionReq{SessionID: sid}, 3*time.Second)
		var a wire.AckMsg
		_ = json.Unmarshal(p, &a)
		out <- res{sid, a, err}
	}
	go ask("A")
	// Make B's request reach the daemon second.
	deadline := time.Now().Add(3 * time.Second)
	for {
		cli.mu.Lock()
		n := cli.nextReq
		cli.mu.Unlock()
		if n > requestIDBase || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	go ask("B")
	for range 2 {
		r := <-out
		if r.err != nil || r.got.SessionID != r.want {
			t.Errorf("request %s: ack %+v, err %v; want its own reply", r.want, r.got, r.err)
		}
	}
}

func TestClient_Request_MatchesReqID(t *testing.T) {
	fd := newFakeDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, err := Dial(ctx, fd.sockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	fd.waitAccept(t)

	go func() {
		var hello wire.Hello
		_, _ = wire.ReadJSON(fd.conn, &hello)
		_ = wire.WriteJSON(fd.conn, wire.FrameWelcome, wire.Welcome{Version: wire.PROTOCOL_VERSION})
		for {
			ft, p, err := wire.ReadFrame(fd.conn)
			if err != nil {
				return
			}
			var req struct {
				ReqID     int    `json:"req_id"`
				SessionID string `json:"session_id"`
			}
			_ = json.Unmarshal(p, &req)
			// Noise that must not settle the request: a broadcast, and an
			// ACK and an ERROR for some other request.
			_ = wire.WriteJSON(fd.conn, wire.FrameSessions, wire.SessionsResp{})
			_ = wire.WriteJSON(fd.conn, wire.FrameAck, wire.AckMsg{ReqID: req.ReqID + 100})
			_ = wire.WriteJSON(fd.conn, wire.FrameError, wire.Error{Code: "other", ReqID: req.ReqID + 100})
			if ft == wire.FrameKillSession && req.SessionID == "bad" {
				_ = wire.WriteJSON(fd.conn, wire.FrameError, wire.Error{Code: "kill_failed", Message: "no", ReqID: req.ReqID})
				continue
			}
			_ = wire.WriteJSON(fd.conn, wire.FrameAck, wire.AckMsg{ReqID: req.ReqID, SessionID: req.SessionID})
		}
	}()
	if _, err := cli.Handshake(wire.Hello{Mode: wire.ModeControl}); err != nil {
		t.Fatal(err)
	}

	ft, p, err := cli.Request(wire.FrameKillSession, wire.KillSessionReq{SessionID: "s1"}, 5*time.Second)
	var ack wire.AckMsg
	_ = json.Unmarshal(p, &ack)
	if err != nil || ft != wire.FrameAck || ack.SessionID != "s1" || ack.ReqID == 0 {
		t.Fatalf("Request = %s %s %v", ft, p, err)
	}
	_, _, err = cli.Request(wire.FrameKillSession, wire.KillSessionReq{SessionID: "bad"}, 5*time.Second)
	var re *RequestError
	if !errors.As(err, &re) || re.Err.Code != "kill_failed" {
		t.Fatalf("err = %v, want a RequestError kill_failed", err)
	}
}

// A req_id the test picks itself must not be taken for the reply to a
// Request in flight.
func TestClient_Request_CallerReqIDDoesNotCollide(t *testing.T) {
	fd := newFakeDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cli, err := Dial(ctx, fd.sockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	fd.waitAccept(t)

	go func() {
		var hello wire.Hello
		_, _ = wire.ReadJSON(fd.conn, &hello)
		_ = wire.WriteJSON(fd.conn, wire.FrameWelcome, wire.Welcome{Version: wire.PROTOCOL_VERSION})
		_, p, err := wire.ReadFrame(fd.conn)
		if err != nil {
			return
		}
		var req struct {
			ReqID int `json:"req_id"`
		}
		_ = json.Unmarshal(p, &req)
		// The reply to a typed sender's own small id, then the real ACK.
		_ = wire.WriteJSON(fd.conn, wire.FrameTranscriptLines, wire.TranscriptLinesMsg{ReqID: 1})
		_ = wire.WriteJSON(fd.conn, wire.FrameAck, wire.AckMsg{ReqID: req.ReqID})
	}()
	if _, err := cli.Handshake(wire.Hello{Mode: wire.ModeControl}); err != nil {
		t.Fatal(err)
	}
	if ft, p, err := cli.Request(wire.FrameKillSession, wire.KillSessionReq{SessionID: "s"}, 3*time.Second); err != nil || ft != wire.FrameAck {
		t.Fatalf("Request = %s %s %v, want the ACK, not the TRANSCRIPT_LINES for req_id 1", ft, p, err)
	}
}

// Unrelated frames piling up in the snapshot channel must not stop the read
// loop from delivering the reply: a Request does not drain that channel.
func TestClient_Request_ManyUnrelatedFramesBeforeAck(t *testing.T) {
	fd := newFakeDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cli, err := Dial(ctx, fd.sockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	fd.waitAccept(t)

	go func() {
		var hello wire.Hello
		_, _ = wire.ReadJSON(fd.conn, &hello)
		_ = wire.WriteJSON(fd.conn, wire.FrameWelcome, wire.Welcome{Version: wire.PROTOCOL_VERSION})
		_, p, err := wire.ReadFrame(fd.conn)
		if err != nil {
			return
		}
		var req struct {
			ReqID int `json:"req_id"`
		}
		_ = json.Unmarshal(p, &req)
		for range 300 { // past the snapshot channel's capacity
			_ = wire.WriteJSON(fd.conn, wire.FrameSessions, wire.SessionsResp{})
		}
		_ = wire.WriteJSON(fd.conn, wire.FrameAck, wire.AckMsg{ReqID: req.ReqID})
	}()
	if _, err := cli.Handshake(wire.Hello{Mode: wire.ModeControl}); err != nil {
		t.Fatal(err)
	}
	if ft, p, err := cli.Request(wire.FrameKillSession, wire.KillSessionReq{SessionID: "s"}, 3*time.Second); err != nil || ft != wire.FrameAck {
		t.Fatalf("Request = %s %s %v, want the ACK", ft, p, err)
	}
}
