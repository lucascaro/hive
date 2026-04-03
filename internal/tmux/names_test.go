package tmux

import (
	"strings"
	"testing"
)

func TestSessionName(t *testing.T) {
	cases := []struct {
		name      string
		projectID string
	}{
		{name: "short id", projectID: "abc"},
		{name: "exactly 8 chars", projectID: "12345678"},
		{name: "longer than 8 chars", projectID: "123456789abcdef"},
		{name: "empty id", projectID: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SessionName(tc.projectID)
			if got != HiveSession {
				t.Errorf("SessionName(%q) = %q, want %q", tc.projectID, got, HiveSession)
			}
			if !strings.HasPrefix(got, "hive-") {
				t.Errorf("SessionName(%q) = %q, want hive- prefix", tc.projectID, got)
			}
		})
	}
}

func TestWindowName(t *testing.T) {
	cases := []struct {
		name        string
		projectName string
		agentType   string
		title       string
		want        string
	}{
		{
			name:        "short names",
			projectName: "myproj",
			agentType:   "claude",
			title:       "main",
			want:        "myproj-claude-main",
		},
		{
			name:        "long project truncated",
			projectName: "very-long-project-name",
			agentType:   "codex",
			title:       "feature",
			want:        "very-lon-codex-feature",
		},
		{
			name:        "long title truncated",
			projectName: "proj",
			agentType:   "gemini",
			title:       "a-very-long-feature-branch-name",
			want:        "proj-gemini-a-very-long-",
		},
		{
			name:        "team worker",
			projectName: "api",
			agentType:   "claude",
			title:       "worker-1",
			want:        "api-claude-worker-1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := WindowName(tc.projectName, tc.agentType, tc.title)
			if got != tc.want {
				t.Errorf("WindowName(%q, %q, %q) = %q, want %q", tc.projectName, tc.agentType, tc.title, got, tc.want)
			}
		})
	}
}

func TestTarget(t *testing.T) {
	cases := []struct {
		name        string
		tmuxSession string
		windowIdx   int
		want        string
	}{
		{
			name:        "basic target",
			tmuxSession: "hive-sessions",
			windowIdx:   0,
			want:        "hive-sessions:0",
		},
		{
			name:        "window index 5",
			tmuxSession: "hive-sessions",
			windowIdx:   5,
			want:        "hive-sessions:5",
		},
		{
			name:        "empty session",
			tmuxSession: "",
			windowIdx:   0,
			want:        ":0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Target(tc.tmuxSession, tc.windowIdx)
			if got != tc.want {
				t.Errorf("Target(%q, %d) = %q, want %q", tc.tmuxSession, tc.windowIdx, got, tc.want)
			}
		})
	}
}
