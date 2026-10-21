package main

import (
	"bytes"
	"strings"
	"testing"
)

// Argument errors exit 2 before any socket is touched, and the flags work
// on either side of the session, as the documented form puts them after.
func TestRunWaitArguments(t *testing.T) {
	t.Setenv("HIVE_SESSION_ID", "")
	t.Setenv("HIVE_SOCKET", "")
	for _, tc := range []struct {
		name string
		args []string
		want string // substring of stderr
	}{
		{"no session", nil, "usage:"},
		{"two sessions", []string{"a", "b"}, "usage:"},
		{"unknown state", []string{"a", "--state", "asleep"}, `unknown state "asleep"`},
		{"gone is not askable", []string{"a", "--state", "idle,gone"}, `unknown state "gone"`},
		{"zero timeout", []string{"a", "--timeout", "0"}, "--timeout must be"},
		{"negative timeout", []string{"--timeout=-5s", "a"}, "--timeout must be"},
		{"over the cap", []string{"a", "--timeout", "31m"}, "--timeout must be"},
		{"unknown flag", []string{"a", "--bogus"}, "usage:"},
		{"valid but not in a session", []string{"a", "--state", "idle,exited", "--timeout", "5s", "--after-working"}, "not running inside a Hive session"},
		{"flags first", []string{"--state", "idle", "a"}, "not running inside a Hive session"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			if code := runWait(tc.args, &out, &errb); code != 2 {
				t.Fatalf("exit = %d, want 2 (stderr %q)", code, errb.String())
			}
			if !strings.Contains(errb.String(), tc.want) {
				t.Errorf("stderr = %q, want containing %q", errb.String(), tc.want)
			}
		})
	}
}

func TestRunWaitHelpExitsZero(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runWait([]string{"-h"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "--after-working") {
		t.Fatalf("exit %d, stdout %q", code, out.String())
	}
}

func TestRunSessionArguments(t *testing.T) {
	t.Setenv("HIVE_SESSION_ID", "")
	t.Setenv("HIVE_SOCKET", "")
	for _, args := range [][]string{nil, {"nope"}, {"list", "extra"}} {
		var out, errb bytes.Buffer
		if code := runSession(args, &out, &errb); code != 2 || !strings.Contains(errb.String(), "usage:") {
			t.Errorf("%v: exit %d, stderr %q", args, code, errb.String())
		}
	}
	var out, errb bytes.Buffer
	if code := runSession([]string{"list"}, &out, &errb); code != 2 || !strings.Contains(errb.String(), "not running inside a Hive session") {
		t.Errorf("outside a session: exit %d, stderr %q", code, errb.String())
	}
}
