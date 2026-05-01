// Package session models a single hived session: one PTY, the shell
// running on it, and the live scrollback ring of its output.
package session

import (
	"errors"
	"io"
	"log"
	"os"
	"sync"

	"github.com/aymanbagabas/go-pty"
	"github.com/google/uuid"
)

// Sink is an active output destination. The session fans PTY output to
// every registered sink. When a sink's Write returns an error, the
// session removes it.
type Sink interface {
	Write(p []byte) (int, error)
}

// Session owns a PTY, the process running on it, and a ring of recent
// output. It does not own any wire-level state — that lives in the
// daemon package, which calls Subscribe to receive bytes.
type Session struct {
	ID         string
	Scrollback *Scrollback

	cmd  *pty.Cmd
	ptmx pty.Pty

	mu    sync.Mutex
	sinks map[Sink]struct{}
	done  chan struct{}
}

// Options configures a new Session.
type Options struct {
	Shell       string
	Cwd         string // working directory for the shell; default = sane choice
	Cols, Rows  int
	ScrollBytes int
	Env         []string // appended to os.Environ()
}

// resolveCwd returns the working directory to use for a new session.
// Caller-supplied wins. Otherwise we use the daemon's own cwd, except
// when that's "/" (the typical Finder-launch case on macOS) — then we
// fall back to $HOME so sessions don't open in the filesystem root.
func resolveCwd(opt string) string {
	if opt != "" {
		return opt
	}
	if cwd, err := os.Getwd(); err == nil && cwd != "" && cwd != "/" {
		return cwd
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}

// Start spawns the shell on a new PTY. The session ID is a fresh UUID.
func Start(opts Options) (*Session, error) {
	if opts.Shell == "" {
		opts.Shell = defaultShell()
	}
	if opts.Cols == 0 {
		opts.Cols = 80
	}
	if opts.Rows == 0 {
		opts.Rows = 24
	}

	ptmx, err := pty.New()
	if err != nil {
		return nil, err
	}
	if err := ptmx.Resize(opts.Cols, opts.Rows); err != nil {
		_ = ptmx.Close()
		return nil, err
	}

	cmd := ptmx.Command(opts.Shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if len(opts.Env) > 0 {
		cmd.Env = append(cmd.Env, opts.Env...)
	}
	cmd.Dir = resolveCwd(opts.Cwd)
	if err := cmd.Start(); err != nil {
		_ = ptmx.Close()
		return nil, err
	}

	s := &Session{
		ID:         uuid.NewString(),
		Scrollback: NewScrollback(opts.ScrollBytes),
		cmd:        cmd,
		ptmx:       ptmx,
		sinks:      make(map[Sink]struct{}),
		done:       make(chan struct{}),
	}
	go s.readLoop()
	return s, nil
}

// readLoop drains the PTY into the scrollback ring and every active
// sink. It is the only goroutine that reads from the PTY.
func (s *Session) readLoop() {
	defer close(s.done)
	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			_, _ = s.Scrollback.Write(buf[:n])
			s.fanout(buf[:n])
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("session %s: pty read: %v", s.ID, err)
			}
			s.fanoutClose()
			return
		}
	}
}

func (s *Session) fanout(p []byte) {
	s.mu.Lock()
	dead := make([]Sink, 0)
	for sink := range s.sinks {
		if _, err := sink.Write(p); err != nil {
			dead = append(dead, sink)
		}
	}
	for _, d := range dead {
		delete(s.sinks, d)
	}
	s.mu.Unlock()
}

func (s *Session) fanoutClose() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sink := range s.sinks {
		if c, ok := sink.(io.Closer); ok {
			_ = c.Close()
		}
		delete(s.sinks, sink)
	}
}

// Subscribe registers a sink to receive future PTY output. Returns an
// unsubscribe function. The replay buffer is NOT sent automatically;
// the caller is responsible for replaying Scrollback.Snapshot() before
// subscribing if it wants atomic "replay then live" behavior.
//
// To prevent the gap between snapshot and subscribe from dropping bytes,
// callers should hold the session mutex via SubscribeAtomic instead.
func (s *Session) Subscribe(sink Sink) func() {
	s.mu.Lock()
	s.sinks[sink] = struct{}{}
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.sinks, sink)
		s.mu.Unlock()
	}
}

// SubscribeAtomic returns the current scrollback snapshot AND registers
// the sink for live updates under a single lock acquisition, so no
// PTY bytes are dropped between replay and live stream.
func (s *Session) SubscribeAtomic(sink Sink) (replay []byte, unsubscribe func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	replay = s.Scrollback.Snapshot()
	s.sinks[sink] = struct{}{}
	return replay, func() {
		s.mu.Lock()
		delete(s.sinks, sink)
		s.mu.Unlock()
	}
}

// Write forwards bytes from a client to the PTY (i.e. keystrokes).
func (s *Session) Write(p []byte) (int, error) {
	return s.ptmx.Write(p)
}

// Resize updates the PTY's window size. cols × rows.
func (s *Session) Resize(cols, rows int) error {
	return s.ptmx.Resize(cols, rows)
}

// Close terminates the shell and releases the PTY.
func (s *Session) Close() error {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return s.ptmx.Close()
}

// Done returns a channel that is closed when the session exits.
func (s *Session) Done() <-chan struct{} { return s.done }

func defaultShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if s := os.Getenv("ComSpec"); s != "" {
		return s
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	return "cmd.exe"
}
