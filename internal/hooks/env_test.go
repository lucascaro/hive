package hooks

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/state"
)

func testEvent() state.HookEvent {
	return state.HookEvent{
		Name:         "session-create",
		ProjectID:    "proj-123",
		ProjectName:  "my project",
		SessionID:    "sess-456",
		SessionTitle: "my session",
		TeamID:       "team-789",
		TeamName:     "my team",
		TeamRole:     state.RoleOrchestrator,
		AgentType:    state.AgentClaude,
		AgentCmd:     []string{"claude", "--no-update"},
		TmuxSession:  "hive-proj1234",
		TmuxWindow:   3,
		WorkDir:      "/home/user/project",
	}
}

func TestBuildEnv_ContainsAllVars(t *testing.T) {
	env := BuildEnv(testEvent())

	expected := map[string]string{
		"HIVE_VERSION":       version,
		"HIVE_EVENT":         "session-create",
		"HIVE_PROJECT_ID":    "proj-123",
		"HIVE_PROJECT_NAME":  "my project",
		"HIVE_SESSION_ID":    "sess-456",
		"HIVE_SESSION_TITLE": "my session",
		"HIVE_TEAM_ID":       "team-789",
		"HIVE_TEAM_NAME":     "my team",
		"HIVE_TEAM_ROLE":     "orchestrator",
		"HIVE_AGENT_TYPE":    "claude",
		"HIVE_AGENT_CMD":     "claude --no-update",
		"HIVE_TMUX_SESSION":  "hive-proj1234",
		"HIVE_TMUX_WINDOW":   "3",
		"HIVE_WORK_DIR":      "/home/user/project",
	}

	envMap := make(map[string]string, len(env))
	for _, entry := range env {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	for key, want := range expected {
		got, ok := envMap[key]
		if !ok {
			t.Errorf("missing env var %q", key)
			continue
		}
		if got != want {
			t.Errorf("env %q = %q, want %q", key, got, want)
		}
	}
}

func TestBuildEnv_AgentCmdJoinedBySpace(t *testing.T) {
	event := testEvent()
	event.AgentCmd = []string{"claude", "--dangerously-skip-permissions", "--no-update"}
	env := BuildEnv(event)

	for _, entry := range env {
		if strings.HasPrefix(entry, "HIVE_AGENT_CMD=") {
			got := strings.TrimPrefix(entry, "HIVE_AGENT_CMD=")
			want := "claude --dangerously-skip-permissions --no-update"
			if got != want {
				t.Errorf("HIVE_AGENT_CMD = %q, want %q", got, want)
			}
			return
		}
	}
	t.Error("HIVE_AGENT_CMD not found in env")
}

func TestBuildEnv_EmptyAgentCmd(t *testing.T) {
	event := testEvent()
	event.AgentCmd = []string{}
	env := BuildEnv(event)

	for _, entry := range env {
		if strings.HasPrefix(entry, "HIVE_AGENT_CMD=") {
			got := strings.TrimPrefix(entry, "HIVE_AGENT_CMD=")
			if got != "" {
				t.Errorf("HIVE_AGENT_CMD = %q for empty AgentCmd, want empty string", got)
			}
			return
		}
	}
	t.Error("HIVE_AGENT_CMD not found in env")
}

func TestBuildEnv_Length(t *testing.T) {
	env := BuildEnv(testEvent())
	// We expect exactly 14 vars.
	if len(env) != 14 {
		t.Errorf("BuildEnv() returned %d vars, want 14", len(env))
	}
}
