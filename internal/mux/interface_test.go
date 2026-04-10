package mux

import "testing"

func TestWindowName(t *testing.T) {
	tests := []struct {
		name        string
		project     string
		agentType   string
		title       string
		want        string
	}{
		{"short names", "proj", "claude", "sess", "proj-claude-sess"},
		{"project truncated at 8", "longproject", "claude", "sess", "longproj-claude-sess"},
		{"project exactly 8", "12345678", "claude", "sess", "12345678-claude-sess"},
		{"title truncated at 12", "p", "claude", "a-very-long-title", "p-claude-a-very-long-"},
		{"title exactly 12", "p", "claude", "123456789012", "p-claude-123456789012"},
		{"empty fields", "", "", "", "--"},
		{"empty project", "", "claude", "s", "-claude-s"},
		{"empty title", "p", "claude", "", "p-claude-"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := WindowName(tc.project, tc.agentType, tc.title)
			if got != tc.want {
				t.Errorf("WindowName(%q, %q, %q) = %q, want %q",
					tc.project, tc.agentType, tc.title, got, tc.want)
			}
		})
	}
}

func TestTarget(t *testing.T) {
	tests := []struct {
		name    string
		session string
		window  int
		want    string
	}{
		{"standard", "hive-sessions", 3, "hive-sessions:3"},
		{"window zero", "hive-sessions", 0, "hive-sessions:0"},
		{"empty session", "", 5, ":5"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Target(tc.session, tc.window)
			if got != tc.want {
				t.Errorf("Target(%q, %d) = %q, want %q", tc.session, tc.window, got, tc.want)
			}
		})
	}
}
