// Package daemon is the hived process: a single-session PTY host that
// accepts client connections over a Unix socket and speaks the wire
// protocol from internal/wire.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"

	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// Config configures a Daemon.
type Config struct {
	SocketPath string         // empty → SocketPath()
	Session    session.Options // shell, initial size, env
}

// Daemon owns the listener and the single Phase-1 session.
type Daemon struct {
	cfg  Config
	sock string

	ln  net.Listener
	sess *session.Session

	mu      sync.Mutex
	clients map[net.Conn]struct{}
}

// New creates the daemon, binds the socket, and starts the session.
// The caller must call Run to begin accepting clients, and Close when
// done.
func New(cfg Config) (*Daemon, error) {
	sock := cfg.SocketPath
	if sock == "" {
		sock = SocketPath()
	}
	if err := EnsureSocketDir(sock); err != nil {
		return nil, fmt.Errorf("daemon: socket dir: %w", err)
	}
	// If a stale socket file exists, attempt a probe-and-replace: try
	// to dial it; if that succeeds, another daemon is alive — refuse to
	// start. If it fails, remove the stale file.
	if _, err := os.Stat(sock); err == nil {
		if c, derr := net.Dial("unix", sock); derr == nil {
			_ = c.Close()
			return nil, fmt.Errorf("daemon: another hived appears to be running at %s", sock)
		}
		_ = os.Remove(sock)
	}

	ln, err := net.Listen("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("daemon: listen %s: %w", sock, err)
	}

	sess, err := session.Start(cfg.Session)
	if err != nil {
		_ = ln.Close()
		_ = os.Remove(sock)
		return nil, fmt.Errorf("daemon: session start: %w", err)
	}

	return &Daemon{
		cfg:     cfg,
		sock:    sock,
		ln:      ln,
		sess:    sess,
		clients: make(map[net.Conn]struct{}),
	}, nil
}

// Run accepts clients until ctx is cancelled or the listener is closed.
func (d *Daemon) Run(ctx context.Context) error {
	log.Printf("hived: listening on %s, session %s", d.sock, d.sess.ID)
	go func() {
		<-ctx.Done()
		_ = d.ln.Close()
	}()
	for {
		conn, err := d.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			log.Printf("hived: accept: %v", err)
			continue
		}
		go d.serve(conn)
	}
}

// SocketPath returns the path the daemon is bound to.
func (d *Daemon) SocketPath() string { return d.sock }

// SessionID returns the ID of the active session.
func (d *Daemon) SessionID() string { return d.sess.ID }

// Close terminates the session, closes the listener, and removes the
// socket file.
func (d *Daemon) Close() error {
	d.mu.Lock()
	for c := range d.clients {
		_ = c.Close()
	}
	d.clients = nil
	d.mu.Unlock()
	if d.ln != nil {
		_ = d.ln.Close()
	}
	if d.sess != nil {
		_ = d.sess.Close()
	}
	_ = os.Remove(d.sock)
	return nil
}

// serve runs the per-client lifecycle: handshake, replay, fan-in/out.
func (d *Daemon) serve(conn net.Conn) {
	d.mu.Lock()
	if d.clients == nil {
		d.mu.Unlock()
		_ = conn.Close()
		return
	}
	d.clients[conn] = struct{}{}
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.clients, conn)
		d.mu.Unlock()
		_ = conn.Close()
	}()

	// 1. Read HELLO.
	var hello wire.Hello
	ft, err := wire.ReadJSON(conn, &hello)
	if err != nil {
		log.Printf("hived: read hello: %v", err)
		return
	}
	if ft != wire.FrameHello {
		log.Printf("hived: expected HELLO, got %s", ft)
		return
	}
	if hello.Version != wire.PROTOCOL_VERSION {
		// Soft mismatch: send error + close.
		_ = wire.WriteJSON(conn, wire.FrameError, wire.Error{
			Code:    "protocol_version_mismatch",
			Message: fmt.Sprintf("server speaks v%d; client speaks v%d", wire.PROTOCOL_VERSION, hello.Version),
		})
		return
	}

	// 2. Send WELCOME.
	cols, rows := d.cfg.Session.Cols, d.cfg.Session.Rows
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	if err := wire.WriteJSON(conn, wire.FrameWelcome, wire.Welcome{
		Version:   wire.PROTOCOL_VERSION,
		SessionID: d.sess.ID,
		Cols:      cols,
		Rows:      rows,
	}); err != nil {
		return
	}

	// 3. Atomic replay + subscribe so no live bytes are dropped.
	sink := &frameSink{conn: conn}
	replay, unsub := d.sess.SubscribeAtomic(sink)
	defer unsub()

	// Send the replay snapshot, chunked, then announce that replay is
	// done. The client uses this signal to switch from "painting
	// scrollback" to "live".
	if err := writeChunked(conn, wire.FrameData, replay, 16<<10); err != nil {
		return
	}
	if err := wire.WriteJSON(conn, wire.FrameEvent, wire.Event{
		Kind: wire.EventScrollbackReplayDone,
	}); err != nil {
		return
	}

	// 4. Read loop: client → PTY (and resize control frames).
	for {
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("hived: client read: %v", err)
			}
			return
		}
		switch ft {
		case wire.FrameData:
			if _, werr := d.sess.Write(payload); werr != nil {
				return
			}
		case wire.FrameResize:
			var rz wire.Resize
			if jerr := jsonUnmarshal(payload, &rz); jerr != nil {
				continue
			}
			_ = d.sess.Resize(rz.Cols, rz.Rows)
		default:
			log.Printf("hived: unexpected frame from client: %s", ft)
		}
	}
}

// frameSink wraps a net.Conn so it can be used as a session.Sink. Each
// PTY chunk becomes one DATA frame.
type frameSink struct {
	conn net.Conn
	mu   sync.Mutex
}

func (f *frameSink) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := wire.WriteFrame(f.conn, wire.FrameData, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (f *frameSink) Close() error {
	return f.conn.Close()
}

func writeChunked(w io.Writer, t wire.FrameType, p []byte, chunk int) error {
	for len(p) > 0 {
		n := chunk
		if n > len(p) {
			n = len(p)
		}
		if err := wire.WriteFrame(w, t, p[:n]); err != nil {
			return err
		}
		p = p[n:]
	}
	return nil
}
