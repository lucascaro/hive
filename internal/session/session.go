// Package session models a single hived session: one PTY and the
// shell running on it. Reattach repaints come from the VT mirror.
package session

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

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

	// Window title (OSC 0/2) plumbing, all guarded by mu.
	//
	// title is the last value handed to titleHook, NOT simply the last
	// value seen: it is what makes a program that re-sets the identical
	// string every frame (common in TUIs) cost nothing downstream.
	//
	// titleHook is installed by the registry to broadcast the change to
	// clients. It is invoked from readLoop with mu released — see the
	// comment on noteTitle for why that ordering is load-bearing.
	title      string
	titleHook  func(string)
	titleTimer *time.Timer

	// Terminal-bell plumbing. bells scans the PTY stream for real BEL
	// bytes (see bell.go); bellHook is installed by the registry to
	// mark the session as wanting attention. Like titleHook it is
	// invoked from readLoop with mu released, keeping the
	// registry→session lock order one-way.
	//
	// There is no throttle here, unlike the title: a program that rings
	// twice is still just "wants attention", and the registry drops the
	// hook call when the flag is already set — so a bell storm costs a
	// bool compare, not a frame per bell.
	bells    bellScanner
	bellHook func()
}

// titleThrottle bounds how often a session reports a title change. Some
// TUIs animate their title (a spinner glyph in the window title), and
// every report costs a socket frame plus a Wails IPC hop plus a JSON
// parse for each connected client. The throttle is trailing, so the
// final title of a burst always lands — it is a coalesce, not a drop.
// A var, not a const, purely so tests can shrink it — production never
// assigns it.
var titleThrottle = 500 * time.Millisecond

// Options configures a new Session.
type Options struct {
	Shell      string
	Cmd        []string // when non-empty, runs in place of $SHELL (e.g. an agent)
	Cwd        string   // working directory; default = sane choice
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
		if runtime.GOOS == "windows" {
			// cmd.exe doesn't understand `-l -i -c`. Windows installs
			// `claude` (and most npm-shipped CLIs) as a `.cmd` shim that
			// CreateProcessW can't exec directly — so wrapping via
			// `cmd.exe /S /C "<line>"` is both correct and required.
			// PATH inherits from the parent process; there's no Windows
			// analogue to login+interactive rc sourcing to preserve.
			//
			// Implementation note: Go's Windows exec layer re-quotes
			// every argv element via `windows.ComposeCommandLine`,
			// which would mangle the precisely-quoted line produced by
			// `cmdExeEscape`. We bypass that by setting
			// `SysProcAttr.CmdLine` directly — see
			// `newWindowsCmd` in spawn_windows.go.
			wrapper := os.Getenv("ComSpec")
			if wrapper == "" {
				wrapper = "cmd.exe"
			}
			line := cmdExeEscape(opts.Cmd)
			cmd = newWindowsCmd(ptmx, wrapper, line)
			log.Printf("session: spawn %s /S /C %q (cwd=%s)", wrapper, `"`+line+`"`, opts.Cwd)
		} else {
			// Run the command via the user's login + interactive shell so
			// PATH/aliases/functions set up in *either* .zprofile (login)
			// or .zshrc (interactive) apply. fnm, nvm, asdf, etc. land in
			// different rc files depending on the install instructions —
			// covering both is the safe default. Same model Terminal.app
			// uses for new windows.
			line := shellEscape(opts.Cmd)
			cmd = ptmx.Command(shell, "-l", "-i", "-c", line)
			log.Printf("session: spawn %s -l -i -c %q (cwd=%s)", shell, line, opts.Cwd)
		}
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
			// After deliver, not inside it: deliver holds s.mu across the
			// VT write and the sink fanout, and noteTitle's hook reaches
			// into the registry, which takes r.mu. registry imports
			// session and never the reverse, so firing here (mu already
			// released) keeps that a one-way edge instead of a lock cycle.
			s.noteTitle()
			s.noteBell(buf[:n])
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
// s.mu across both steps is what makes SubscribeWithAtomicReplay's
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

// SetTitleHook installs the callback invoked when the program running on
// this session changes its window title. Passing nil disables reporting.
// The hook runs on the readLoop goroutine with no session lock held, so
// it may take locks of its own; it must not block for long, since the
// PTY drain is stalled while it runs.
func (s *Session) SetTitleHook(fn func(string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.titleHook = fn
}

// Title returns the window title the running program most recently set,
// or "" if it never set one.
func (s *Session) Title() string {
	return s.vt.Title()
}

// SetBellHook installs the callback invoked when the program running on
// this session rings the terminal bell. Passing nil disables reporting.
// Same contract as SetTitleHook: it runs on the readLoop goroutine with
// no session lock held, and must not block for long — the PTY drain is
// stalled while it runs.
func (s *Session) SetBellHook(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bellHook = fn
}

// noteBell scans one delivered chunk for a real bell and reports it.
// Called from readLoop after deliver, for the same lock-ordering reason
// as noteTitle.
//
// The scanner is stateful across chunks, so it must see every chunk in
// order — including when no hook is installed. Skipping the scan on a
// nil hook would leave the state machine mid-OSC and make the next
// title's terminator read as a bell.
func (s *Session) noteBell(p []byte) {
	s.mu.Lock()
	rang := s.bells.Scan(p)
	hook := s.bellHook
	s.mu.Unlock()
	if rang && hook != nil {
		hook()
	}
}

// noteTitle reports a changed window title to the hook, at most once per
// titleThrottle. Called from readLoop after every delivered chunk.
//
// Unchanged titles return before touching the timer, so the steady state
// (a program that never sets a title, or re-sets the same one) is a
// string compare and nothing else. A change inside the throttle window
// arms a trailing timer rather than dropping the update, so the last
// title of a burst is always the one clients end up with.
func (s *Session) noteTitle() {
	s.mu.Lock()
	cur := s.vt.Title()
	if cur == s.title || s.titleHook == nil {
		s.mu.Unlock()
		return
	}
	if s.titleTimer != nil {
		// A trailing fire is already pending; it will read the latest
		// title when it runs, so there is nothing more to do here.
		s.mu.Unlock()
		return
	}
	s.title = cur
	hook := s.titleHook
	s.titleTimer = time.AfterFunc(titleThrottle, s.flushTitle)
	s.mu.Unlock()
	hook(cur)
}

// flushTitle runs when a throttle window closes. It re-reads the title
// and reports it if the program changed it again while the window was
// open, so no update is ever merely dropped.
func (s *Session) flushTitle() {
	s.mu.Lock()
	s.titleTimer = nil
	cur := s.vt.Title()
	if cur == s.title || s.titleHook == nil {
		s.mu.Unlock()
		return
	}
	s.title = cur
	hook := s.titleHook
	// Re-arm: the burst is evidently still going, so keep coalescing
	// rather than letting the next chunk through unthrottled.
	s.titleTimer = time.AfterFunc(titleThrottle, s.flushTitle)
	s.mu.Unlock()
	hook(cur)
}

func (s *Session) fanoutClose() {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The PTY is done, so no further title can arrive; drop any pending
	// trailing fire rather than leaving a timer holding this session
	// alive for another throttle window.
	if s.titleTimer != nil {
		s.titleTimer.Stop()
		s.titleTimer = nil
	}
	for sink := range s.sinks {
		if c, ok := sink.(io.Closer); ok {
			_ = c.Close()
		}
		delete(s.sinks, sink)
	}
}

// SubscribeWithAtomicReplay runs writeFn with the current ring
// snapshot AND registers sink for future fanout, all under s.mu, so
// no live PTY byte can land on writeFn's transport between the
// snapshot capture and the writeFn return. After writeFn returns
// (without error), the sink is registered and starts receiving live
// fanout from the next deliver onward.
//
// writeFn is expected to write a Begin event, the replay bytes, and a
// Done event to its underlying transport, ideally under the sink's
// own write mutex so any concurrent non-fanout writer is also
// serialized. While writeFn runs, deliver() is blocked on s.mu — the
// PTY reader continues filling the kernel buffer, but no other write
// reaches the wire. Keep writeFn quick: long replays mean longer
// session pauses (PTY backpressure picks up at the kernel buffer
// level, ~64 KiB on macOS, before the agent stalls).
//
// On writeFn error the sink is not registered and unsubscribe is nil.
func (s *Session) SubscribeWithAtomicReplay(sink Sink, writeFn func(replay []byte) error) (unsubscribe func(), err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Initial attach uses InitialReplayBytes, not the raw ring: both screen
	// types get a compact snapshot (alt-screen: one screen, no scrollback;
	// normal: one screen + historyRows of history, sized to match xterm's
	// own scrollback cap), avoiding the many-tile startup flood. Only the
	// resize-driven re-replay (EmitAtomicReplay) still streams the raw ring,
	// and only for reflow recovery.
	replay, snapshot := s.vt.InitialReplayBytes()
	// Logged so hived.log proves which path ran per attach — a snapshot
	// line here (vs a multi-MB ring) is the unconfounded signal that the
	// fixed daemon build is live, independent of the days-long freeze.
	log.Printf("session %s: initial replay %d bytes snapshot=%v", s.ID, len(replay), snapshot)
	if err := writeFn(replay); err != nil {
		return nil, err
	}
	s.sinks[sink] = struct{}{}
	return func() {
		s.mu.Lock()
		delete(s.sinks, sink)
		s.mu.Unlock()
	}, nil
}

// EmitAtomicReplay runs writeFn with the current ring snapshot under
// s.mu, so no deliver runs while writeFn writes. Used by clients
// asking for a re-replay mid-attach (FrameRequestReplay) where the
// sink is already registered — we still need the snapshot to be
// captured atomically with the wire write to prevent live bytes from
// being interleaved between Begin and Done.
func (s *Session) EmitAtomicReplay(writeFn func(replay []byte) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeFn(s.vt.RingBytes())
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
// escaped via the standard '\” trick.
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
			if strings.IndexByte(safe, a[j]) < 0 {
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

// cmdExeEscape joins argv into a single command line for `cmd.exe /C`.
// Each element is wrapped in double quotes; embedded `"` is escaped by
// preceding backslashes per the standard CommandLineToArgvW rules.
// cmd.exe metacharacters (`& | < > ^`) inside quotes are passed through
// literally — cmd.exe only treats them as special outside quotes, so
// quoting the whole arg neutralizes them.
//
// Caveat: `%` is NOT neutralized by quoting — cmd.exe performs `%VAR%`
// environment-variable expansion even inside double quotes. Callers
// must not pass user-controlled `%` characters expecting them to
// survive verbatim. For agent argv (`claude`, flag-value pairs) this
// is fine because `%` is not used; if a future caller needs `%`,
// disable expansion by invoking cmd.exe with `/V:OFF` and `/D`, or
// pre-escape outside this helper.
func cmdExeEscape(argv []string) string {
	out := make([]byte, 0, 32)
	for i, a := range argv {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, '"')
		// CommandLineToArgvW rules: a run of N backslashes followed by
		// `"` becomes 2N backslashes plus an escaped quote; a run
		// followed by end-of-arg becomes 2N backslashes (so the closing
		// `"` isn't escaped); otherwise backslashes pass through.
		bs := 0
		for j := 0; j < len(a); j++ {
			c := a[j]
			switch c {
			case '\\':
				bs++
			case '"':
				for k := 0; k < bs*2+1; k++ {
					out = append(out, '\\')
				}
				out = append(out, '"')
				bs = 0
			default:
				for k := 0; k < bs; k++ {
					out = append(out, '\\')
				}
				bs = 0
				out = append(out, c)
			}
		}
		for k := 0; k < bs*2; k++ {
			out = append(out, '\\')
		}
		out = append(out, '"')
	}
	return string(out)
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

// ScreenDigest hashes what the session's screen currently looks like.
// See VT.ScreenDigest: it is the signal behind "working" vs "idle",
// because bytes arriving and work happening are different things.
func (s *Session) ScreenDigest() uint64 { return s.vt.ScreenDigest() }
