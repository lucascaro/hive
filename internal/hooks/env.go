package hooks

import (
	"fmt"
	"strings"

	"github.com/lucascaro/hive/internal/state"
)

const version = "0.1.0"

// BuildEnv returns the environment variable slice to inject into hook processes.
func BuildEnv(event state.HookEvent) []string {
	return []string{
		"HIVE_VERSION=" + version,
		"HIVE_EVENT=" + event.Name,
		"HIVE_PROJECT_ID=" + event.ProjectID,
		"HIVE_PROJECT_NAME=" + event.ProjectName,
		"HIVE_SESSION_ID=" + event.SessionID,
		"HIVE_SESSION_TITLE=" + event.SessionTitle,
		"HIVE_TEAM_ID=" + event.TeamID,
		"HIVE_TEAM_NAME=" + event.TeamName,
		"HIVE_TEAM_ROLE=" + string(event.TeamRole),
		"HIVE_AGENT_TYPE=" + string(event.AgentType),
		"HIVE_AGENT_CMD=" + strings.Join(event.AgentCmd, " "),
		"HIVE_TMUX_SESSION=" + event.TmuxSession,
		"HIVE_TMUX_WINDOW=" + fmt.Sprintf("%d", event.TmuxWindow),
		"HIVE_WORK_DIR=" + event.WorkDir,
	}
}
