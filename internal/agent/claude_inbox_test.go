//go:build !windows

package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
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

// stubInboxVersion makes `claude --version` print out and re-arms the
// inbox's once-per-daemon gate, so SendToClaude probes again.
func stubInboxVersion(t *testing.T, out string, err error) {
	t.Helper()
	restore := SetClaudeVersionProbeForTest(func() ([]byte, error) { return []byte(out), err })
	reset := func() { inboxVersionOnce, inboxVersionOK = sync.Once{}, false }
	reset()
	t.Cleanup(func() { restore(); reset() })
}

// registerInConfigDir points CLAUDE_CONFIG_DIR at a temp dir holding one
// registration for session id "s" naming a live fake inbox.
func registerInConfigDir(t *testing.T) <-chan string {
	t.Helper()
	sock, got := listen(t)
	cfg := t.TempDir()
	if err := os.Mkdir(filepath.Join(cfg, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	register(t, filepath.Join(cfg, "sessions"), "1.json", "s", sock)
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	return got
}

func TestSendToClaudeDeliversOnVerifiedVersion(t *testing.T) {
	stubInboxVersion(t, "2.1.300 (Claude Code)\n", nil)
	got := registerInConfigDir(t)
	if err := SendToClaude([]string{"", "s"}, "hello"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("nothing reached the inbox")
	}
}

// The version gate is the guard against an unverified wire shape: below
// it, or when the version is unknown, nothing is dialled.
func TestSendToClaudeVersionGate(t *testing.T) {
	for name, probe := range map[string]struct {
		out string
		err error
	}{
		"older":   {"2.1.299 (Claude Code)\n", nil},
		"garbled": {"claude", nil},
		"missing": {"", errors.New("exec: not found")},
	} {
		t.Run(name, func(t *testing.T) {
			stubInboxVersion(t, probe.out, probe.err)
			got := registerInConfigDir(t)
			if err := SendToClaude([]string{"s"}, "hello"); !errors.Is(err, ErrNoInbox) {
				t.Fatalf("err = %v, want ErrNoInbox", err)
			}
			select {
			case line := <-got:
				t.Fatalf("gated claude still received %q", line)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}

func TestSendToClaudeNoRegistration(t *testing.T) {
	stubInboxVersion(t, "2.1.300\n", nil)
	registerInConfigDir(t)
	if err := SendToClaude([]string{"someone-else"}, "hello"); !errors.Is(err, ErrNoInbox) {
		t.Fatalf("err = %v, want ErrNoInbox", err)
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

func TestFindClaudeInboxesSkipsSymlinkAndFIFORegistrations(t *testing.T) {
	sock, _ := listen(t)
	reg := t.TempDir()
	real := t.TempDir()
	register(t, real, "target.json", "s", sock)
	if err := os.Symlink(filepath.Join(real, "target.json"), filepath.Join(reg, "10.json")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(reg, "11.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan []string, 1)
	go func() { done <- FindClaudeInboxes(reg, "s") }()
	select {
	case got := <-done:
		if len(got) != 0 {
			t.Fatalf("accepted a symlink or FIFO registration: %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a FIFO registration blocked the lookup")
	}
}

func TestReadRegistrationRejectsOversize(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(p, make([]byte, inboxRegMax+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readRegistration(p); err == nil {
		t.Fatal("oversize registration accepted")
	}
}

func TestFindClaudeInboxesRejectsWritableSocketDir(t *testing.T) {
	dir, err := os.MkdirTemp("", "cc")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	l, err := net.Listen("unix", filepath.Join(dir, "1.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	reg := t.TempDir()
	register(t, reg, "1.json", "s", filepath.Join(dir, "1.sock"))
	if got := FindClaudeInboxes(reg, "s"); len(got) != 1 {
		t.Fatalf("0700 dir rejected: %v", got)
	}
	if err := os.Chmod(dir, 0o770); err != nil {
		t.Fatal(err)
	}
	if got := FindClaudeInboxes(reg, "s"); len(got) != 0 {
		t.Fatalf("group-writable socket dir accepted: %v", got)
	}
}
