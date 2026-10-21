// `hived wait` blocks until a sibling session reaches a state — the
// other half of session observation (spec 390), so a session that sent a
// note can carry on once the sibling is done with it. Inside a Hive
// session only: the daemon owns the wait, resolves the target in the
// caller's project and logs the outcome.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

const waitUsage = `usage:
  hived wait <session-id-or-name> [--state idle,exited,error] [--timeout 10m] [--after-working]

Blocks until the session — one in this session's project — reaches one of
the states, prints the state reached and exits 0. Exit 1: the timeout
elapsed or the session disappeared first. Exit 2: bad arguments, not in a
Hive session, or the session is unknown, ambiguous, in another project, or
this one.

  --state            comma-separated; any of idle, working, waiting_input,
                     waiting_permission, exited, error. Default idle,exited,error.
  --timeout          at most 30m, default 10m.
  --after-working    do not count an idle that predates the session
                     starting work. Use it right after "hived msg": the
                     agent is still idle until it has picked the note up.

waiting_input and waiting_permission never end on their own, so
"--state idle" on a session stuck at a permission prompt waits until
--timeout.
`

// runWait implements `hived wait`; it returns the process exit code.
func runWait(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	states := fs.String("state", "", "states that end the wait")
	timeout := fs.Duration("timeout", wire.WaitDefaultTimeout, "give up after this long")
	afterWorking := fs.Bool("after-working", false, "ignore an idle that predates the session starting work")
	// The session comes first in the documented form, and flag stops at
	// the first non-flag, so parse once for flags ahead of it and again
	// for the ones after.
	err := fs.Parse(args)
	if err == nil && fs.NArg() > 0 {
		target := fs.Arg(0)
		if err = fs.Parse(fs.Args()[1:]); err == nil {
			args = append([]string{target}, fs.Args()...)
		}
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, waitUsage)
			return 0
		}
		fmt.Fprintf(stderr, "hived wait: %v\n%s", err, waitUsage)
		return 2
	}
	if len(args) != 1 {
		fmt.Fprint(stderr, waitUsage)
		return 2
	}
	if *timeout <= 0 || *timeout > wire.WaitMaxTimeout {
		fmt.Fprintf(stderr, "hived wait: --timeout must be between 1s and %s\n", wire.WaitMaxTimeout)
		return 2
	}
	var names []string
	if *states != "" {
		names = strings.Split(*states, ",")
		for _, n := range names {
			if _, ok := wire.ParseStateName(n); !ok {
				fmt.Fprintf(stderr, "hived wait: unknown state %q; want one of %s\n", n, strings.Join(wire.WaitStateNames, ", "))
				return 2
			}
		}
	}
	resp, err := waitForSession(wire.WaitSessionReq{
		SessionID:      args[0],
		States:         names,
		TimeoutSeconds: int(timeout.Round(time.Second) / time.Second),
		AfterWorking:   *afterWorking,
	})
	if err != nil {
		fmt.Fprintf(stderr, "hived wait: %v\n", err)
		return 2
	}
	switch resp.State {
	case wire.WaitStateTimeout:
		fmt.Fprintf(stderr, "hived wait: timed out after %s\n", *timeout)
		return 1
	case wire.WaitStateGone:
		fmt.Fprintln(stderr, "hived wait: the session is gone")
		return 1
	}
	fmt.Fprintln(stdout, resp.State)
	return 0
}

// waitForSession sends WAIT_SESSION and blocks for the WAITED answer.
func waitForSession(req wire.WaitSessionReq) (wire.WaitedResp, error) {
	sessionID, sock, err := ideaEnv()
	if err != nil {
		return wire.WaitedResp{}, err
	}
	c, err := ideaDial(sock, sessionID)
	if err != nil {
		return wire.WaitedResp{}, err
	}
	defer c.Close()
	// ideaDial leaves the short CLI deadline armed; this request is
	// meant to take as long as the timeout, plus the daemon's answer.
	_ = c.SetDeadline(time.Now().Add(time.Duration(req.TimeoutSeconds)*time.Second + ideaDeadline))
	if err := c.WriteJSON(wire.FrameWaitSession, req); err != nil {
		return wire.WaitedResp{}, err
	}
	for {
		ft, payload, err := c.ReadFrame()
		if err != nil {
			return wire.WaitedResp{}, err
		}
		switch ft {
		case wire.FrameWaited:
			var resp wire.WaitedResp
			if err := json.Unmarshal(payload, &resp); err != nil {
				return wire.WaitedResp{}, err
			}
			return resp, nil
		case wire.FrameError:
			return wire.WaitedResp{}, ideaFrameError(payload)
		}
	}
}
