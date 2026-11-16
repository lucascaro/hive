// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package registry

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// envToken is the HIVE_SESSION_TOKEN in a spawn's environment.
func envToken(t *testing.T, env []string) string {
	t.Helper()
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "HIVE_SESSION_TOKEN="); ok {
			return v
		}
	}
	t.Fatalf("spawn env has no HIVE_SESSION_TOKEN: %v", env)
	return ""
}

// Every spawn hands its session a fresh token that only it holds: two
// sessions never share one, a restart replaces it (so the old process
// tree stops verifying), and it never leaves the daemon's memory —
// not in SessionInfo, not on disk.
func TestSessionTokenPerSpawn(t *testing.T) {
	skipOnWindows(t)
	rec := captureStartSession(t)
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	spawned := func() string {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return envToken(t, rec.opts[len(rec.opts)-1].Env)
	}
	a, err := r.Create(context.Background(), wire.CreateSpec{Name: "a", Cmd: []string{"/bin/cat"}})
	if err != nil {
		t.Fatal(err)
	}
	tokA := spawned()
	b, err := r.Create(context.Background(), wire.CreateSpec{Name: "b", Cmd: []string{"/bin/cat"}})
	if err != nil {
		t.Fatal(err)
	}
	tokB := spawned()

	if len(tokA) != 64 || tokA == tokB {
		t.Fatalf("tokens %q / %q: want two distinct 32-byte hex tokens", tokA, tokB)
	}
	if !r.VerifySessionToken(a.ID, tokA) || !r.VerifySessionToken(b.ID, tokB) {
		t.Fatal("a session's own spawn token does not verify")
	}
	for name, ok := range map[string]bool{
		"other session's token": r.VerifySessionToken(a.ID, tokB),
		"empty token":           r.VerifySessionToken(a.ID, ""),
		"unknown id":            r.VerifySessionToken("no-such-session", tokA),
		"unknown id, empty":     r.VerifySessionToken("no-such-session", ""),
	} {
		if ok {
			t.Errorf("%s verified", name)
		}
	}

	if err := r.Restart(a.ID); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	tokA2 := spawned()
	if tokA2 == tokA || r.VerifySessionToken(a.ID, tokA) || !r.VerifySessionToken(a.ID, tokA2) {
		t.Fatalf("restart kept the old token verifying (old %q, new %q)", tokA, tokA2)
	}

	for _, info := range r.List() {
		js, _ := json.Marshal(info)
		for _, tok := range []string{tokA, tokA2, tokB} {
			if strings.Contains(string(js), tok) {
				t.Fatalf("SessionInfo carries a token: %s", js)
			}
		}
	}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, _ := os.ReadFile(p)
		for _, tok := range []string{tokA, tokA2, tokB} {
			if strings.Contains(string(raw), tok) {
				t.Errorf("%s persists a session token", p)
			}
		}
		return nil
	})
}
