package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Claude Code's cross-session inbox (spec 338, phase 2).
//
// Each running Claude registers itself in a small JSON file under its
// config directory's sessions folder, naming a unix socket. A client that
// writes one newline-terminated JSON line to that socket injects a user
// message; Claude reads it between tool calls, or starts a turn if idle.
// A connection that sends no complete line within 30 s is closed.
//
// The socket also accepts an auth line, but on macOS and Linux it is
// optional, and the token lives only in the child's own environment and
// in a sibling credentials file that Hive never opens (the guard test
// TestNoClaudeSessionKeyReads enforces that). Hive therefore sends none.
//
// Both the registration file and the message line are Claude internals,
// verified by hand on claude 2.1.300; see the table in
// docs/design-docs/control-plane.md. Every failure here is a fallback to
// the typed path, never an error the user sees.

// minInboxVersion is the oldest claude the inbox was verified on. There
// is no known-bad upper bound.
const minInboxVersion = "2.1.300"

const (
	inboxDialTimeout = 2 * time.Second
	// inboxRegMax caps a registration file read: they are tiny, and the
	// directory is shared with every other Claude on the machine.
	inboxRegMax = 64 << 10
)

// ErrNoInbox means no usable inbox was found for the session; the caller
// falls back to typing.
var ErrNoInbox = errors.New("agent: no claude inbox for session")

// ClaudeSessionsDir is where Claude registers running sessions.
func ClaudeSessionsDir(home string) string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "sessions")
	}
	return filepath.Join(home, ".claude", "sessions")
}

// ownedByUs is swapped in tests; the real check is per-platform.
var ownedByUs = fileOwnedByUs

// FindClaudeInboxes returns the inbox sockets registered for any of ids
// (a Claude session id), newest registration first. Only *.json files are
// read. A candidate must be a socket owned by the current user whose
// parent directory is also a real directory owned by the current user,
// so a path planted in a registration file cannot aim Hive at someone
// else's socket.
func FindClaudeInboxes(sessionsDir string, ids ...string) []string {
	want := map[string]bool{}
	for _, id := range ids {
		if id != "" {
			want[id] = true
		}
	}
	entries, err := os.ReadDir(sessionsDir)
	if err != nil || len(want) == 0 {
		return nil
	}
	type cand struct {
		path string
		mod  time.Time
	}
	var cands []cand
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}
		fi, err := de.Info()
		// DirEntry.Info is an Lstat: a symlink or FIFO named *.json is not a
		// regular file, so it is skipped before anything opens it.
		if err != nil || !fi.Mode().IsRegular() || !ownedByUs(fi) {
			continue
		}
		b, err := readRegistration(filepath.Join(sessionsDir, de.Name()))
		if err != nil {
			continue
		}
		var reg struct {
			SessionID string `json:"sessionId"`
			Socket    string `json:"messagingSocketPath"`
		}
		if json.Unmarshal(b, &reg) != nil || !want[reg.SessionID] || reg.Socket == "" {
			continue
		}
		if !filepath.IsAbs(reg.Socket) || filepath.Clean(reg.Socket) != reg.Socket {
			continue
		}
		if !socketOwnedByUs(reg.Socket) {
			continue
		}
		cands = append(cands, cand{reg.Socket, fi.ModTime()})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].mod.After(cands[j].mod) })
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.path
	}
	return out
}

// readRegistration reads at most inboxRegMax bytes and rejects a longer
// file, whatever its stat size claimed.
func readRegistration(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, inboxRegMax+1))
	if err == nil && len(b) > inboxRegMax {
		err = errors.New("registration too large")
	}
	return b, err
}

func socketOwnedByUs(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSocket == 0 || !ownedByUs(fi) {
		return false
	}
	dir, err := os.Lstat(filepath.Dir(path))
	// A directory others can write to lets them swap the socket between
	// this check and the dial.
	return err == nil && dir.IsDir() && dir.Mode()&os.ModeSymlink == 0 &&
		dir.Mode().Perm()&0o022 == 0 && ownedByUs(dir)
}

// inboxLine is the exact wire form of one message. Encoder.Encode adds
// the terminating newline.
func inboxLine(text string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	err := enc.Encode(struct {
		Type    string `json:"type"`
		Message msg    `json:"message"`
	}{"user", msg{"user", text}})
	return buf.Bytes(), err
}

// SendClaudeInbox writes text to the first of socks that accepts it.
func SendClaudeInbox(socks []string, text string, timeout time.Duration) error {
	line, err := inboxLine(text)
	if err != nil {
		return err
	}
	var last error = ErrNoInbox
	for _, p := range socks {
		c, err := net.DialTimeout("unix", p, timeout)
		if err != nil {
			last = err
			continue
		}
		_ = c.SetWriteDeadline(time.Now().Add(timeout))
		_, err = c.Write(line)
		_ = c.Close()
		if err == nil {
			return nil
		}
		last = err
	}
	return last
}

var inboxLogged sync.Map

// logInboxOnce logs each distinct fallback reason once per daemon
// lifetime, so a Claude upgrade that breaks the inbox is visible
// without spamming.
func logInboxOnce(reason string) {
	if _, dup := inboxLogged.LoadOrStore(reason, true); !dup {
		log.Printf("agent: claude inbox unavailable (%s); typing instead", reason)
	}
}

var (
	inboxVersionOnce sync.Once
	inboxVersionOK   bool
)

// claudeInboxSupported runs the version probe once per daemon lifetime.
func claudeInboxSupported() bool {
	inboxVersionOnce.Do(func() {
		out, err := claudeVersionProbe()
		m := claudeSemverRe.FindSubmatch(out)
		switch {
		case err != nil || m == nil:
			logInboxOnce("claude version unknown")
		case semverLess(string(m[0]), minInboxVersion):
			logInboxOnce(fmt.Sprintf("claude %s is older than %s", m[0], minInboxVersion))
		default:
			inboxVersionOK = true
		}
	})
	return inboxVersionOK
}

// SendToClaude delivers text to the Claude session known by any of ids.
// ErrNoInbox means "type it instead"; any other error does too.
func SendToClaude(ids []string, text string) error {
	if !claudeInboxSupported() {
		return ErrNoInbox
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ErrNoInbox
	}
	socks := FindClaudeInboxes(ClaudeSessionsDir(home), ids...)
	if len(socks) == 0 {
		logInboxOnce("no registered socket for session")
		return ErrNoInbox
	}
	err = SendClaudeInbox(socks, text, inboxDialTimeout)
	if err != nil {
		logInboxOnce("send failed: " + err.Error())
	}
	return err
}
