// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package plugin

import "testing"

func TestManifest_NodeFieldValidated(t *testing.T) {
	base := func(node string, main bool) Manifest {
		m := Manifest{ID: "x", Name: "X", Version: "1", APIVersion: APIVersion, Node: node,
			UI: &UIEntry{Entry: "ui.mjs"}}
		if main {
			m.Main = &Entry{Command: []string{"node", "main.mjs"}}
		}
		return m
	}
	for _, ok := range []string{"", "26.0.0", "22.13.1", "0.0.1"} {
		if err := base(ok, true).Validate(); err != nil {
			t.Errorf("node %q: %v", ok, err)
		}
	}
	for _, bad := range []string{"26", "26.0", "v26.0.0", "26.0.0-rc.1", "26.x.0", "026.0.0", "-1.0.0", "99999999999999999999.0.0", "26.0.0.0", " 26.0.0"} {
		if err := base(bad, true).Validate(); err == nil {
			t.Errorf("node %q accepted", bad)
		}
	}
	if err := base("26.0.0", false).Validate(); err == nil {
		t.Error(`"node" without "main" accepted`)
	}
}

func TestParseNodeVersion(t *testing.T) {
	for in, want := range map[string][3]int{"v24.11.0\n": {24, 11, 0}, "26.0.0": {26, 0, 0}, "v26.1.2-rc.1": {26, 1, 2}} {
		if got, ok := parseNodeVersion(in); !ok || got != want {
			t.Errorf("parseNodeVersion(%q) = %v, %v", in, got, ok)
		}
	}
}
