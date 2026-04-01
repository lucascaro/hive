package tmux

import (
	"strings"
	"testing"
)

func TestSessionName(t *testing.T) {
	cases := []struct {
		name      string
		projectID string
		want      string
	}{
		{
			name:      "short id not truncated",
			projectID: "abc",
			want:      "hive-abc",
		},
		{
			name:      "exactly 8 chars",
			projectID: "12345678",
			want:      "hive-12345678",
		},
		{
			name:      "longer than 8 chars gets truncated",
			projectID: "123456789abcdef",
			want:      "hive-12345678",
		},
		{
			name:      "empty id",
			projectID: "",
			want:      "hive-",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SessionName(tc.projectID)
			if got != tc.want {
				t.Errorf("SessionName(%q) = %q, want %q", tc.projectID, got, tc.want)
			}
			if !strings.HasPrefix(got, "hive-") {
				t.Errorf("SessionName(%q) = %q, want hive- prefix", tc.projectID, got)
			}
		})
	}
}

func TestWindowName(t *testing.T) {
	cases := []struct {
		name      string
		sessionID string
		want      string
	}{
		{
			name:      "short id not truncated",
			sessionID: "abc",
			want:      "abc",
		},
		{
			name:      "exactly 8 chars",
			sessionID: "12345678",
			want:      "12345678",
		},
		{
			name:      "longer than 8 chars gets truncated",
			sessionID: "123456789abcdef",
			want:      "12345678",
		},
		{
			name:      "empty id",
			sessionID: "",
			want:      "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := WindowName(tc.sessionID)
			if got != tc.want {
				t.Errorf("WindowName(%q) = %q, want %q", tc.sessionID, got, tc.want)
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
			tmuxSession: "hive-abc",
			windowIdx:   0,
			want:        "hive-abc:0",
		},
		{
			name:        "window index 5",
			tmuxSession: "hive-proj",
			windowIdx:   5,
			want:        "hive-proj:5",
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
