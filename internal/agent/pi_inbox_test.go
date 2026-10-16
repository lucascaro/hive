//go:build !windows

package agent

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// shortDir is a private (0700) directory with a short path: t.TempDir is
// 0755 and, on macOS, too long for a unix socket.
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "hp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

// fakeExtension listens like the Pi extension: reads to EOF, records the
// text, and replies with reply (nothing when reply is "").
func fakeExtension(t *testing.T, dir, reply string) (string, <-chan string) {
	t.Helper()
	p := filepath.Join(dir, "pi-test.sock")
	l, err := net.Listen("unix", p)
	if err != nil {
		t.Fatal(err)
	}
	hold := make(chan struct{})
	t.Cleanup(func() { close(hold); l.Close() })
	got := make(chan string, 4)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				var sb strings.Builder
				buf := make([]byte, 1024)
				for {
					n, err := c.Read(buf)
					sb.Write(buf[:n])
					if err != nil {
						break
					}
				}
				got <- sb.String()
				if reply == "" {
					<-hold // accept and sit on it: the client must give up
					return
				}
				_, _ = c.Write([]byte(reply))
			}()
		}
	}()
	return p, got
}

func TestPiInboxPathShortStableAndPerSession(t *testing.T) {
	sock := "/var/folders/ab/cdef/T/hive/hived.sock"
	a := PiInboxPath(sock, "0f8fad5b-d9cb-469f-a165-70867728950e")
	b := PiInboxPath(sock, "1f8fad5b-d9cb-469f-a165-70867728950e")
	if a == "" || a == b {
		t.Fatalf("paths %q, %q: want distinct and non-empty", a, b)
	}
	if a != PiInboxPath(sock, "0f8fad5b-d9cb-469f-a165-70867728950e") {
		t.Fatal("path is not stable")
	}
	if filepath.Dir(a) != filepath.Dir(sock) || len(a) >= 104 {
		t.Fatalf("path %q should sit beside the events socket and fit sun_path", a)
	}
}

func TestPiInboxPathOffWhenUnusable(t *testing.T) {
	if PiInboxPath("", "abc") != "" || PiInboxPath("/a/b.sock", "") != "" {
		t.Fatal("missing socket path or id must disable the inbox")
	}
	long := "/" + strings.Repeat("d", 100) + "/hived.sock"
	if p := PiInboxPath(long, "0f8fad5b-d9cb"); p != "" {
		t.Fatalf("over-long path %q must disable the inbox", p)
	}
}

func TestSendToPiWritesNoteAndAwaitsAck(t *testing.T) {
	dir := shortDir(t)
	p, got := fakeExtension(t, dir, "ok\n")
	if err := SendToPi(p, "line one\nline two"); err != nil {
		t.Fatalf("SendToPi: %v", err)
	}
	if text := <-got; text != "line one\nline two" {
		t.Fatalf("extension received %q", text)
	}
}

// An extension that accepts and never answers must fail within the
// deadline, so the caller types the note instead of hanging.
func TestSendToPiNoAckIsError(t *testing.T) {
	dir := shortDir(t)
	p, _ := fakeExtension(t, dir, "")
	start := time.Now()
	if err := sendPiInbox(p, "x", time.Second, 200*time.Millisecond); err == nil {
		t.Fatal("no ack must be an error")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %v; the ack deadline did not bite", d)
	}
}

func TestSendToPiWrongAckIsError(t *testing.T) {
	dir := shortDir(t)
	p, _ := fakeExtension(t, dir, "no\n")
	if err := SendToPi(p, "x"); err == nil {
		t.Fatal("a reply other than ok must be an error")
	}
}

func TestSendToPiRefusesWritableDir(t *testing.T) {
	dir := shortDir(t)
	p, _ := fakeExtension(t, dir, "ok\n")
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := SendToPi(p, "x"); err != ErrNoInbox {
		t.Fatalf("err = %v, want ErrNoInbox for a world-writable dir", err)
	}
}

func TestSendToPiRefusesRegularFile(t *testing.T) {
	dir := shortDir(t)
	p := filepath.Join(dir, "pi-file.sock")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SendToPi(p, "x"); err != ErrNoInbox {
		t.Fatalf("err = %v, want ErrNoInbox for a non-socket", err)
	}
}

// A refused dial leaves the socket file alone: a full backlog reads the
// same, and a restarted extension may own the path now.
func TestSendToPiKeepsSocketOnRefusedDial(t *testing.T) {
	dir := shortDir(t)
	p := filepath.Join(dir, "pi-stale.sock")
	l, err := net.Listen("unix", p)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close()
	if err := SendToPi(p, "x"); err == nil {
		t.Fatal("dialing a dead socket must fail")
	}
	if _, err := os.Lstat(p); err != nil {
		t.Fatalf("socket file removed on a refused dial: %v", err)
	}
}
