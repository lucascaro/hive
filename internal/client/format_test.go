package client

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

func TestFormatSessions_RendersIDNameAgentAlive(t *testing.T) {
	out := FormatSessions([]wire.SessionInfo{
		{ID: "abc123", Name: "api", Agent: "claude", Alive: true},
		{ID: "def456", Name: "scratch", Agent: "", Alive: false},
	})
	for _, want := range []string{"abc123", "api", "claude", "def456", "scratch", "shell", "dead"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

func TestFormatSessions_EmptyIsHelpful(t *testing.T) {
	out := FormatSessions(nil)
	if !strings.Contains(out, "no sessions") {
		t.Errorf("empty list should say so, got: %q", out)
	}
}
