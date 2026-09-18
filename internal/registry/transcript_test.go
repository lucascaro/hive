package registry

import (
	"testing"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
)

// TestTranscriptPathsReasons pins the failure reasons the GUI renders
// differently: a missing transcript must not read as "unsupported".
func TestTranscriptPathsReasons(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows

	claude := string(agent.IDClaude)
	r := &Registry{
		entries: map[string]*Entry{
			"shell":       {ID: "shell"},
			"no-agent-id": {ID: "no-agent-id", Agent: claude, WorktreePath: "/repo"},
			"no-cwd":      {ID: "no-cwd", Agent: claude, AgentSessionID: "abc"},
			"no-file":     {ID: "no-file", Agent: claude, AgentSessionID: "abc", ProjectID: "p"},
		},
		projects: map[string]*Project{"p": {ID: "p", Cwd: "/repo"}},
	}
	cases := []struct{ id, want string }{
		{"missing", wire.TranscriptNoSession},
		{"shell", wire.TranscriptUnsupported},
		// Caller-supplied argv: no AgentSessionID was ever recorded.
		{"no-agent-id", wire.TranscriptMissing},
		{"no-cwd", wire.TranscriptMissing},
		// Resolvable inputs, but the agent never wrote the file.
		{"no-file", wire.TranscriptMissing},
	}
	for _, tc := range cases {
		paths, reason := r.TranscriptPaths(tc.id)
		if reason != tc.want || paths != nil {
			t.Errorf("TranscriptPaths(%q) = (%v, %q), want (nil, %q)", tc.id, paths, reason, tc.want)
		}
	}
}
