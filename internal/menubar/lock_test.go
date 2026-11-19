// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package menubar

import "testing"

func TestLockRecordRoundTrip(t *testing.T) {
	pid, build, ok := ParseLockRecord([]byte(FormatLockRecord(4242, "abc1234-dirty")))
	if !ok || pid != 4242 || build != "abc1234-dirty" {
		t.Fatalf("got (%d, %q, %v)", pid, build, ok)
	}
}

// A hivebar from before the build id wrote only its pid. It must parse
// with an empty build so the first post-update spawn replaces it.
func TestParseLockRecordPidOnly(t *testing.T) {
	pid, build, ok := ParseLockRecord([]byte("99\n"))
	if !ok || pid != 99 || build != "" {
		t.Fatalf("got (%d, %q, %v)", pid, build, ok)
	}
}

func TestParseLockRecordRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "\n", "x abc", "0 abc", "-5 abc"} {
		if _, _, ok := ParseLockRecord([]byte(in)); ok {
			t.Errorf("ParseLockRecord(%q) ok, want rejected", in)
		}
	}
}
