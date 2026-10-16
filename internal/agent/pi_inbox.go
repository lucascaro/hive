package agent

import (
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Pi's inbox (spec 338, phase 3). Unlike Claude's, this one is ours: the
// Hive extension listens on a per-session unix socket and hands each note
// to pi.sendUserMessage. The daemon tells the extension where to listen
// through PiInboxEnv, so the path is derived in exactly one place.
//
// Protocol: one connection per note. The client writes the UTF-8 text and
// half-closes; the extension replies "ok\n" once it has accepted the
// note — before the agent has acted on it, since a note to an idle Pi
// starts a whole turn. No ack in piAckTimeout is a failure, and the
// caller types the note instead. The extension drops a note that took
// longer than its own budget (shorter than piAckTimeout) rather than
// deliver what Hive has already typed; a stall before the extension
// accepts the connection cannot be seen from its side, so a note can
// still arrive twice then (documented in control-plane.md).

// PiInboxEnv names the socket the extension should listen on.
const PiInboxEnv = "HIVE_PI_INBOX"

const (
	// piSockMax is the sun_path budget (104 on macOS, 108 on Linux),
	// counting the NUL.
	piSockMax      = 104
	piAckTimeout   = 3 * time.Second
	piAckResponse  = "ok\n"
	piInboxIDChars = 16
)

// PiInboxPath is where a Pi session's extension listens: beside the
// daemon's events socket, which is already placed so sockets fit. "" when
// there is nowhere sensible (no socket path, Windows, or a path too long
// for sun_path), which switches the feature off for that session.
func PiInboxPath(eventsSock, sessionID string) string {
	if eventsSock == "" || runtime.GOOS == "windows" {
		return ""
	}
	var b strings.Builder
	for _, r := range sessionID {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			b.WriteRune(r)
			if b.Len() == piInboxIDChars {
				break
			}
		}
	}
	if b.Len() == 0 {
		return ""
	}
	p := filepath.Join(filepath.Dir(eventsSock), "pi-"+b.String()+".sock")
	if len(p)+1 >= piSockMax {
		return ""
	}
	return p
}

// SendToPi delivers text through a Pi extension's inbox socket. Any error
// means "type it instead".
func SendToPi(sockPath, text string) error {
	if sockPath == "" || !socketOwnedByUs(sockPath) {
		return ErrNoInbox
	}
	return sendPiInbox(sockPath, text, inboxDialTimeout, piAckTimeout)
}

func sendPiInbox(sockPath, text string, dial, ack time.Duration) error {
	c, err := net.DialTimeout("unix", sockPath, dial)
	if err != nil {
		// A refused dial is not removed: a full listen backlog reads the
		// same, and an extension that just restarted may have bound a new
		// socket here. The extension unlinks a stale one when it starts.
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(ack))
	if _, err := c.Write([]byte(text)); err != nil {
		return err
	}
	if uc, ok := c.(*net.UnixConn); ok {
		if err := uc.CloseWrite(); err != nil {
			return err
		}
	}
	buf := make([]byte, len(piAckResponse))
	n := 0
	for n < len(buf) {
		m, err := c.Read(buf[n:])
		n += m
		if err != nil {
			break
		}
	}
	if string(buf[:n]) != piAckResponse {
		return fmt.Errorf("agent: pi inbox did not acknowledge (got %q)", buf[:n])
	}
	return nil
}
