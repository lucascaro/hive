// Package session models a single hived session: one PTY and the
// shell running on it. Reattach repaints come from the VT mirror.
package session

import (
	"errors"
	"fmt"
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

// Session owns a PTY and the process running on it. It does not own any
// wire-level state — that lives in the daemon package, which calls
// Subscribe to receive bytes. Reattach repaints come from the VT mirror.
type Session struct {
	ID string
	vt *VT

	cmd  *pty.Cmd
	ptmx pty.Pty

	mu        sync.Mutex
	sinks     map[Sink]struct{}
	done      chan struct{}
	vtErrOnce sync.Once
}

// Options configures a new Session.
type Options struct {
	Shell       string
	Cmd         []string // when non-empty, runs in place of $SHELL (e.g. an agent)
	Cwd         string   // working directory; default = sane choice
	Cols, Rows int
	Env        []string // appended to os.Environ()
}

// resolveCwd returns the working directory to use for a new session.
// A caller-supplied path must exist and be a directory — if it doesn't,
// we return an error rather than silently substituting a different one.
// (Without this check, fork/exec surfaces the failure as a misleading
// ENOENT pointing at the shell binary, e.g. "fork/exec /usr/local/bin/fish:
// no such file or directory", which sends users hunting for a missing
// shell when the real cause is a deleted project directory.) When no
// path is supplied, fall back to the daemon's own cwd, except when
// that's "/" (the typical Finder-launch case on macOS) — then $HOME,
// so sessions don't open in the filesystem root.
func resolveCwd(opt string) (string, error) {
	if opt != "" {
		fi, err := os.Stat(opt)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("session cwd %q does not exist (was the directory moved or deleted?)", opt)
			}
			return "", fmt.Errorf("session cwd %q: %w", opt, err)
		}
		if !fi.IsDir() {
			return "", fmt.Errorf("session cwd %q is not a directory", opt)
		}
		return opt, nil
	}
	if cwd, err := os.Getwd(); err == nil && cwd != "" && cwd != "/" {
		return cwd, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home, nil
	}
	return "", nil
}

// Start spawns a process on a new PTY. By default the process is the
// user's login shell; pass a non-empty Cmd to run something else (an
// agent, etc.). The session ID is a fresh UUID.
func Start(opts Options) (*Session, error) {
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

	shell := opts.Shell
	if shell == "" {
		shell = defaultShell()
	}

	var cmd *pty.Cmd
	if len(opts.Cmd) > 0 {
		// Run the command via the user's login + interactive shell so
		// PATH/aliases/functions set up in *either* .zprofile (login)
		// or .zshrc (interactive) apply. fnm, nvm, asdf, etc. land in
		// different rc files depending on the install instructions —
		// covering both is the safe default. Same model Terminal.app
		// uses for new windows.
		line := shellEscape(opts.Cmd)
		cmd = ptmx.Command(shell, "-l", "-i", "-c", line)
		log.Printf("session: spawn %s -l -i -c %q (cwd=%s)", shell, line, opts.Cwd)
	} else {
		cmd = ptmx.Command(shell)
		log.Printf("session: spawn %s (cwd=%s)", shell, opts.Cwd)
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if len(opts.Env) > 0 {
		cmd.Env = append(cmd.Env, opts.Env...)
	}
	dir, err := resolveCwd(opts.Cwd)
	if err != nil {
		_ = ptmx.Close()
		return nil, err
	}
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		_ = ptmx.Close()
		return nil, err
	}

	s := &Session{
		ID:    uuid.NewString(),
		vt:    NewVT(opts.Cols, opts.Rows),
		cmd:   cmd,
		ptmx:  ptmx,
		sinks: make(map[Sink]struct{}),
		done:  make(chan struct{}),
	}
	go s.readLoop()
	return s, nil
}

// readLoop drains the PTY into the VT mirror and every active sink. It
// is the only goroutine that reads from the PTY.
func (s *Session) readLoop() {
	defer close(s.done)
	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			s.deliver(buf[:n])
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

// deliver applies one chunk of PTY output to the VT mirror and fans it
// out to every active sink under a single critical section. Holding
// s.mu across both steps is what makes SubscribeAtomicSnapshot's
// "snapshot then live" guarantee actually atomic: a reattach either
// sees the snapshot before this chunk (and receives it via fanout) or
// after (and the chunk is in the snapshot but the new sink wasn't
// registered when fanout ran). Without the shared lock, a chunk could
// land in the snapshot AND be re-delivered to the new sink.
func (s *Session) deliver(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, vterr := s.vt.Write(p); vterr != nil {
		s.vtErrOnce.Do(func() {
			log.Printf("session %s: vt write: %v", s.ID, vterr)
		})
	}
	dead := make([]Sink, 0)
	for sink := range s.sinks {
		if _, err := sink.Write(p); err != nil {
			dead = append(dead, sink)
		}
	}
	for _, d := range dead {
		delete(s.sinks, d)
	}
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
// unsubscribe function. No replay is sent — callers that want atomic
// "repaint then live" behavior should use SubscribeAtomicSnapshot.
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

// SubscribeAtomicSnapshot returns a synthesized repaint of the current
// visible state (via the VT mirror) AND registers the sink for live
// updates under a single lock acquisition, so no PTY bytes are dropped
// between the snapshot and the live stream becoming active.
func (s *Session) SubscribeAtomicSnapshot(sink Sink) (snapshot []byte, unsubscribe func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot = s.vt.RenderSnapshot()
	s.sinks[sink] = struct{}{}
	return snapshot, func() {
		s.mu.Lock()
		delete(s.sinks, sink)
		s.mu.Unlock()
	}
}

// Write forwards bytes from a client to the PTY (i.e. keystrokes).
func (s *Session) Write(p []byte) (int, error) {
	return s.ptmx.Write(p)
}

// Resize updates the PTY's window size. cols × rows. Also resizes the
// VT mirror so the next reattach snapshot matches the new dimensions.
func (s *Session) Resize(cols, rows int) error {
	if err := s.ptmx.Resize(cols, rows); err != nil {
		return err
	}
	_ = s.vt.Resize(cols, rows)
	return nil
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

// shellEscape joins argv into a single line safe for "sh -c". Bare-word
// args pass through unquoted; anything with whitespace or shell
// metacharacters gets single-quoted with embedded single quotes
// escaped via the standard '\'' trick.
func shellEscape(argv []string) string {
	const safe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-./@:+=,%"
	out := make([]byte, 0, 32)
	for i, a := range argv {
		if i > 0 {
			out = append(out, ' ')
		}
		if a == "" {
			out = append(out, '\'', '\'')
			continue
		}
		ok := true
		for j := 0; j < len(a); j++ {
			if !contains(safe, a[j]) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, a...)
			continue
		}
		out = append(out, '\'')
		for j := 0; j < len(a); j++ {
			if a[j] == '\'' {
				out = append(out, '\'', '\\', '\'', '\'')
			} else {
				out = append(out, a[j])
			}
		}
		out = append(out, '\'')
	}
	return string(out)
}

func contains(s string, b byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return true
		}
	}
	return false
}

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
