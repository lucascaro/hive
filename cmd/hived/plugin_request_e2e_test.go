// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build e2e

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The SDK's request() against a real hived (spec 528): CREATE_SESSION
// answers with the new session's id, KILL_SESSION acks, and a refusal
// rejects with the daemon's error code.
func TestE2E_SDKRequest_CreateSessionReturnsID(t *testing.T) {
	requireNode(t)
	d := spawnDaemon(t)
	script := `import { connect } from './hive-plugin.mjs';
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
const out = (name, v) => writeFileSync(join(process.env.HIVE_PLUGIN_DATA_DIR, name), v);
const hive = await connect();
const ack = await hive.request('CREATE_SESSION', { shell: '/bin/sh', name: 'from-sdk' });
out('created', ack.session_id);
const killed = await hive.request('KILL_SESSION', { session_id: ack.session_id, force: true });
out('killed', killed.session_id);
try {
  await hive.request('PROMPT_ACP', { session_id: 'nope', text: 'x' });
} catch (e) {
  out('refused', e.hive.code);
}
`
	c := dialControl(t, d)
	installEnable(t, c, writeFixturePlugin(t, "requester", script), "requester")
	dataDir := filepath.Join(d.stateDir, "plugin-data", "requester")
	read := func(name string) string {
		deadline := time.Now().Add(15 * time.Second)
		for {
			if b, err := os.ReadFile(filepath.Join(dataDir, name)); err == nil {
				return strings.TrimSpace(string(b))
			}
			if time.Now().After(deadline) {
				b, _ := os.ReadFile(filepath.Join(dataDir, "plugin.log"))
				t.Fatalf("plugin never wrote %q; log:\n%s", name, b)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	created, killed := read("created"), read("killed")
	if created == "" || created != killed {
		t.Errorf("created %q, killed %q: want the same non-empty id", created, killed)
	}
	if got := read("refused"); got != "no_such_session" {
		t.Errorf("refusal code = %q, want no_such_session", got)
	}
}
