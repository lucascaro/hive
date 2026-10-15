// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// `hived msg` hands another session a one-line note from a shell — the
// CLI half of session messaging (spec 338).
//
// It speaks to the control socket, so it works from an ordinary
// terminal. Inside a Hive session HIVE_SOCKET names the narrowed events
// socket, which does not serve SEND_TO_SESSION: a session may direct its
// siblings only once it has been granted that (spec 389).
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/buildinfo"
	"github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/wire"
)

const msgUsage = `usage:
  hived msg [--no-queue] <session-id-or-name> <text...>

Types <text> into the session. A session that is working is queued (the
text is typed when it next goes idle) unless --no-queue is given.
`

// runMsg implements `hived msg`; it returns the process exit code.
func runMsg(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("msg", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	noQueue := fs.Bool("no-queue", false, "fail instead of queueing when the session is busy")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, msgUsage)
			return 0
		}
		fmt.Fprintf(stderr, "hived msg: %v\n%s", err, msgUsage)
		return 2
	}
	rest := fs.Args()
	if len(rest) < 2 {
		fmt.Fprint(stderr, msgUsage)
		return 2
	}
	text := strings.Join(rest[1:], " ")
	if err := sendMsg(rest[0], text, !*noQueue, stdout); err != nil {
		fmt.Fprintf(stderr, "hived msg: %v\n", err)
		return 2
	}
	return 0
}

// resolveTarget finds the session ref names: an exact id, else an exact
// name. No prefix matching — this types into a terminal, and landing in
// the wrong one because two names share a prefix is the worse failure.
func resolveTarget(sessions []wire.SessionInfo, ref string) (wire.SessionInfo, error) {
	for _, s := range sessions {
		if s.ID == ref {
			return s, nil
		}
	}
	var hits []wire.SessionInfo
	for _, s := range sessions {
		if s.Name == ref {
			hits = append(hits, s)
		}
	}
	switch len(hits) {
	case 0:
		return wire.SessionInfo{}, fmt.Errorf("no session with id or name %q", ref)
	case 1:
		return hits[0], nil
	}
	ids := make([]string, len(hits))
	for i, s := range hits {
		ids[i] = s.ID
	}
	return wire.SessionInfo{}, fmt.Errorf("%d sessions are named %q — use an id: %s", len(hits), ref, strings.Join(ids, ", "))
}

func sendMsg(ref, text string, queue bool, stdout io.Writer) error {
	if _, _, err := ideaEnv(); err == nil {
		return errors.New("`hived msg` is not available inside a Hive session yet; run it from an ordinary shell")
	}
	sock := daemon.ActiveSocketPath()
	if err := daemon.CheckSocketDir(sock); err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", sock, ideaDialTimeout)
	if err != nil {
		return fmt.Errorf("cannot reach the daemon at %s: %w", sock, err)
	}
	_ = conn.SetDeadline(time.Now().Add(ideaDeadline))
	c, err := wire.Handshake(conn, wire.Hello{
		Version: wire.PROTOCOL_VERSION,
		Client:  "hived-msg/" + buildinfo.Version(),
		BuildID: buildinfo.BuildID(),
		Mode:    wire.ModeControl,
	})
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	// Re-armed after the handshake, which clears the read deadline.
	_ = conn.SetDeadline(time.Now().Add(ideaDeadline))

	if err := c.WriteJSON(wire.FrameListSessions, struct{}{}); err != nil {
		return err
	}
	sessions, err := awaitSessions(c)
	if err != nil {
		return err
	}
	target, err := resolveTarget(sessions, ref)
	if err != nil {
		return err
	}
	// Said before sending, so a wrong target is visible even when it
	// cannot be undone.
	fmt.Fprintf(stdout, "to %s (%s)\n", target.Name, target.ID)

	if err := c.WriteJSON(wire.FrameSendToSession, wire.SendToSessionReq{
		SessionID: target.ID, Text: text, Queue: queue,
	}); err != nil {
		return err
	}
	resp, err := awaitSent(c, target.ID)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, resp.Delivery)
	return nil
}

// awaitSent reads until our send is answered. A control connection also
// receives every broadcast, which is skipped.
func awaitSent(c *wire.Client, sessionID string) (wire.SentResp, error) {
	for {
		ft, payload, err := c.ReadFrame()
		if err != nil {
			return wire.SentResp{}, err
		}
		switch ft {
		case wire.FrameSent:
			var resp wire.SentResp
			if err := json.Unmarshal(payload, &resp); err != nil {
				return wire.SentResp{}, err
			}
			if resp.SessionID == sessionID {
				return resp, nil
			}
		case wire.FrameError:
			return wire.SentResp{}, ideaFrameError(payload)
		}
	}
}
