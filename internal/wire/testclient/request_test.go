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
