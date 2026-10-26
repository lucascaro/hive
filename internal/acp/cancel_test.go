// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package acp

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/acp/acptest"
)

func TestCancel_SendsNotification(t *testing.T) {
	dir := t.TempDir()
	rec := &recorder{}
	a := startFake(t, dir, rec.handler(func(PermissionRequest) any { return Cancelled() }), acptest.FlagBlock)
	if _, err := a.Initialize(ctx(t)); err != nil {
		t.Fatal(err)
	}
	ns, _ := a.NewSession(ctx(t), dir, nil, nil)
	type res struct {
		stop string
		err  error
	}
	done := make(chan res, 1)
	go func() {
		stop, err := a.Prompt(ctx(t), ns.SessionID, "work")
		done <- res{stop, err}
	}()
	// Wait until the prompt is in flight, then cancel it mid-turn.
	deadline := time.Now().Add(10 * time.Second)
	for !slices.Contains(acptest.Calls(dir), "session/prompt "+ns.SessionID) {
		if time.Now().After(deadline) {
			t.Fatal("prompt never reached the agent")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := a.Cancel(ns.SessionID); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.err != nil || r.stop != "cancelled" {
			t.Fatalf("Prompt = %q, %v; want cancelled", r.stop, r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("prompt did not end after cancel")
	}
	if !slices.Contains(acptest.Calls(dir), "session/cancel "+ns.SessionID) {
		t.Errorf("calls = %q; session/cancel missing", acptest.Calls(dir))
	}
}

func TestUpdateRawKeptForUsage(t *testing.T) {
	dir := t.TempDir()
	rec := &recorder{}
	a := startFake(t, dir, rec.handler(func(PermissionRequest) any { return Cancelled() }))
	if _, err := a.Initialize(ctx(t)); err != nil {
		t.Fatal(err)
	}
	ns, _ := a.NewSession(ctx(t), dir, nil, nil)
	if _, err := a.Prompt(ctx(t), ns.SessionID, "hi"); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for _, u := range rec.updates {
		if u.SessionUpdate == UpdateUsage {
			if !strings.Contains(string(u.Raw), `"size":1000`) {
				t.Errorf("usage Raw = %s", u.Raw)
			}
			return
		}
	}
	t.Error("no usage_update delivered")
}
