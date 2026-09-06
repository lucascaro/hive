package main

import (
	"bytes"
	"strings"
	"testing"
)

// Run outside a Hive session, `hived idea` must say so and exit 2 —
// not hang dialing a socket that isn't there, and not exit 0 having
// silently dropped the note.
func TestIdeaAddOutsideHiveExits2(t *testing.T) {
	t.Setenv("HIVE_SESSION_ID", "")
	t.Setenv("HIVE_SOCKET", "")

	var stdout, stderr bytes.Buffer
	if code := runIdea([]string{"add", "something"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "not running inside a Hive session") {
		t.Errorf("stderr = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestIdeaListOutsideHiveExits2(t *testing.T) {
	t.Setenv("HIVE_SESSION_ID", "")
	t.Setenv("HIVE_SOCKET", "")

	var stdout, stderr bytes.Buffer
	if code := runIdea([]string{"list"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "not running inside a Hive session") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

// Argument handling is checked before anything dials, so these fail
// the same way inside a session or out of one.
func TestIdeaArgErrors(t *testing.T) {
	t.Setenv("HIVE_SESSION_ID", "s1")
	t.Setenv("HIVE_SOCKET", "/nonexistent/hived.sock")

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no subcommand", nil, "usage:"},
		{"unknown subcommand", []string{"frobnicate"}, "unknown subcommand"},
		{"empty text", []string{"add"}, "pass the idea text"},
		{"blank text", []string{"add", "   "}, "pass the idea text"},
		{"unknown kind", []string{"add", "-k", "rant", "hi"}, "unknown kind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := runIdea(tc.args, &stdout, &stderr); code != 2 {
				t.Fatalf("exit code = %d, want 2", code)
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr = %q, want it to mention %q", stderr.String(), tc.want)
			}
		})
	}
}

func TestIdeaHelpExitsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runIdea([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "hived idea add") {
		t.Errorf("stdout = %q", stdout.String())
	}
}

// A multi-word idea is one idea, not one per word.
func TestIdeaAddJoinsArgs(t *testing.T) {
	t.Setenv("HIVE_SESSION_ID", "s1")
	t.Setenv("HIVE_SOCKET", "/nonexistent/hived.sock")

	var stdout, stderr bytes.Buffer
	// The dial fails, but only after the text has been assembled and
	// validated — which is the part under test here. A "pass the idea
	// text" error would mean the join dropped everything.
	if code := runIdea([]string{"add", "the", "grid", "loses", "focus"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "cannot reach the daemon") {
		t.Errorf("stderr = %q, want a dial failure (text assembled)", stderr.String())
	}
}

func TestOneLine(t *testing.T) {
	if got := oneLine("first\nsecond"); got != "first …" {
		t.Errorf("oneLine = %q", got)
	}
	if got := oneLine("single"); got != "single" {
		t.Errorf("oneLine = %q", got)
	}
}
