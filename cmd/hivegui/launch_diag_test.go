package main

import (
	"strings"
	"testing"
)

func TestLaunchEnvSummary(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "empty env",
			env:  map[string]string{},
			want: "",
		},
		{
			name: "allowlisted keys appear in order",
			env: map[string]string{
				"SHLVL":           "2",
				"HIVE_SOCKET":     "/tmp/hive.sock",
				"HIVE_SESSION_ID": "abc123",
				"TERM_PROGRAM":    "iTerm.app",
			},
			want: "HIVE_SESSION_ID=abc123 HIVE_SOCKET=/tmp/hive.sock TERM_PROGRAM=iTerm.app SHLVL=2",
		},
		{
			name: "empty-valued allowlisted keys are skipped",
			env: map[string]string{
				"HIVE_SESSION_ID": "",
				"HIVE_SOCKET":     "/tmp/hive.sock",
			},
			want: "HIVE_SOCKET=/tmp/hive.sock",
		},
		{
			name: "non-allowlisted keys never appear",
			env: map[string]string{
				"GITHUB_TOKEN": "secret-value",
				"HIVE_SOCKET":  "/tmp/hive.sock",
			},
			want: "HIVE_SOCKET=/tmp/hive.sock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }
			got := launchEnvSummary(getenv)
			if got != tt.want {
				t.Errorf("launchEnvSummary() = %q, want %q", got, tt.want)
			}
			if strings.Contains(got, "GITHUB_TOKEN") || strings.Contains(got, "secret-value") {
				t.Errorf("launchEnvSummary() leaked a non-allowlisted value: %q", got)
			}
		})
	}
}
