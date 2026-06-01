# Remote Claude Code via Hive — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a `hive` terminal CLI that attaches to live `hived` sessions over its Unix socket, so a remote SSH session is the *same* persistent session the desktop GUI shows — then stand up Tailscale + Windows OpenSSH + Wake-on-LAN so the PC can be woken and driven from away.

**Architecture:** A new `cmd/hive` binary (thin main) delegates to a new `internal/client` package that speaks the existing `internal/wire` protocol v1. No daemon changes — the daemon already keeps PTY sessions alive independent of clients and replays scrollback on attach (`FrameRequestReplay`). The CLI dials `daemon.SocketPath()`, lists sessions in control mode, and attaches in attach mode: it puts the local terminal in raw mode, pumps `FrameData` both ways, sends `FrameResize` on size change, and detaches cleanly on `Ctrl-A d` leaving the session running. Part 2 is a guided infra runbook (not code).

**Tech Stack:** Go 1.25, `internal/wire` (framed protocol), `golang.org/x/term` (already a dep) for raw mode + size, `net` AF_UNIX (works on Windows 10+). Tailscale, Windows OpenSSH Server, `wakeonlan` on Raspberry Pi OS.

**Scope note:** Part 1 (the `hive` CLI) is the testable software deliverable and is built TDD. Part 2 (Tailscale/SSH/WoL) is manual device configuration — it's a verification runbook with explicit pass criteria, not TDD tasks.

---

## File Structure

**Create:**
- `cmd/hive/main.go` — arg parsing + dispatch (`ls`, `attach`); thin wrapper, no logic.
- `cmd/hive/term_unix.go` — `// +build !windows` raw-mode + SIGWINCH resize source.
- `cmd/hive/term_windows.go` — `// +build windows` raw-mode + ticker-polled resize source.
- `internal/client/client.go` — `Dial`, `List` (control-mode round trip).
- `internal/client/format.go` — `FormatSessions` (pure table rendering).
- `internal/client/resolve.go` — `ResolveSession` (pure: 0/1/N selection).
- `internal/client/detach.go` — `DetachScanner` (pure Ctrl-A d state machine).
- `internal/client/attach.go` — `Attach` orchestration (pumps + resize + replay), terminal-agnostic via small interfaces.
- `internal/client/format_test.go`, `resolve_test.go`, `detach_test.go` — pure unit tests (cross-platform).
- `internal/client/attach_test.go` — attach pumps against in-memory pipes (cross-platform).
- `internal/client/integration_test.go` — full round trip against a real daemon (skips on Windows, mirrors `internal/daemon` test harness).

**Modify:**
- `build.sh` — add `cmd/hive` build target.
- `README.md` — add `hive` to the Windows/Linux cross-build lines and the layout section.

**No daemon files change.**

---

## Task 1: Scaffold the package and a walking-skeleton binary

**Files:**
- Create: `internal/client/client.go`
- Create: `cmd/hive/main.go`
- Test: `internal/client/client_test.go`

- [ ] **Step 1: Write the failing test**

`internal/client/client_test.go`:
```go
package client

import "testing"

func TestVersionStringIsNonEmpty(t *testing.T) {
	if ClientName() == "" {
		t.Fatal("ClientName() must not be empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/ -run TestVersionStringIsNonEmpty -v`
Expected: FAIL — `undefined: ClientName`.

- [ ] **Step 3: Write minimal implementation**

`internal/client/client.go`:
```go
// Package client implements the hive terminal CLI: it speaks the
// internal/wire protocol to a running hived over its Unix socket,
// listing and attaching to sessions from a plain terminal.
package client

// ClientName is the wire Hello.Client identifier for this binary.
func ClientName() string { return "hive-cli/0.1" }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/ -run TestVersionStringIsNonEmpty -v`
Expected: PASS.

- [ ] **Step 5: Create the binary skeleton**

`cmd/hive/main.go`:
```go
// Command hive is the terminal client for hived. It lists and attaches
// to live daemon sessions from a plain terminal (e.g. over SSH), so a
// remote session is the same session the Hive GUI shows.
package main

import (
	"fmt"
	"os"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: hive <command>")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  ls               list sessions")
	fmt.Fprintln(os.Stderr, "  attach [id]      attach to a session")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ls":
		fmt.Fprintln(os.Stderr, "ls: not implemented yet")
		os.Exit(1)
	case "attach":
		fmt.Fprintln(os.Stderr, "attach: not implemented yet")
		os.Exit(1)
	default:
		usage()
		os.Exit(2)
	}
}
```

- [ ] **Step 6: Verify it builds and runs**

Run: `go build ./cmd/hive/ && go run ./cmd/hive/`
Expected: prints usage, exits 2.

- [ ] **Step 7: Commit**

```bash
git add internal/client/client.go internal/client/client_test.go cmd/hive/main.go
git commit -m "feat(hive): scaffold hive CLI package and binary skeleton"
```

---

## Task 2: `Dial` — connect to the daemon socket with a friendly error

**Files:**
- Modify: `internal/client/client.go`
- Test: `internal/client/client_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/client/client_test.go`:
```go
func TestDial_NoDaemonGivesFriendlyError(t *testing.T) {
	_, err := Dial("/nonexistent/path/hived.sock")
	if err == nil {
		t.Fatal("expected error dialing a missing socket")
	}
	if got := err.Error(); !strings.Contains(got, "is Hive running") {
		t.Fatalf("error should hint the daemon is down, got: %q", got)
	}
}
```
Add `"strings"` to the test imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/ -run TestDial_NoDaemon -v`
Expected: FAIL — `undefined: Dial`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/client/client.go` (add imports `fmt`, `net`):
```go
import (
	"fmt"
	"net"
)

// Dial connects to a running hived at socketPath. Unlike the GUI, it
// never spawns a daemon — if nothing is listening it returns a friendly
// error pointing at the most likely cause.
func Dial(socketPath string) (net.Conn, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("cannot reach hived at %s — is Hive running? (%w)", socketPath, err)
	}
	return conn, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/ -run TestDial_NoDaemon -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/client/client.go internal/client/client_test.go
git commit -m "feat(hive): add Dial with friendly daemon-down error"
```

---

## Task 3: `FormatSessions` — render the `ls` table (pure)

**Files:**
- Create: `internal/client/format.go`
- Test: `internal/client/format_test.go`

- [ ] **Step 1: Write the failing test**

`internal/client/format_test.go`:
```go
package client

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

func TestFormatSessions_RendersIDNameAgentAlive(t *testing.T) {
	out := FormatSessions([]wire.SessionInfo{
		{ID: "abc123", Name: "api", Agent: "claude", Alive: true},
		{ID: "def456", Name: "scratch", Agent: "", Alive: false},
	})
	for _, want := range []string{"abc123", "api", "claude", "def456", "scratch", "shell", "dead"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

func TestFormatSessions_EmptyIsHelpful(t *testing.T) {
	out := FormatSessions(nil)
	if !strings.Contains(out, "no sessions") {
		t.Errorf("empty list should say so, got: %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/ -run TestFormatSessions -v`
Expected: FAIL — `undefined: FormatSessions`.

- [ ] **Step 3: Write minimal implementation**

`internal/client/format.go`:
```go
package client

import (
	"fmt"
	"strings"

	"github.com/lucascaro/hive/internal/wire"
)

// FormatSessions renders a session list as an aligned text table.
func FormatSessions(sessions []wire.SessionInfo) string {
	if len(sessions) == 0 {
		return "no sessions\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-12s  %-16s  %-8s  %s\n", "ID", "NAME", "AGENT", "STATE")
	for _, s := range sessions {
		agent := s.Agent
		if agent == "" {
			agent = "shell"
		}
		state := "alive"
		if !s.Alive {
			state = "dead"
		}
		id := s.ID
		if len(id) > 12 {
			id = id[:12]
		}
		fmt.Fprintf(&b, "%-12s  %-16s  %-8s  %s\n", id, s.Name, agent, state)
	}
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/ -run TestFormatSessions -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/client/format.go internal/client/format_test.go
git commit -m "feat(hive): add FormatSessions table renderer"
```

---

## Task 4: `List` — control-mode session listing

**Files:**
- Modify: `internal/client/client.go`
- Test: `internal/client/integration_test.go`

The daemon, after a control handshake, pushes an unsolicited PROJECTS then SESSIONS snapshot. `List` sends `FrameListSessions` explicitly and reads frames until the first `FrameSessions`, so it works regardless of unsolicited ordering.

- [ ] **Step 1: Write the integration-test harness + failing test**

`internal/client/integration_test.go`:
```go
package client

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/session"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("daemon PTY test harness is POSIX-only; client logic is covered by unit tests")
	}
}

// startDaemon boots a real hived on a temp socket with one long-lived
// bootstrap session ("cat" blocks on stdin, so the PTY stays alive).
func startDaemon(t *testing.T) (sock string) {
	t.Helper()
	skipOnWindows(t)
	dir := t.TempDir()
	sock = filepath.Join(dir, "hived.sock")
	d, err := daemon.New(daemon.Config{
		SocketPath:       sock,
		StateDir:         filepath.Join(dir, "state"),
		BootstrapSession: session.Options{Name: "boot", Cmd: []string{"cat"}, Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = d.Run(ctx) }()
	t.Cleanup(func() { cancel(); _ = d.Close() })

	// Wait for the socket to accept connections.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("unix", sock); err == nil {
			_ = c.Close()
			return sock
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("daemon socket never appeared: %v", err)
	}
	return sock
}

func TestList_ReturnsBootstrapSession(t *testing.T) {
	sock := startDaemon(t)
	conn, err := Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	sessions, err := List(conn)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Name != "boot" {
		t.Fatalf("expected one 'boot' session, got %+v", sessions)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/ -run TestList_ReturnsBootstrapSession -v`
Expected: FAIL — `undefined: List`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/client/client.go` (add imports `time`, and `github.com/lucascaro/hive/internal/wire`):
```go
// List opens a control-mode conversation on conn and returns the
// daemon's current sessions. conn must be freshly dialed (no prior
// handshake). It is consumed by this call.
func List(conn net.Conn) ([]wire.SessionInfo, error) {
	if err := wire.WriteJSON(conn, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION,
		Client:  ClientName(),
		Mode:    wire.ModeControl,
	}); err != nil {
		return nil, fmt.Errorf("control hello: %w", err)
	}
	var welcome wire.Welcome
	ft, err := wire.ReadJSON(conn, &welcome)
	if err != nil {
		return nil, fmt.Errorf("control welcome: %w", err)
	}
	if ft != wire.FrameWelcome {
		return nil, fmt.Errorf("control: expected WELCOME, got %s", ft)
	}
	if err := wire.WriteJSON(conn, wire.FrameListSessions, wire.ListSessionsReq{}); err != nil {
		return nil, fmt.Errorf("list request: %w", err)
	}
	// Read until the first SESSIONS frame (the daemon may push an
	// unsolicited PROJECTS snapshot first).
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	for {
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			return nil, fmt.Errorf("list read: %w", err)
		}
		if ft != wire.FrameSessions {
			continue
		}
		var resp wire.SessionsResp
		if len(payload) > 0 {
			if err := jsonUnmarshal(payload, &resp); err != nil {
				return nil, fmt.Errorf("list decode: %w", err)
			}
		}
		return resp.Sessions, nil
	}
}
```

Add a tiny JSON helper at the bottom of `client.go` (add import `encoding/json`):
```go
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/ -run TestList_ReturnsBootstrapSession -v`
Expected: PASS (on Linux/macOS). On Windows it SKIPS.

- [ ] **Step 5: Wire `ls` into the binary**

In `cmd/hive/main.go`, replace the `case "ls":` body:
```go
	case "ls":
		runLS()
```
And add (imports `github.com/lucascaro/hive/internal/client`, `github.com/lucascaro/hive/internal/daemon`):
```go
func runLS() {
	conn, err := client.Dial(daemon.SocketPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	sessions, err := client.List(conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hive ls:", err)
		os.Exit(1)
	}
	fmt.Print(client.FormatSessions(sessions))
}
```

- [ ] **Step 6: Verify build**

Run: `go build ./cmd/hive/ && go vet ./cmd/hive/ ./internal/client/`
Expected: no output (success).

- [ ] **Step 7: Commit**

```bash
git add internal/client/client.go internal/client/integration_test.go cmd/hive/main.go
git commit -m "feat(hive): implement 'hive ls' session listing"
```

---

## Task 5: `ResolveSession` — pick a target for `attach` (pure)

**Files:**
- Create: `internal/client/resolve.go`
- Test: `internal/client/resolve_test.go`

- [ ] **Step 1: Write the failing test**

`internal/client/resolve_test.go`:
```go
package client

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

var twoSessions = []wire.SessionInfo{
	{ID: "aaa", Name: "api", Alive: true},
	{ID: "bbb", Name: "web", Alive: true},
}

func TestResolveSession_ExplicitIDWins(t *testing.T) {
	id, err := ResolveSession(twoSessions, "bbb")
	if err != nil || id != "bbb" {
		t.Fatalf("got (%q,%v), want (bbb,nil)", id, err)
	}
}

func TestResolveSession_UnknownIDErrors(t *testing.T) {
	_, err := ResolveSession(twoSessions, "zzz")
	if err == nil || !strings.Contains(err.Error(), "no session") {
		t.Fatalf("want 'no session' error, got %v", err)
	}
}

func TestResolveSession_NoArgSingleSession(t *testing.T) {
	id, err := ResolveSession(twoSessions[:1], "")
	if err != nil || id != "aaa" {
		t.Fatalf("got (%q,%v), want (aaa,nil)", id, err)
	}
}

func TestResolveSession_NoArgMultipleErrors(t *testing.T) {
	_, err := ResolveSession(twoSessions, "")
	if err == nil || !strings.Contains(err.Error(), "specify a session id") {
		t.Fatalf("want 'specify a session id' error, got %v", err)
	}
}

func TestResolveSession_NoneErrors(t *testing.T) {
	_, err := ResolveSession(nil, "")
	if err == nil || !strings.Contains(err.Error(), "no sessions") {
		t.Fatalf("want 'no sessions' error, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/ -run TestResolveSession -v`
Expected: FAIL — `undefined: ResolveSession`.

- [ ] **Step 3: Write minimal implementation**

`internal/client/resolve.go`:
```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/ -run TestResolveSession -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/client/resolve.go internal/client/resolve_test.go
git commit -m "feat(hive): add ResolveSession target selection"
```

---

## Task 6: `DetachScanner` — the `Ctrl-A d` state machine (pure)

**Files:**
- Create: `internal/client/detach.go`
- Test: `internal/client/detach_test.go`

Semantics (tmux/screen-style), prefix = `Ctrl-A` (0x01):
- `Ctrl-A` then `d` (or `D`) → **detach** (do not forward either byte).
- `Ctrl-A` then `Ctrl-A` → forward a single literal `Ctrl-A`.
- `Ctrl-A` then any other byte X → forward `Ctrl-A` then X.
- A lone `Ctrl-A` at end of a chunk is held until the next byte arrives.

- [ ] **Step 1: Write the failing test**

`internal/client/detach_test.go`:
```go
package client

import (
	"bytes"
	"testing"
)

func feed(s *DetachScanner, in []byte) (out []byte, detached bool) {
	var buf bytes.Buffer
	for _, b := range in {
		fwd, det := s.Push(b)
		buf.Write(fwd)
		if det {
			return buf.Bytes(), true
		}
	}
	return buf.Bytes(), false
}

func TestDetach_PlainBytesPassThrough(t *testing.T) {
	out, det := feed(&DetachScanner{}, []byte("hello"))
	if det || !bytes.Equal(out, []byte("hello")) {
		t.Fatalf("got (%q,%v)", out, det)
	}
}

func TestDetach_CtrlADTriggersDetach(t *testing.T) {
	out, det := feed(&DetachScanner{}, []byte{0x01, 'd'})
	if !det {
		t.Fatal("expected detach on Ctrl-A d")
	}
	if len(out) != 0 {
		t.Fatalf("detach must not forward bytes, got %q", out)
	}
}

func TestDetach_DoubleCtrlAForwardsLiteral(t *testing.T) {
	out, det := feed(&DetachScanner{}, []byte{0x01, 0x01})
	if det || !bytes.Equal(out, []byte{0x01}) {
		t.Fatalf("got (%q,%v), want single 0x01", out, det)
	}
}

func TestDetach_CtrlAThenOtherForwardsBoth(t *testing.T) {
	out, det := feed(&DetachScanner{}, []byte{0x01, 'x'})
	if det || !bytes.Equal(out, []byte{0x01, 'x'}) {
		t.Fatalf("got (%q,%v), want 0x01 'x'", out, det)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/ -run TestDetach -v`
Expected: FAIL — `undefined: DetachScanner`.

- [ ] **Step 3: Write minimal implementation**

`internal/client/detach.go`:
```go
package client

// prefixByte is the detach prefix: Ctrl-A.
const prefixByte = 0x01

// DetachScanner inspects the client's keystroke stream for the
// Ctrl-A d detach sequence. Feed it one byte at a time with Push; it
// returns the bytes to forward to the session and whether the user
// asked to detach. The zero value is ready to use.
type DetachScanner struct {
	armed bool // last byte was the unconsumed prefix
}

// Push processes one input byte. forward is the (possibly empty) slice
// of bytes to send to the session; detach is true when the user typed
// the detach sequence.
func (s *DetachScanner) Push(b byte) (forward []byte, detach bool) {
	if s.armed {
		s.armed = false
		switch b {
		case 'd', 'D':
			return nil, true
		case prefixByte:
			return []byte{prefixByte}, false
		default:
			return []byte{prefixByte, b}, false
		}
	}
	if b == prefixByte {
		s.armed = true
		return nil, false
	}
	return []byte{b}, false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/ -run TestDetach -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/client/detach.go internal/client/detach_test.go
git commit -m "feat(hive): add Ctrl-A d detach scanner"
```

---

## Task 7: `Attach` — bidirectional pumps, resize, replay, detach (terminal-agnostic)

**Files:**
- Create: `internal/client/attach.go`
- Test: `internal/client/attach_test.go`

`Attach` is written against interfaces so it can be tested with in-memory pipes (no TTY). The real terminal wiring lands in Task 8.

- [ ] **Step 1: Write the failing test**

`internal/client/attach_test.go`:
```go
package client

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// fakeTerm is an in-memory Terminal: stdin is a pipe we write to, stdout
// is a buffer we inspect, size is fixed, and resize never fires.
type fakeTerm struct {
	in     io.Reader
	out    *syncBuf
	resize chan struct{}
}

func (f *fakeTerm) Stdin() io.Reader            { return f.in }
func (f *fakeTerm) Stdout() io.Writer           { return f.out }
func (f *fakeTerm) Size() (int, int, error)     { return 80, 24, nil }
func (f *fakeTerm) ResizeEvents() <-chan struct{} { return f.resize }

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// serverEnd runs a minimal fake daemon attach side on one end of a
// socketpair: it sends WELCOME then DATA("hi"), and records bytes the
// client forwards.
func serverEnd(t *testing.T, conn net.Conn, gotInput *syncBuf, done chan<- struct{}) {
	defer close(done)
	// Expect HELLO.
	if ft, _, err := wire.ReadFrame(conn); err != nil || ft != wire.FrameHello {
		t.Errorf("server: want HELLO, got %s err=%v", ft, err)
		return
	}
	_ = wire.WriteJSON(conn, wire.FrameWelcome, wire.Welcome{Mode: wire.ModeAttach, Cols: 80, Rows: 24})
	_ = wire.WriteFrame(conn, wire.FrameData, []byte("hi"))
	// Echo subsequent client frames' payloads into gotInput until close.
	for {
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			return
		}
		if ft == wire.FrameData {
			gotInput.Write(payload)
		}
	}
}

func TestAttach_StreamsBothWaysAndDetaches(t *testing.T) {
	srv, cli := net.Pipe()
	gotInput := &syncBuf{}
	srvDone := make(chan struct{})
	go serverEnd(t, srv, gotInput, srvDone)

	// Client stdin: type "ok" then Ctrl-A d to detach.
	stdin := bytes.NewReader([]byte{'o', 'k', 0x01, 'd'})
	out := &syncBuf{}
	term := &fakeTerm{in: stdin, out: out, resize: make(chan struct{})}

	errc := make(chan error, 1)
	go func() { errc <- Attach(cli, "sess1", term, AttachOptions{RequestReplay: false}) }()

	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("Attach returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Attach did not return after detach")
	}

	if !bytes.Contains([]byte(out.String()), []byte("hi")) {
		t.Errorf("stdout missing server data %q", out.String())
	}
	srv.Close()
	<-srvDone
	if got := gotInput.String(); got != "ok" {
		t.Errorf("server got input %q, want %q (Ctrl-A d must not be forwarded)", got, "ok")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/client/ -run TestAttach_StreamsBothWays -v`
Expected: FAIL — `undefined: Attach` / `undefined: Terminal` / `undefined: AttachOptions`.

- [ ] **Step 3: Write minimal implementation**

`internal/client/attach.go`:
```go
package client

import (
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/lucascaro/hive/internal/wire"
)

// Terminal abstracts the local terminal so Attach is testable without a
// real TTY. Stdin/Stdout are the raw byte streams; Size reports the
// current dimensions; ResizeEvents fires whenever the size changes.
type Terminal interface {
	Stdin() io.Reader
	Stdout() io.Writer
	Size() (cols, rows int, err error)
	ResizeEvents() <-chan struct{}
}

// AttachOptions tunes attach behavior.
type AttachOptions struct {
	// RequestReplay asks the daemon to repaint scrollback on attach.
	RequestReplay bool
}

// Attach performs the attach handshake on conn (freshly dialed) for
// sessionID, then streams PTY data to term.Stdout and term keystrokes
// to the session until the user detaches (Ctrl-A d) or the connection
// closes. It returns nil on a clean detach or session exit.
func Attach(conn net.Conn, sessionID string, term Terminal, opts AttachOptions) error {
	if err := wire.WriteJSON(conn, wire.FrameHello, wire.Hello{
		Version:   wire.PROTOCOL_VERSION,
		Client:    ClientName(),
		Mode:      wire.ModeAttach,
		SessionID: sessionID,
	}); err != nil {
		return fmt.Errorf("attach hello: %w", err)
	}
	ft, payload, err := wire.ReadFrame(conn)
	if err != nil {
		return fmt.Errorf("attach welcome: %w", err)
	}
	if ft == wire.FrameError {
		var werr wire.Error
		if jsonUnmarshal(payload, &werr) == nil && werr.Message != "" {
			return fmt.Errorf("attach rejected: %s", werr.Message)
		}
		return errors.New("attach rejected by daemon")
	}
	if ft != wire.FrameWelcome {
		return fmt.Errorf("attach: expected WELCOME, got %s", ft)
	}

	// Send our current size, then optionally request scrollback replay.
	if cols, rows, err := term.Size(); err == nil && cols > 0 && rows > 0 {
		_ = wire.WriteJSON(conn, wire.FrameResize, wire.Resize{Cols: cols, Rows: rows})
	}
	if opts.RequestReplay {
		_ = wire.WriteFrame(conn, wire.FrameRequestReplay, nil)
	}

	// reader: daemon -> stdout. Closes when the connection ends.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			ft, payload, err := wire.ReadFrame(conn)
			if err != nil {
				return
			}
			switch ft {
			case wire.FrameData:
				_, _ = term.Stdout().Write(payload)
			case wire.FrameEvent, wire.FrameError:
				// Scrollback boundary / errors: ignored for raw passthrough.
			}
		}
	}()

	// resize: term -> daemon.
	resizeStop := make(chan struct{})
	go func() {
		for {
			select {
			case <-resizeStop:
				return
			case <-term.ResizeEvents():
				if cols, rows, err := term.Size(); err == nil && cols > 0 && rows > 0 {
					_ = wire.WriteJSON(conn, wire.FrameResize, wire.Resize{Cols: cols, Rows: rows})
				}
			}
		}
	}()
	defer close(resizeStop)

	// writer: stdin -> daemon, watching for the detach sequence. Runs on
	// this goroutine so a clean detach returns from Attach.
	var scanner DetachScanner
	buf := make([]byte, 4096)
	in := term.Stdin()
	for {
		select {
		case <-readerDone:
			return nil // connection closed (session exited or daemon gone)
		default:
		}
		n, rerr := in.Read(buf)
		for i := 0; i < n; i++ {
			fwd, detach := scanner.Push(buf[i])
			if len(fwd) > 0 {
				if err := wire.WriteFrame(conn, wire.FrameData, fwd); err != nil {
					return nil // connection gone
				}
			}
			if detach {
				return nil
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return nil
			}
			return fmt.Errorf("stdin read: %w", rerr)
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/client/ -run TestAttach_StreamsBothWays -v`
Expected: PASS.

- [ ] **Step 5: Run the full package test suite**

Run: `go test ./internal/client/ -v`
Expected: all PASS (integration test skips on Windows).

- [ ] **Step 6: Commit**

```bash
git add internal/client/attach.go internal/client/attach_test.go
git commit -m "feat(hive): add Attach with bidirectional pumps, resize, and detach"
```

---

## Task 8: Real terminal wiring (`cmd/hive`) + raw mode + resize source

**Files:**
- Create: `cmd/hive/term_unix.go`
- Create: `cmd/hive/term_windows.go`
- Modify: `cmd/hive/main.go`

No automated test — this is the OS-specific glue (TTY raw mode, SIGWINCH vs polling). It's verified by the manual smoke test in Task 9. Keep it thin so all logic stays in the tested `internal/client`.

- [ ] **Step 1: Implement the shared terminal type + Unix resize source**

`cmd/hive/term_unix.go`:
```go
//go:build !windows

package main

import (
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

// osTerm implements client.Terminal over the real process stdio.
type osTerm struct {
	resize chan struct{}
}

func newTerm() (*osTerm, func(), error) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, nil, err
	}
	restore := func() { _ = term.Restore(fd, oldState) }

	t := &osTerm{resize: make(chan struct{}, 1)}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	go func() {
		for range sig {
			select {
			case t.resize <- struct{}{}:
			default:
			}
		}
	}()
	cleanup := func() { signal.Stop(sig); close(sig); restore() }
	return t, cleanup, nil
}

func (t *osTerm) Stdin() io.Reader              { return os.Stdin }
func (t *osTerm) Stdout() io.Writer             { return os.Stdout }
func (t *osTerm) ResizeEvents() <-chan struct{} { return t.resize }
func (t *osTerm) Size() (int, int, error) {
	return term.GetSize(int(os.Stdout.Fd()))
}
```

- [ ] **Step 2: Implement the Windows variant (poll size; no SIGWINCH)**

`cmd/hive/term_windows.go`:
```go
//go:build windows

package main

import (
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

// osTerm implements client.Terminal over the real process stdio.
// Windows has no SIGWINCH, so size changes are detected by polling.
type osTerm struct {
	resize chan struct{}
	stop   chan struct{}
}

func newTerm() (*osTerm, func(), error) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, nil, err
	}
	restore := func() { _ = term.Restore(fd, oldState) }

	t := &osTerm{resize: make(chan struct{}, 1), stop: make(chan struct{})}
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		lastC, lastR, _ := t.Size()
		for {
			select {
			case <-t.stop:
				return
			case <-ticker.C:
				c, r, err := t.Size()
				if err == nil && (c != lastC || r != lastR) {
					lastC, lastR = c, r
					select {
					case t.resize <- struct{}{}:
					default:
					}
				}
			}
		}
	}()
	cleanup := func() { close(t.stop); restore() }
	return t, cleanup, nil
}

func (t *osTerm) Stdin() io.Reader              { return os.Stdin }
func (t *osTerm) Stdout() io.Writer             { return os.Stdout }
func (t *osTerm) ResizeEvents() <-chan struct{} { return t.resize }
func (t *osTerm) Size() (int, int, error) {
	return term.GetSize(int(os.Stdout.Fd()))
}
```

- [ ] **Step 3: Wire `attach` into main**

In `cmd/hive/main.go`, replace the `case "attach":` body:
```go
	case "attach":
		runAttach(os.Args[2:])
```
Add:
```go
func runAttach(args []string) {
	var idArg string
	if len(args) > 0 {
		idArg = args[0]
	}

	conn, err := client.Dial(daemon.SocketPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	// Resolve the target via a separate control connection.
	lsConn, err := client.Dial(daemon.SocketPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sessions, err := client.List(lsConn)
	_ = lsConn.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hive attach:", err)
		os.Exit(1)
	}
	id, err := client.ResolveSession(sessions, idArg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hive attach:", err)
		os.Exit(1)
	}

	t, cleanup, err := newTerm()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hive attach: terminal:", err)
		os.Exit(1)
	}
	defer cleanup()

	fmt.Fprintf(os.Stderr, "attached to %s — detach with Ctrl-A d\r\n", id)
	if err := client.Attach(conn, id, t, client.AttachOptions{RequestReplay: true}); err != nil {
		cleanup()
		fmt.Fprintln(os.Stderr, "hive attach:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Verify both build targets compile**

Run:
```bash
go build ./cmd/hive/
GOOS=windows GOARCH=amd64 go build ./cmd/hive/
```
Expected: both succeed, no output.

- [ ] **Step 5: Vet**

Run: `go vet ./cmd/hive/ ./internal/client/`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add cmd/hive/term_unix.go cmd/hive/term_windows.go cmd/hive/main.go
git commit -m "feat(hive): wire real terminal (raw mode + resize) into attach"
```

---

## Task 9: Build wiring + manual smoke test against the local daemon

**Files:**
- Modify: `build.sh`
- Modify: `README.md`

- [ ] **Step 1: Inspect how `build.sh` builds the daemon**

Run: `grep -n "hived\|go build\|GOOS\|cmd/" build.sh`
Expected: shows the existing `hived` build line(s). Mirror that pattern for `cmd/hive` (same output dir as `hived`/`hivegui`).

- [ ] **Step 2: Add the `hive` build to `build.sh`**

After the line that builds `hived`, add an analogous line building `./cmd/hive` to the same `build/bin` output directory (use the exact path/flags the script already uses for `hived`, substituting `hive`/`cmd/hive`). Example shape (match the script's real variables):
```sh
go build -o "$BIN_DIR/hive$EXT" ./cmd/hive
```

- [ ] **Step 3: Update README cross-build + layout**

In `README.md`, under the Windows/Linux build commands, add beneath the `hived` lines:
```sh
# Windows
GOOS=windows GOARCH=amd64 go build -o cmd/hivegui/build/bin/hive.exe ./cmd/hive
# Linux
GOOS=linux GOARCH=amd64 go build -o cmd/hivegui/build/bin/hive ./cmd/hive
```
And add `hive` to the `cmd/` layout list with the note: `hive/ — terminal attach client (hive ls / hive attach)`.

- [ ] **Step 4: Full build + test suite**

Run:
```bash
./build.sh
go test ./...
```
Expected: build succeeds; all tests pass (client integration test skips on Windows, runs on Linux/macOS).

- [ ] **Step 5: Manual smoke test (local, no network)**

On the machine running Hive (do this in a real terminal, not CI):
1. Launch the Hive GUI and start a Claude (or shell) session in it.
2. In a separate terminal: `hive ls` → confirm the session appears with the right name/agent.
3. `hive attach <id>` → confirm:
   - scrollback repaints (you see prior output, not a blank screen),
   - typing reaches the session and output streams back live,
   - resizing the terminal reflows the session,
   - `Ctrl-A d` returns you to your shell,
   - the Hive GUI still shows the session running (it was never killed).
4. `hive attach <id>` again → confirm you rejoin the same live session.

Document the result (paste the `hive ls` output and note each check) before moving on. If any check fails, fix it under the relevant task before continuing.

- [ ] **Step 6: Commit**

```bash
git add build.sh README.md
git commit -m "build(hive): build hive CLI and document it"
```

---

## Part 2 — Remote access runbook (manual, with pass criteria)

Not code. Perform on the devices; verify each gate before the next. Do these in order — each builds on the previous and is independently testable.

### Stage A — Windows OpenSSH Server (LAN-only first)

- [ ] Enable the server: in an elevated PowerShell,
  `Add-WindowsCapability -Online -Name OpenSSH.Server~~~~0.0.1.0`
- [ ] Start + auto-start: `Start-Service sshd; Set-Service -Name sshd -StartupType Automatic`
- [ ] Install your client's public key. For an admin account, append it to
  `C:\ProgramData\ssh\administrators_authorized_keys` and fix ACLs
  (owner Administrators/SYSTEM only) — this file's permissions are the
  usual reason key auth silently fails.
- [ ] (Optional) set default shell to PowerShell:
  `New-ItemProperty -Path "HKLM:\SOFTWARE\OpenSSH" -Name DefaultShell -Value "C:\Program Files\PowerShell\7\pwsh.exe" -PropertyType String -Force`
- [ ] **Gate:** from another machine on the LAN, `ssh <user>@<pc-lan-ip>` logs in with the key (no password), and `hive ls` works over that SSH session.

### Stage B — Wake-on-LAN

- [ ] **BIOS/UEFI:** enable "Wake on LAN" / "Power On by PCIe/PCI"; disable ErP/EuP deep-sleep if present.
- [ ] **Windows NIC:** Device Manager → your Ethernet adapter → Power Management: check "Allow this device to wake the computer" and "Only allow a magic packet…"; Advanced tab → enable "Wake on Magic Packet".
- [ ] **Disable Fast Startup:** Control Panel → Power Options → "Choose what the power buttons do" → uncheck "Turn on fast startup". (It silently blocks wake-from-shutdown.)
- [ ] Note the Ethernet adapter's MAC: `getmac /v` (or `ipconfig /all`).
- [ ] **On the Pi:** `sudo apt update && sudo apt install -y wakeonlan`. Create `~/bin/wake-pc`:
  ```sh
  #!/bin/sh
  exec wakeonlan AA:BB:CC:DD:EE:FF   # ← the PC's Ethernet MAC
  ```
  `chmod +x ~/bin/wake-pc`.
- [ ] **Gate (sleep):** put the PC to sleep; from the Pi run `~/bin/wake-pc`; PC wakes within a few seconds.
- [ ] **Gate (shutdown):** fully shut down; run `wake-pc`; PC powers on. If it doesn't, the board doesn't support wake-from-S5 — fall back to using Sleep instead of shutdown and note that.

### Stage C — Tailscale (remote access)

- [ ] Install Tailscale on PC, Pi, and phone/laptop; sign all into one tailnet.
- [ ] Enable MagicDNS in the Tailscale admin console; confirm `mypc` / `mypi` names resolve.
- [ ] Confirm the Windows Tailscale service is set to start at boot (so the PC rejoins the tailnet after a WoL wake without a login).
- [ ] Lock down OpenSSH to Tailscale only (optional hardening): a Windows firewall rule allowing TCP 22 inbound only on the Tailscale interface.
- [ ] **Gate:** from off your home network (e.g. phone on cellular):
  ```
  ssh mypi 'wake-pc'      # PC wakes
  # wait ~20–30s
  ssh mypc                # connects over Tailscale
  hive attach             # rejoin the live Hive session
  ```

### Stage D — Convenience (optional)

- [ ] Laptop one-shot wrapper `connect-pc`:
  ```sh
  #!/bin/sh
  ssh mypi 'wake-pc'
  printf 'waking'; until ping -c1 -W1 mypc >/dev/null 2>&1; do printf '.'; sleep 2; done; echo
  exec ssh -t mypc hive attach
  ```
  `chmod +x` and drop on PATH.

---

## Self-Review (completed)

- **Spec coverage:** `hive ls` (Task 4), `hive attach` resolve/attach/replay/detach (Tasks 5–8), raw mode + resize incl. Windows polling (Task 8), build + ship (Task 9), Tailscale/OpenSSH/WoL/wrapper (Part 2 Stages A–D). No daemon changes needed — confirmed sessions outlive client detach. All spec sections map to a task.
- **Placeholder scan:** none — every code step has complete code; the only intentional fill-ins are the user's real MAC address (Part 2) and matching `build.sh`'s existing variable names (Task 9 Step 2, which Step 1 has the engineer read first).
- **Type consistency:** `Terminal` interface methods (`Stdin`/`Stdout`/`Size`/`ResizeEvents`) match between `attach.go`, `attach_test.go`, and `cmd/hive` `osTerm`. `AttachOptions{RequestReplay}`, `DetachScanner.Push`, `ResolveSession`, `List`, `FormatSessions`, `Dial`, `ClientName` signatures are consistent across tasks and call sites. Wire types/frames used (`FrameHello`/`FrameWelcome`/`FrameData`/`FrameResize`/`FrameRequestReplay`/`FrameListSessions`/`FrameSessions`/`Hello`/`Welcome`/`SessionInfo`/`SessionsResp`/`Resize`/`Error`) all exist in `internal/wire`.
