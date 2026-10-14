//go:build !windows

package agent

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// listen starts a fake Claude inbox and returns its path and the channel
// the first received line arrives on. MkdirTemp, not t.TempDir: the
// latter is too long for a unix socket path on macOS.
func listen(t *testing.T) (string, <-chan string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "cc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "1.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	got := make(chan string, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		s, _ := bufio.NewReader(c).ReadString('\n')
		got <- s
	}()
	return sock, got
}

func register(t *testing.T, dir, file, sessionID, sock string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"sessionId": sessionID, "pid": 1, "messagingSocketPath": sock})
	if err := os.WriteFile(filepath.Join(dir, file), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// The fixture is the line claude 2.1.300 itself documents in its debug
// log for injecting a message ("hello"); it was captured from a real
// run, not written from this code.
func TestSendClaudeInboxWritesCapturedLine(t *testing.T) {
	want, err := os.ReadFile("testdata/claude_inbox_line.json")
	if err != nil {
		t.Fatal(err)
	}
	sock, got := listen(t)
	if err := SendClaudeInbox([]string{sock}, "hello", time.Second); err != nil {
		t.Fatal(err)
	}
	if line := <-got; line != string(want) {
		t.Fatalf("wire line = %q, want %q", line, want)
	}
}

func TestSendClaudeInboxKeepsMultilineAndQuotes(t *testing.T) {
	sock, got := listen(t)
	text := "a \"b\"\nc <d> & e"
	if err := SendClaudeInbox([]string{sock}, text, time.Second); err != nil {
		t.Fatal(err)
	}
	var m struct {
		Type    string `json:"type"`
		Message struct{ Role, Content string }
	}
	if err := json.Unmarshal([]byte(<-got), &m); err != nil {
		t.Fatal(err)
	}
	if m.Type != "user" || m.Message.Role != "user" || m.Message.Content != text {
		t.Fatalf("decoded %+v", m)
	}
}

func TestSendClaudeInboxTriesNextCandidate(t *testing.T) {
	sock, got := listen(t)
	if err := SendClaudeInbox([]string{"/nonexistent/dead.sock", sock}, "hi", time.Second); err != nil {
		t.Fatal(err)
	}
	<-got
}

func TestSendClaudeInboxNoCandidates(t *testing.T) {
	if err := SendClaudeInbox(nil, "hi", time.Second); err != ErrNoInbox {
		t.Fatalf("got %v, want ErrNoInbox", err)
	}
}

func TestFindClaudeInboxesMatchesAnyID(t *testing.T) {
	sock, _ := listen(t)
	reg := t.TempDir()
	register(t, reg, "10.json", "agent-id", sock)
	register(t, reg, "11.json", "other", sock)
	if got := FindClaudeInboxes(reg, "hive-id", "agent-id"); len(got) != 1 || got[0] != sock {
		t.Fatalf("got %v", got)
	}
	if got := FindClaudeInboxes(reg, "nobody"); len(got) != 0 {
		t.Fatalf("matched a stranger: %v", got)
	}
	if got := FindClaudeInboxes(reg, ""); len(got) != 0 {
		t.Fatalf("empty id matched: %v", got)
	}
}

func TestFindClaudeInboxesReturnsAllNewestFirst(t *testing.T) {
	a, _ := listen(t)
	b, _ := listen(t)
	reg := t.TempDir()
	register(t, reg, "10.json", "s", a)
	register(t, reg, "11.json", "s", b)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(reg, "10.json"), old, old); err != nil {
		t.Fatal(err)
	}
	got := FindClaudeInboxes(reg, "s")
	if len(got) != 2 || got[0] != b || got[1] != a {
		t.Fatalf("got %v, want [%s %s]", got, b, a)
	}
}

func TestFindClaudeInboxesRejectsNonSocketAndRelative(t *testing.T) {
	dir, err := os.MkdirTemp("", "cc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	reg := t.TempDir()
	register(t, reg, "1.json", "s", plain)
	register(t, reg, "2.json", "s", "rel/x.sock")
	if got := FindClaudeInboxes(reg, "s"); len(got) != 0 {
		t.Fatalf("accepted %v", got)
	}
}

func TestFindClaudeInboxesRejectsForeignOwner(t *testing.T) {
	sock, _ := listen(t)
	reg := t.TempDir()
	register(t, reg, "1.json", "s", sock)
	old := ownedByUs
	ownedByUs = func(os.FileInfo) bool { return false }
	defer func() { ownedByUs = old }()
	if got := FindClaudeInboxes(reg, "s"); len(got) != 0 {
		t.Fatalf("accepted a foreign-owned socket: %v", got)
	}
}

// Only *.json registrations are read; a credentials file next to a
// matching registration must never be opened. A directory named like one
// fails any read, so an open would show up as a skipped candidate only
// if the code tried it; the real guard is the source-tree test
// TestNoClaudeSessionKeyReads, and this pins the observable half: the
// matching registration is still found with such an entry beside it.
func TestFindClaudeInboxesIgnoresNonJSON(t *testing.T) {
	sock, _ := listen(t)
	reg := t.TempDir()
	register(t, reg, "10.json", "s", sock)
	register(t, reg, "10.abc.notjson", "s", "/nonexistent/other.sock")
	if got := FindClaudeInboxes(reg, "s"); len(got) != 1 || got[0] != sock {
		t.Fatalf("got %v", got)
	}
}

func TestClaudeSessionsDirHonoursConfigDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/cfg")
	if got := ClaudeSessionsDir("/home/u"); got != "/cfg/sessions" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if got := ClaudeSessionsDir("/home/u"); got != "/home/u/.claude/sessions" {
		t.Fatalf("got %q", got)
	}
}
