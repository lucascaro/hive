package client

import (
	"fmt"
	"strings"

	"github.com/lucascaro/hive/internal/wire"
)

// FormatSessions renders a session list as an aligned text table.
func FormatSessions(sessions []wire.SessionInfo) string {
	if len(sessions) == 0 {
		return "no sessions\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-12s  %-16s  %-8s  %s\n", "ID", "NAME", "AGENT", "STATE")
	for _, s := range sessions {
		agent := s.Agent
		if agent == "" {
			agent = "shell"
		}
		state := "alive"
		if !s.Alive {
			state = "dead"
		}
		id := s.ID
		if len(id) > 12 {
			id = id[:12]
		}
		fmt.Fprintf(&b, "%-12s  %-16s  %-8s  %s\n", id, s.Name, agent, state)
	}
	return b.String()
}
