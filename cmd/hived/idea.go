// `hived idea` files and lists ideas from inside a Hive session — the
// shell-side half of the idea inbox, so a note can be captured without
// leaving the terminal the thought happened in.
//
// Unlike `hived hook` this command is user-facing: it prints, and it
// exits nonzero when it cannot do what was asked.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/buildinfo"
	"github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/wire"
)

const (
	ideaDialTimeout = 3 * time.Second
	ideaDeadline    = 5 * time.Second
)

// errNotInSession is what every subcommand fails with outside Hive.
var errNotInSession = errors.New("not running inside a Hive session")

const ideaUsage = `usage:
  hived idea add [-k idea|bug|feedback] <text...>
  hived idea list [--all]
`

// runIdea implements `hived idea`. It returns the process exit code so
// main stays the only thing that calls os.Exit.
func runIdea(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, ideaUsage)
		return 2
	}
	var err error
	switch args[0] {
	case "add":
		err = ideaAdd(args[1:], stdout)
	case "list":
		err = ideaList(args[1:], stdout)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, ideaUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "hived idea: unknown subcommand %q\n%s", args[0], ideaUsage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "hived idea: %v\n", err)
		return 2
	}
	return 0
}

func ideaAdd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("idea add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	kind := fs.String("k", wire.IdeaKindIdea, "idea | bug | feedback")
	if err := fs.Parse(args); err != nil {
		return err
	}
	text := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if text == "" {
		return errors.New("nothing to file — pass the idea text")
	}
	if !wire.IdeaKinds[*kind] {
		return fmt.Errorf("unknown kind %q (want idea, bug or feedback)", *kind)
	}

	sessionID, sock, err := ideaEnv()
	if err != nil {
		return err
	}
	c, err := ideaDial(sock, sessionID)
	if err != nil {
		return err
	}
	defer c.Close()

	// No project id is sent, deliberately: the daemon resolves it from
	// the live entry for this session, so an idea filed after the
	// session was reassigned lands in the project it is in now.
	if err := c.WriteJSON(wire.FrameAddIdea, wire.AddIdeaReq{
		SessionID: sessionID,
		Kind:      *kind,
		Text:      text,
	}); err != nil {
		return err
	}
	// The daemon answers a successful add with IDEA_EVENT(added) on
	// every control connection, this one included, so the new id can
	// be read back off our own fan-out rather than a bespoke reply.
	ev, err := awaitIdeaAdded(c, sessionID)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, ev.ID)
	return nil
}

func ideaList(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("idea list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	all := fs.Bool("all", false, "list every project's ideas (refused from inside a session)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	sessionID, sock, err := ideaEnv()
	if err != nil {
		return err
	}
	c, err := ideaDial(sock, sessionID)
	if err != nil {
		return err
	}
	defer c.Close()

	// LIST_IDEAS takes a project id and does not resolve one from a
	// session the way ADD_IDEA does, so resolve it here off the
	// SESSIONS snapshot every control connection is sent unprompted.
	// Read that BEFORE asking, rather than filtering the reply against
	// whatever had arrived by then: the snapshot and the reply are
	// written by different goroutines in the daemon and can interleave.
	projectID := ""
	if !*all {
		sessions, err := awaitSessions(c)
		if err != nil {
			return err
		}
		for _, s := range sessions {
			if s.ID == sessionID {
				projectID = s.ProjectID
				break
			}
		}
		if projectID == "" {
			return errNotInSession
		}
	}
	if err := c.WriteJSON(wire.FrameListIdeas, wire.ListIdeasReq{ProjectID: projectID}); err != nil {
		return err
	}
	ideas, err := awaitIdeas(c)
	if err != nil {
		return err
	}
	for _, i := range ideas {
		fmt.Fprintf(stdout, "%s  %-8s %-7s %s\n", i.ID, i.Kind, i.Status, oneLine(i.Text))
	}
	return nil
}

// oneLine keeps a multi-line idea from breaking the list's one-row-per
// -idea shape. The full text is in the GUI inbox. It cuts at the first
// control character, not just \r\n: an idea carrying an ESC/OSC
// sequence would otherwise be handed to the terminal verbatim.
func oneLine(s string) string {
	if i := strings.IndexFunc(s, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	}); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func ideaEnv() (sessionID, sock string, err error) {
	sessionID = os.Getenv("HIVE_SESSION_ID")
	sock = os.Getenv("HIVE_SOCKET")
	if sessionID == "" || sock == "" {
		return "", "", errNotInSession
	}
	return sessionID, sock, nil
}

func ideaDial(sock, sessionID string) (*wire.Client, error) {
	// HIVE_SOCKET is inherited, and a socket that answers is not
	// automatically ours — check the directory before handshaking, the
	// same as every other client does.
	if err := daemon.CheckSocketDir(sock); err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", sock, ideaDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the daemon at %s: %w", sock, err)
	}
	_ = conn.SetDeadline(time.Now().Add(ideaDeadline))
	c, err := wire.Handshake(conn, wire.Hello{
		Version: wire.PROTOCOL_VERSION,
		Client:  "hived-idea/" + buildinfo.Version(),
		BuildID: buildinfo.BuildID(),
		// ModeSession, not ModeControl: HIVE_SOCKET names the daemon's
		// events socket, which serves the idea verbs and nothing else.
		// The session id narrows the SESSIONS snapshot below to our own
		// entry, which is all `list` needs to resolve its project.
		Mode:      wire.ModeSession,
		SessionID: sessionID,
	})
	if err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

// awaitIdeaAdded reads until our own add comes back as IDEA_EVENT, or
// the daemon refuses. Frames for other clients' ideas are skipped:
// a control connection sees every idea event, not just its own.
func awaitIdeaAdded(c *wire.Client, sessionID string) (wire.IdeaInfo, error) {
	for {
		ft, payload, err := c.ReadFrame()
		if err != nil {
			return wire.IdeaInfo{}, err
		}
		switch ft {
		case wire.FrameIdeaEvent:
			var ev wire.IdeaEvent
			if err := json.Unmarshal(payload, &ev); err != nil {
				return wire.IdeaInfo{}, err
			}
			if ev.Kind == wire.IdeaEventAdded && ev.Idea.SourceSessionID == sessionID {
				return ev.Idea, nil
			}
		case wire.FrameError:
			return wire.IdeaInfo{}, ideaFrameError(payload)
		}
	}
}

// awaitSessions reads until the unprompted SESSIONS snapshot arrives.
func awaitSessions(c *wire.Client) ([]wire.SessionInfo, error) {
	for {
		ft, payload, err := c.ReadFrame()
		if err != nil {
			return nil, err
		}
		switch ft {
		case wire.FrameSessions:
			var resp wire.SessionsResp
			if err := json.Unmarshal(payload, &resp); err != nil {
				return nil, err
			}
			return resp.Sessions, nil
		case wire.FrameError:
			return nil, ideaFrameError(payload)
		}
	}
}

// awaitIdeas reads until the IDEAS reply to our LIST_IDEAS.
func awaitIdeas(c *wire.Client) ([]wire.IdeaInfo, error) {
	for {
		ft, payload, err := c.ReadFrame()
		if err != nil {
			return nil, err
		}
		switch ft {
		case wire.FrameIdeas:
			var resp wire.IdeasResp
			if err := json.Unmarshal(payload, &resp); err != nil {
				return nil, err
			}
			return resp.Ideas, nil
		case wire.FrameError:
			return nil, ideaFrameError(payload)
		}
	}
}

// ideaFrameError turns an ERROR frame into the message the user sees.
func ideaFrameError(payload []byte) error {
	var e wire.Error
	if err := json.Unmarshal(payload, &e); err != nil {
		return err
	}
	return fmt.Errorf("%s: %s", e.Code, e.Message)
}
