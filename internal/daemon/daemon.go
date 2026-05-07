// Package daemon is the hived process: a multi-session PTY host that
// accepts client connections over a Unix socket and speaks the wire
// protocol from internal/wire.
//
// A connection chooses its mode in HELLO:
//   - control: session-management (LIST/CREATE/KILL/UPDATE), no DATA
//   - attach:  attach to an existing session by ID
//   - create:  create a new session, then attach to it
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

	"github.com/lucascaro/hive/internal/buildinfo"
	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// Config configures a Daemon.
type Config struct {
	SocketPath string // empty → SocketPath()
	StateDir   string // empty → registry.StateDir()
	// BootstrapSession, if non-zero, makes the daemon create a default
	// session at startup so a fresh GUI has something to attach to.
	BootstrapSession session.Options
}

// Daemon owns the listener and the registry.
type Daemon struct {
	cfg  Config
	sock string
	reg  *registry.Registry
	ln   net.Listener

	mu      sync.Mutex
	clients map[net.Conn]struct{}
}

// New binds the socket, opens the registry, and (if configured)
// creates the bootstrap session. Call Run to start accepting clients.
func New(cfg Config) (*Daemon, error) {
	sock := cfg.SocketPath
	if sock == "" {
		sock = SocketPath()
	}
	if err := EnsureSocketDir(sock); err != nil {
		return nil, fmt.Errorf("daemon: socket dir: %w", err)
	}
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

	reg, err := registry.Open(cfg.StateDir)
	if err != nil {
		_ = ln.Close()
		return nil, err
	}

	// Ensure a default project exists, then migrate any orphan
	// sessions to it. This is idempotent: existing installs (Phase
	// 1-3) get a "default" project created on the first Phase 4 boot
	// with their pre-existing sessions reassigned.
	if _, err := reg.EnsureDefaultProject(cfg.BootstrapSession.Cwd); err != nil {
		log.Printf("hived: ensure default project: %v", err)
	}
	reg.MigrateOrphanSessions()
	// Reclaim worktree directories whose owning session no longer
	// exists (e.g. previous daemon was SIGKILL'd mid-Kill).
	reg.ReclaimOrphanWorktrees()

	// Revive any persisted sessions that have no live PTY (i.e. every
	// entry loaded from disk on this run). Metadata is preserved; the
	// shell is fresh — Phase 1.7 will eventually replay scrollback here.
	for _, info := range reg.List() {
		if !info.Alive {
			if err := reg.Revive(info.ID, cfg.BootstrapSession); err != nil {
				log.Printf("hived: revive %s: %v", info.ID, err)
			}
		}
	}

	// Bootstrap session only if the registry is still empty after revive
	// (i.e. truly first run on this machine).
	if len(reg.List()) == 0 && bootstrapWanted(cfg.BootstrapSession) {
		_, err := reg.Create(wire.CreateSpec{
			Name:  "main",
			Cols:  cfg.BootstrapSession.Cols,
			Rows:  cfg.BootstrapSession.Rows,
			Shell: cfg.BootstrapSession.Shell,
		})
		if err != nil {
			log.Printf("hived: bootstrap session: %v", err)
		}
	}

	return &Daemon{
		cfg:     cfg,
		sock:    sock,
		reg:     reg,
		ln:      ln,
		clients: make(map[net.Conn]struct{}),
	}, nil
}

// Run accepts clients until ctx is cancelled or the listener is closed.
func (d *Daemon) Run(ctx context.Context) error {
	log.Printf("hived: listening on %s, %d session(s)", d.sock, len(d.reg.List()))
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

// Registry exposes the registry for tests; production code should
// not bypass the wire protocol.
func (d *Daemon) Registry() *registry.Registry { return d.reg }

// Close terminates every session, closes listeners, removes the socket.
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
	if d.reg != nil {
		_ = d.reg.Close()
	}
	_ = os.Remove(d.sock)
	return nil
}

// serve dispatches on the HELLO mode.
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
		_ = wire.WriteJSON(conn, wire.FrameError, wire.Error{
			Code:    "protocol_version_mismatch",
			Message: fmt.Sprintf("server speaks v%d; client speaks v%d", wire.PROTOCOL_VERSION, hello.Version),
		})
		return
	}

	switch hello.Mode {
	case wire.ModeControl:
		d.serveControl(conn)
	case wire.ModeAttach:
		d.serveAttach(conn, hello.SessionID)
	case wire.ModeCreate:
		spec := wire.CreateSpec{}
		if hello.Create != nil {
			spec = *hello.Create
		}
		e, err := d.reg.Create(spec)
		if err != nil {
			_ = wire.WriteJSON(conn, wire.FrameError, wire.Error{Code: "create_failed", Message: err.Error()})
			return
		}
		d.serveAttach(conn, e.ID)
	default:
		_ = wire.WriteJSON(conn, wire.FrameError, wire.Error{
			Code:    "unknown_mode",
			Message: fmt.Sprintf("mode %q; want control|attach|create", hello.Mode),
		})
	}
}

// serveControl handles a session-management connection.
func (d *Daemon) serveControl(conn net.Conn) {
	if err := wire.WriteJSON(conn, wire.FrameWelcome, wire.Welcome{
		Version: wire.PROTOCOL_VERSION,
		BuildID: buildinfo.BuildID(),
		Mode:    wire.ModeControl,
	}); err != nil {
		return
	}
	listener, unsub := d.reg.Subscribe()
	defer unsub()
	pListener, pUnsub := d.reg.SubscribeProjects()
	defer pUnsub()

	// Per-conn write mutex so the snapshot/event goroutines don't
	// interleave bytes with each other or with the response writes
	// from the request loop below.
	var connMu sync.Mutex
	writeJSON := func(t wire.FrameType, v any) error {
		connMu.Lock()
		defer connMu.Unlock()
		return wire.WriteJSON(conn, t, v)
	}

	stop := make(chan struct{})
	go func() {
		// Initial snapshot — projects first so the client can resolve
		// session.project_id without a roundtrip.
		_ = writeJSON(wire.FrameProjects, wire.ProjectsResp{Projects: d.reg.ListProjects()})
		_ = writeJSON(wire.FrameSessions, wire.SessionsResp{Sessions: d.reg.List()})
		for {
			select {
			case ev, ok := <-listener:
				if !ok {
					return
				}
				if err := writeJSON(wire.FrameSessionEvent, ev); err != nil {
					return
				}
			case ev, ok := <-pListener:
				if !ok {
					return
				}
				if err := writeJSON(wire.FrameProjectEvent, ev); err != nil {
					return
				}
			case <-stop:
				return
			}
		}
	}()
	defer close(stop)

	sendError := func(code, msg string) {
		_ = writeJSON(wire.FrameError, wire.Error{Code: code, Message: msg})
	}
	for {
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("hived: control read: %v", err)
			}
			return
		}
		switch ft {
		case wire.FrameListSessions:
			_ = writeJSON(wire.FrameSessions, wire.SessionsResp{Sessions: d.reg.List()})
		case wire.FrameCreateSession:
			var spec wire.CreateSpec
			if err := jsonUnmarshal(payload, &spec); err != nil {
				sendError("bad_payload", err.Error())
				continue
			}
			if _, err := d.reg.Create(spec); err != nil {
				sendError("create_failed", err.Error())
			}
		case wire.FrameKillSession:
			var req wire.KillSessionReq
			if err := jsonUnmarshal(payload, &req); err != nil {
				sendError("bad_payload", err.Error())
				continue
			}
			if err := d.reg.Kill(req.SessionID, req.Force); err != nil {
				if errors.Is(err, registry.ErrWorktreeDirty) {
					_ = writeJSON(wire.FrameError, wire.Error{
						Code:      wire.ErrCodeWorktreeDirty,
						Message:   "worktree has uncommitted changes",
						SessionID: req.SessionID,
					})
				} else {
					sendError("kill_failed", err.Error())
				}
			}
		case wire.FrameRestartSession:
			var req wire.RestartSessionReq
			if err := jsonUnmarshal(payload, &req); err != nil {
				sendError("bad_payload", err.Error())
				continue
			}
			if err := d.reg.Restart(req.SessionID); err != nil {
				sendError("restart_failed", err.Error())
			}
		case wire.FrameUpdateSession:
			var req wire.UpdateSessionReq
			if err := jsonUnmarshal(payload, &req); err != nil {
				sendError("bad_payload", err.Error())
				continue
			}
			if _, err := d.reg.Update(req); err != nil {
				sendError("update_failed", err.Error())
			}
		case wire.FrameListProjects:
			_ = writeJSON(wire.FrameProjects, wire.ProjectsResp{Projects: d.reg.ListProjects()})
		case wire.FrameCreateProject:
			var req wire.CreateProjectReq
			if err := jsonUnmarshal(payload, &req); err != nil {
				sendError("bad_payload", err.Error())
				continue
			}
			if _, err := d.reg.CreateProject(req); err != nil {
				sendError("create_project_failed", err.Error())
			}
		case wire.FrameKillProject:
			var req wire.KillProjectReq
			if err := jsonUnmarshal(payload, &req); err != nil {
				sendError("bad_payload", err.Error())
				continue
			}
			if err := d.reg.KillProject(req.ProjectID, req.KillSessions); err != nil {
				sendError("kill_project_failed", err.Error())
			}
		case wire.FrameUpdateProject:
			var req wire.UpdateProjectReq
			if err := jsonUnmarshal(payload, &req); err != nil {
				sendError("bad_payload", err.Error())
				continue
			}
			if _, err := d.reg.UpdateProject(req); err != nil {
				sendError("update_project_failed", err.Error())
			}
		default:
			log.Printf("hived: unexpected control frame: %s", ft)
		}
	}
}

// serveAttach handles a session-attached connection.
func (d *Daemon) serveAttach(conn net.Conn, sessionID string) {
	entry := d.reg.Get(sessionID)
	if entry == nil {
		_ = wire.WriteJSON(conn, wire.FrameError, wire.Error{
			Code:    "no_such_session",
			Message: sessionID,
		})
		return
	}
	if entry.Session() == nil {
		_ = wire.WriteJSON(conn, wire.FrameError, wire.Error{
			Code:    "session_dead",
			Message: "session has no live PTY (daemon-restart resume not implemented yet)",
		})
		return
	}
	sess := entry.Session()

	// Resolve current PTY size for WELCOME. session has no getter; we
	// reuse cfg defaults if the bootstrap matches, else 80x24 as a
	// reasonable default — the client usually issues a Resize next.
	cols := d.cfg.BootstrapSession.Cols
	if cols == 0 {
		cols = 80
	}
	rows := d.cfg.BootstrapSession.Rows
	if rows == 0 {
		rows = 24
	}
	if err := wire.WriteJSON(conn, wire.FrameWelcome, wire.Welcome{
		Version:   wire.PROTOCOL_VERSION,
		BuildID:   buildinfo.BuildID(),
		Mode:      wire.ModeAttach,
		SessionID: entry.ID,
		Cols:      cols,
		Rows:      rows,
	}); err != nil {
		return
	}

	sink := &frameSink{conn: conn}
	snapshot, unsub := sess.SubscribeAtomicSnapshot(sink)
	defer unsub()

	if err := writeChunked(conn, wire.FrameData, snapshot, 16<<10); err != nil {
		return
	}
	if err := wire.WriteJSON(conn, wire.FrameEvent, wire.Event{
		Kind: wire.EventScrollbackReplayDone,
	}); err != nil {
		return
	}

	for {
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("hived: attach read: %v", err)
			}
			return
		}
		switch ft {
		case wire.FrameData:
			if _, werr := sess.Write(payload); werr != nil {
				return
			}
		case wire.FrameResize:
			var rz wire.Resize
			if err := jsonUnmarshal(payload, &rz); err != nil {
				continue
			}
			_ = sess.Resize(rz.Cols, rz.Rows)
		default:
			log.Printf("hived: unexpected attach frame: %s", ft)
		}
	}
}

// frameSink wraps a net.Conn so it can be a session.Sink.
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

func (f *frameSink) Close() error { return f.conn.Close() }

// bootstrapWanted reports whether opts has any non-default field set.
// Can't use struct equality because session.Options has a slice field.
func bootstrapWanted(opts session.Options) bool {
	return opts.Shell != "" || opts.Cols != 0 || opts.Rows != 0 || len(opts.Env) > 0
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
