package client

import (
	"fmt"

	"github.com/lucascaro/hive/internal/wire"
)

// ResolveSession picks the session to attach to. With an explicit arg it
// must match a session ID. With no arg it returns the sole session, or
// errors asking the user to disambiguate.
func ResolveSession(sessions []wire.SessionInfo, arg string) (string, error) {
	if arg != "" {
		for _, s := range sessions {
			if s.ID == arg {
				return s.ID, nil
			}
		}
		return "", fmt.Errorf("no session with id %q (try 'hive ls')", arg)
	}
	switch len(sessions) {
	case 0:
		return "", fmt.Errorf("no sessions to attach to")
	case 1:
		return sessions[0].ID, nil
	default:
		return "", fmt.Errorf("multiple sessions — specify a session id (try 'hive ls')")
	}
}
