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

// ArchiveSessionAck and RemoveWorktreeAck send their payload fields plus a
// req_id, and return the ACK that echoes it (B3-R7).
func TestClient_ArchiveAndRemoveAcks(t *testing.T) {
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
		for {
			ft, p, err := wire.ReadFrame(fd.conn)
			if err != nil {
				return
			}
			var req struct {
				ReqID         int    `json:"req_id"`
				SessionID     string `json:"session_id"`
				WorkflowRunID string `json:"workflow_run_id"`
			}
			_ = json.Unmarshal(p, &req)
			if ft == wire.FrameRemoveWorktree && req.WorkflowRunID == "" {
				_ = wire.WriteJSON(fd.conn, wire.FrameError, wire.Error{Code: wire.ErrCodeWorktreeUnpushed, ReqID: req.ReqID})
				continue
			}
			_ = wire.WriteJSON(fd.conn, wire.FrameAck, wire.AckMsg{ReqID: req.ReqID, SessionID: req.SessionID})
		}
	}()
	if _, err := cli.Handshake(wire.Hello{Mode: wire.ModeControl}); err != nil {
		t.Fatal(err)
	}

	if a, err := cli.ArchiveSessionAck("s1", 5*time.Second); err != nil || a.SessionID != "s1" || a.ReqID == 0 {
		t.Fatalf("ArchiveSessionAck = %+v, %v", a, err)
	}
	if a, err := cli.RemoveWorktreeAck(wire.RemoveWorktreeReq{ProjectID: "p", Path: "/x", WorkflowRunID: "r"}, 5*time.Second); err != nil || a.ReqID == 0 {
		t.Fatalf("RemoveWorktreeAck = %+v, %v", a, err)
	}
	_, err = cli.RemoveWorktreeAck(wire.RemoveWorktreeReq{ProjectID: "p", Path: "/x"}, 5*time.Second)
	var re *RequestError
	if !errors.As(err, &re) || re.Err.Code != wire.ErrCodeWorktreeUnpushed {
		t.Fatalf("err = %v, want a RequestError worktree_unpushed", err)
	}
}
