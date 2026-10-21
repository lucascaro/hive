// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

func TestResolveTarget(t *testing.T) {
	sessions := []wire.SessionInfo{
		{ID: "aaa111", Name: "refactor"},
		{ID: "bbb222", Name: "refactor-two"},
		{ID: "ccc333", Name: "twin"},
		{ID: "ddd444", Name: "twin"},
	}
	for _, tc := range []struct {
		ref, wantID, errHas string
	}{
		{"aaa111", "aaa111", ""},
		{"refactor", "aaa111", ""},
		{"refactor-two", "bbb222", ""},
		{"refac", "", "no session"}, // no prefix matching: this types into a terminal
		{"aaa", "", "no session"},
		{"twin", "", "ccc333, ddd444"}, // ambiguous names list the ids
		{"nope", "", "no session"},
	} {
		got, err := resolveTarget(sessions, tc.ref)
		if tc.errHas != "" {
			if err == nil || !strings.Contains(err.Error(), tc.errHas) {
				t.Errorf("%q: err = %v, want containing %q", tc.ref, err, tc.errHas)
			}
			continue
		}
		if err != nil || got.ID != tc.wantID {
			t.Errorf("%q: got %q, %v; want %q", tc.ref, got.ID, err, tc.wantID)
		}
	}
}

func TestResolveTargetIDWinsOverName(t *testing.T) {
	// A session named like another's id must not hijack the id.
	got, err := resolveTarget([]wire.SessionInfo{
		{ID: "x1", Name: "x2"}, {ID: "x2", Name: "other"},
	}, "x2")
	if err != nil || got.ID != "x2" {
		t.Fatalf("got %q, %v; want the session whose ID is x2", got.ID, err)
	}
}

func TestMsgUsage(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runMsg([]string{"onlyonearg"}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "usage") {
		t.Errorf("code=%d stderr=%q, want usage and exit 2", code, errb.String())
	}
	if code := runMsg([]string{"-h"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "usage") {
		t.Errorf("-h: code=%d stdout=%q", code, out.String())
	}
}

// Inside a session `hived msg` takes the session socket (spec 389), not
// the control socket: with HIVE_SESSION_ID and HIVE_SOCKET set it must
// dial HIVE_SOCKET. A missing socket in a proper 0700 directory fails at
// that dial, naming the socket it tried, and not with a refusal.
func TestMsgInsideASessionDialsHiveSocket(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "events.sock")
	t.Setenv("HIVE_SESSION_ID", "abc")
	t.Setenv("HIVE_SOCKET", sock)
	var out, errb bytes.Buffer
	code := runMsg([]string{"a", "b"}, &out, &errb)
	if code != 2 || !strings.Contains(errb.String(), sock) || strings.Contains(errb.String(), "not available") {
		t.Errorf("code=%d stderr=%q, want a dial failure naming %s", code, errb.String(), sock)
	}
}
