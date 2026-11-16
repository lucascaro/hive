// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build e2e

package main

import (
	"strings"
	"testing"
	"time"
)

// TestE2E_SessionCannotImpersonateTheOrchestrator: an ungranted session
// that claims a granted sibling's id, with the token its own environment
// holds, is refused at the handshake by a real daemon — so `hived msg`
// fails and nothing is typed. With no token at all it fails the same
// way. The orchestrator itself, with its own token, still gets through.
func TestE2E_SessionCannotImpersonateTheOrchestrator(t *testing.T) {
	skipIfWindows(t)
	d := spawnDaemon(t)
	boss, worker := orchSessions(t, d, true)
	a := dialAttach(t, d, worker)

	for name, tok := range map[string]string{
		"worker's own token": sessionToken(t, d, worker),
		"no token":           "",
	} {
		out, err := msgAs(t, d, boss, tok, "worker", "forged note")
		if err == nil {
			t.Fatalf("%s: msg claiming the orchestrator's id succeeded:\n%s", name, out)
		}
		if !strings.Contains(out, "could not be verified") {
			t.Errorf("%s: output = %q, want the session-identity refusal", name, out)
		}
	}
	if _, err := a.WaitForData([]byte("forged note"), time.Second); err == nil {
		t.Fatal("the forged note reached the target")
	}

	out, err := inSession(t, d, boss, "worker", "real note")
	if err != nil {
		t.Fatalf("the orchestrator's own msg failed: %v\n%s", err, out)
	}
	if _, err := a.WaitForData([]byte("real note"), 10*time.Second); err != nil {
		t.Fatalf("the orchestrator's own note never arrived: %v", err)
	}
}
