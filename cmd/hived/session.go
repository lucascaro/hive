// `hived session list` prints the sessions in the caller's project —
// the read-only half of session observation (spec 390). Inside a Hive
// session only: it speaks ModeSession on HIVE_SOCKET, where the daemon
// narrows the list to the caller's own project and to four fields.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/lucascaro/hive/internal/wire"
)

const sessionUsage = `usage:
  hived session list

Prints the sessions in this session's project: id, name, agent, state.
Run inside a Hive session. State is one of idle, working, waiting_input,
waiting_permission, exited, error.
`

// runSession implements `hived session`; it returns the exit code.
func runSession(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, sessionUsage)
			return 0
		}
		fmt.Fprintf(stderr, "hived session: %v\n%s", err, sessionUsage)
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) != "list" {
		fmt.Fprint(stderr, sessionUsage)
		return 2
	}
	sessions, err := listProjectSessions()
	if err != nil {
		fmt.Fprintf(stderr, "hived session: %v\n", err)
		return 2
	}
	printSessions(stdout, sessions)
	return 0
}

// listProjectSessions asks the daemon for the caller's project's sessions.
func listProjectSessions() ([]wire.SessionInfo, error) {
	sessionID, sock, err := ideaEnv()
	if err != nil {
		return nil, err
	}
	c, err := ideaDial(sock, sessionID)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	// The daemon volunteers a snapshot of the caller's own entry first;
	// reading it keeps it from being mistaken for our answer.
	if _, err := awaitSessions(c); err != nil {
		return nil, err
	}
	if err := c.WriteJSON(wire.FrameListSessions, struct{}{}); err != nil {
		return nil, err
	}
	return awaitSessions(c)
}

func printSessions(w io.Writer, sessions []wire.SessionInfo) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, s := range sessions {
		agent := s.Agent
		if agent == "" {
			agent = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.ID, oneLine(s.Name), agent, wire.StateName(s.State))
	}
	tw.Flush()
}
