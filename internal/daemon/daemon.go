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

	// commands relays client-to-client verbs (see commands.go). Not
	// state, so it is not in the registry.
	commands *commandHub

	// shutdown is closed by Shutdown to stop Run the same way a
	// cancelled context does. A client asking to exit in-band (the
	// GUI's Restart action) has no handle on the daemon's context,
	// and signalling by pid is exactly the fragile path this exists
	// to avoid.
	shutdown     chan struct{}
	shutdownOnce sync.Once

	// stop is closed by Shutdown and by Close to tell the background
	// boot chores (reviveAll, reclaimWorktrees) to give up. They are
	// owned by d.ops, but a WaitGroup only joins — without a signal a
	// SIGTERM two seconds into boot would sit in Close waiting for
	// eight more login shells to fork, only to kill them all
	// immediately after (golden principle 5: the owner signals, it
	// does not merely wait).
	stop     chan struct{}
	stopOnce sync.Once

	// sockInfo identifies the socket file this daemon bound, so Close
	// only unlinks its own. Teardown now spans the boot chores, which
	// is long enough for the GUI's Restart to have relaunched and a
	// NEW hived to have bound a fresh socket at the same path — and
	// an unconditional os.Remove here would delete that one. See
	// docs/exec-plans/completed/243-restart-hive-doesnt-reliably-restart-daemon.md,
	// which judged this window unreachable back when teardown was
	// ~100ms.
	sockInfo os.FileInfo

	// orphanCandidates is the worktree-directory snapshot taken on the
	// boot path, before any client could connect. The background sweep
	// consumes it; see New.
	orphanCandidates []registry.OrphanWorktreeCandidate

	// ops tracks the goroutines that run session lifecycle work
	// (create/kill/restart) off the control read loop. Close waits on
	// it so a shutting-down daemon doesn't abandon a `git worktree
	// add` halfway. Deliberately NOT waited in Shutdown: Shutdown is
	// called from the control read loop itself (FrameShutdown) and is
	// sync.Once-guarded, so waiting there would both block a GUI
	// restart behind a slow git and fail to be a barrier for the
	// second caller.
	ops sync.WaitGroup
}

// runOp runs one session lifecycle operation off the control read
// loop. Create/Kill/Restart shell out to git, which used to block
// every other client request for the duration (golden principle 5:
// the goroutine has an explicit owner — d.ops, drained by Close).
func (d *Daemon) runOp(fn func()) {
	d.ops.Add(1)
	go func() {
		defer d.ops.Done()
		fn()
	}()
}

// stopOps closes d.stop once. Safe from any goroutine and callable
// more than once: both Shutdown (in-band client request) and Close
// (process teardown) signal here, and either may arrive first.
func (d *Daemon) stopOps() {
	d.stopOnce.Do(func() { close(d.stop) })
}

// stopping reports whether the daemon has begun shutting down. The
// boot chores poll it between units of work.
func (d *Daemon) stopping() bool {
	select {
	case <-d.stop:
		return true
	default:
		return false
	}
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
	reg, err := registry.Open(cfg.StateDir)
	if err != nil {
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

	// Every entry loaded from disk has no PTY yet — reviveAll forks
	// them below, in the background, one at a time. Mark them spawning
	// BEFORE the bind so no client can ever observe the alive:false +
	// ready combination it is entitled to read as "this session died".
	reg.MarkPendingRevive()

	// Orphan-worktree reclaim and session revive deliberately do NOT
	// run here: both are slow (a handful of git subprocesses per
	// worktree, one PTY fork per session) and neither is needed
	// before the daemon can answer a LIST. They run as background
	// ops instead — see the tail of this function.

	// Bootstrap session only if the registry is empty (i.e. truly
	// first run on this machine).
	if len(reg.List()) == 0 && bootstrapWanted(cfg.BootstrapSession) {
		// Pre-Run: no daemon context exists yet, so this one-shot
		// bootstrap create is rooted at Background.
		_, err := reg.Create(context.Background(), wire.CreateSpec{
			Name:  "main",
			Cols:  cfg.BootstrapSession.Cols,
			Rows:  cfg.BootstrapSession.Rows,
			Shell: cfg.BootstrapSession.Shell,
		})
		if err != nil {
			log.Printf("hived: bootstrap session: %v", err)
		}
	}

	// Bind LAST. A bound socket is the readiness signal every client
	// relies on — dialOrSpawn's retry loop, the GUI's restart probe,
	// the e2e waitForSocket — and all of them only stat/dial it. With
	// the bind first, a client dialing during a slow boot landed in
	// the kernel backlog with nobody accepting, and its HELLO timed
	// out against wire.handshakeTimeout while the daemon was still
	// starting up. Nothing above needs the listener.
	ln, err := net.Listen("unix", sock)
	if err != nil {
		_ = reg.Close()
		return nil, fmt.Errorf("daemon: listen %s: %w", sock, err)
	}

	// Collect the orphan-worktree candidates HERE, on the boot path,
	// while no client can have created anything: the sweep itself is
	// slow (git status per candidate) and runs in the background, but
	// it must only ever be able to delete a directory that predates
	// this daemon. A worktree that appears afterwards is live work.
	//
	// Only the canonical daemon owns the on-disk <project>/.worktrees/
	// namespace; an isolated dev daemon (HIVE_STATE_DIR set) shares
	// that directory with prod and would otherwise reap prod's
	// worktrees as orphans.
	var orphanCandidates []registry.OrphanWorktreeCandidate
	if registry.StateDirOverridden() {
		log.Printf("hived: HIVE_STATE_DIR set; skipping orphan-worktree reclaim to protect foreign worktrees")
	} else {
		orphanCandidates = reg.ScanOrphanWorktrees()
	}

	// Identify the file we just bound, for Close's guarded unlink.
	// A stat failure is not fatal — Close falls back to leaving the
	// path alone, which is the safe direction.
	sockInfo, serr := os.Stat(sock)
	if serr != nil {
		log.Printf("hived: stat own socket %s: %v", sock, serr)
	}

	d := &Daemon{
		cfg:      cfg,
		sock:     sock,
		reg:      reg,
		ln:       ln,
		sockInfo: sockInfo,

		orphanCandidates: orphanCandidates,
		clients:          make(map[net.Conn]struct{}),
		commands:         newCommandHub(),
		shutdown:         make(chan struct{}),
		stop:             make(chan struct{}),
	}

	// The two slow boot chores run in the background, off the caller's
	// path: reviving persisted sessions forks one PTY each, and the
	// orphan-worktree reclaim shells out to git. Started here rather
	// than in Run so they cannot be skipped by a Close that beats Run
	// to the scheduler; d.ops is what Close waits on, d.stop is what
	// cuts them short.
	d.runOp(d.reviveAll)
	d.runOp(d.reclaimWorktrees)
	return d, nil
}

// Run accepts clients until ctx is cancelled or the listener is closed.
func (d *Daemon) Run(ctx context.Context) error {
	log.Printf("hived: listening on %s, %d session(s)", d.sock, len(d.reg.List()))
	go func() {
		select {
		case <-ctx.Done():
		case <-d.shutdown:
		}
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
		go d.serve(ctx, conn)
	}
}

// reviveAll starts a fresh PTY for every persisted session that has
// none — i.e. every entry loaded from disk on this run. Metadata is
// preserved; the shell is fresh.
//
// Sequential on purpose: a user with ten sessions would otherwise fork
// ten login shells at once on a machine that is already busy booting.
// Each session is bracketed in wire.PhaseSpawning, so a client
// attaching before its turn gets the "still starting" answer that
// serveAttach already speaks (and the GUI's phase spinner) instead of
// "session_dead".
func (d *Daemon) reviveAll() {
	// Two passes: ReviveWithPhase declines an entry whose lifecycle
	// something else owns right now (a client create/kill/restart
	// landing mid-boot), and a session skipped on the only pass would
	// stay dead until the next daemon start. Bounded at two — a
	// second decline means the other owner is still working, and its
	// own path will leave the session in a sane state.
	skipped := d.revivePass(nil)
	if len(skipped) > 0 && !d.stopping() {
		log.Printf("hived: retrying revive for %d session(s) busy on the first pass", len(skipped))
		if still := d.revivePass(skipped); len(still) > 0 {
			log.Printf("hived: %d session(s) left unrevived; another lifecycle op owns them", len(still))
		}
	}
}

// revivePass revives every non-alive session, or only the given ids
// when non-nil. Returns the ids it declined to touch because another
// lifecycle op owned them.
func (d *Daemon) revivePass(only []string) []string {
	var skipped []string
	for _, info := range d.reg.List() {
		if d.stopping() {
			return nil
		}
		if info.Alive || !wanted(only, info.ID) {
			continue
		}
		revived, err := d.reg.ReviveWithPhase(info.ID, d.cfg.BootstrapSession)
		switch {
		case err != nil:
			log.Printf("hived: revive %s: %v", info.ID, err)
		case !revived:
			log.Printf("hived: revive %s deferred: another lifecycle op owns it", info.ID)
			skipped = append(skipped, info.ID)
		}
	}
	return skipped
}

// wanted reports whether id is in only, treating a nil only as "all".
func wanted(only []string, id string) bool {
	if only == nil {
		return true
	}
	for _, want := range only {
		if want == id {
			return true
		}
	}
	return false
}

// reclaimWorktrees removes worktree directories whose owning session
// no longer exists (e.g. the previous daemon was SIGKILL'd mid-Kill).
// Only the canonical daemon owns the on-disk <project>/.worktrees/
// namespace; an isolated dev daemon (HIVE_STATE_DIR set) shares that
// directory with prod and would otherwise reap prod's worktrees as
// orphans.
func (d *Daemon) reclaimWorktrees() {
	if len(d.orphanCandidates) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-d.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	d.reg.ReclaimOrphanWorktrees(ctx, d.orphanCandidates)
}

// Shutdown asks Run to stop accepting and return, exactly as a
// cancelled context would. Safe to call more than once and from any
// goroutine — a client can send FrameShutdown twice, and the pidfile
// removal + registry flush that follow Run must happen once.
func (d *Daemon) Shutdown() {
	d.stopOps()
	d.shutdownOnce.Do(func() {
		log.Printf("hived: shutdown requested by client")
		close(d.shutdown)
	})
}

// SocketPath returns the path the daemon is bound to.
func (d *Daemon) SocketPath() string { return d.sock }

// Registry exposes the registry for tests; production code should
// not bypass the wire protocol.
func (d *Daemon) Registry() *registry.Registry { return d.reg }

// Close terminates every session, closes listeners, removes the socket.
func (d *Daemon) Close() error {
	// Signal first, then wait: in-flight create/kill work still gets
	// to finish (a half-created worktree with no session entry is an
	// orphan the next boot has to reclaim), but the boot chores stop
	// starting new work instead of forking shells for a daemon that
	// is going away.
	d.stopOps()
	d.ops.Wait()
	d.mu.Lock()
	for c := range d.clients {
		_ = c.Close()
	}
	d.clients = nil
	d.mu.Unlock()
	if d.commands != nil {
		d.commands.Close()
	}
	if d.ln != nil {
		_ = d.ln.Close()
	}
	if d.reg != nil {
		_ = d.reg.Close()
	}
	d.removeOwnSocket()
	return nil
}

// removeOwnSocket unlinks the socket only while it is still the file
// this daemon bound. Teardown can now span the boot chores, which is
// long enough for the GUI's Restart action to have relaunched and a
// replacement hived to have bound a fresh socket at the same path —
// an unconditional unlink here would delete the live daemon's socket
// and leave it serving an inode nobody can dial. Same shape as
// removePidfile in cmd/hived.
func (d *Daemon) removeOwnSocket() {
	if d.sockInfo == nil {
		return
	}
	cur, err := os.Stat(d.sock)
	if err != nil {
		return
	}
	if !os.SameFile(cur, d.sockInfo) {
		log.Printf("hived: socket %s now belongs to another daemon; leaving it alone", d.sock)
		return
	}
	if err := os.Remove(d.sock); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("hived: remove socket %s: %v", d.sock, err)
	}
}

// serve dispatches on the HELLO mode. ctx is the daemon's Run context
// (not a per-connection one): registry work it reaches — the post-spawn
// agent-session-id capture in particular — outlives this connection.
func (d *Daemon) serve(ctx context.Context, conn net.Conn) {
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
		// A connect-then-hang-up with no HELLO is a liveness probe,
		// not an error — the GUI's restart path dials this socket
		// repeatedly to find out whether the daemon is still up.
		// Logging those buries real handshake failures in noise.
		if !errors.Is(err, io.EOF) {
			log.Printf("hived: read hello: %v", err)
		}
		return
	}
	if ft != wire.FrameHello {
		log.Printf("hived: expected HELLO, got %s", ft)
		return
	}
	if hello.Version != wire.PROTOCOL_VERSION {
		_ = wire.WriteJSON(conn, wire.FrameError, wire.Error{
			Code:    wire.ErrCodeProtocolVersionMismatch,
			Message: fmt.Sprintf("server speaks v%d; client speaks v%d", wire.PROTOCOL_VERSION, hello.Version),
		})
		return
	}

	switch hello.Mode {
	case wire.ModeControl:
		d.serveControl(ctx, conn)
	case wire.ModeAttach:
		d.serveAttach(conn, hello.SessionID)
	case wire.ModeCreate:
		spec := wire.CreateSpec{}
		if hello.Create != nil {
			spec = *hello.Create
		}
		e, err := d.reg.Create(ctx, spec)
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
func (d *Daemon) serveControl(ctx context.Context, conn net.Conn) {
	if err := wire.WriteJSON(conn, wire.FrameWelcome, wire.Welcome{
		Version:        wire.PROTOCOL_VERSION,
		BuildID:        buildinfo.BuildID(),
		Release:        buildinfo.Version(),
		DaemonContract: buildinfo.DaemonContract,
		Mode:           wire.ModeControl,
	}); err != nil {
		return
	}
	listener, unsub := d.reg.Subscribe()
	defer unsub()
	pListener, pUnsub := d.reg.SubscribeProjects()
	defer pUnsub()
	cmdListener, cmdUnsub := d.commands.Subscribe()
	defer cmdUnsub()

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
			case cmd, ok := <-cmdListener:
				if !ok {
					return
				}
				if err := writeJSON(wire.FrameClientBroadcast, cmd); err != nil {
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
	// sendWorktrees answers with the project's current inventory. Every
	// worktree mutation ends here — including the ones that failed, and
	// the ones that half succeeded (the local branch went, the remote
	// push did not). A refusal alone would leave the browser rendering
	// rows the mutation already invalidated, so the error goes first
	// and the truth follows it.
	// An empty failCode means "stay quiet if the listing fails" — used
	// after a refusal, where the client already has one error and a
	// second would just be a second toast for the same action.
	sendWorktrees := func(projectID, failCode string) {
		resp, err := d.reg.ListWorktrees(projectID)
		if err != nil {
			if failCode != "" {
				sendError(failCode, err.Error())
			}
			return
		}
		_ = writeJSON(wire.FrameWorktrees, resp)
	}
	// sendWorktreeError maps the registry's refusal sentinels onto
	// wire codes the GUI knows how to confirm against, the same way
	// KILL_SESSION maps ErrWorktreeDirty. Anything unrecognised falls
	// through to the generic code.
	sendWorktreeError := func(err error, genericCode string) {
		switch {
		case errors.Is(err, registry.ErrWorktreeInUse):
			sendError(wire.ErrCodeWorktreeInUse, err.Error())
		case errors.Is(err, registry.ErrWorktreeDirty):
			sendError(wire.ErrCodeWorktreeDirty, err.Error())
		case errors.Is(err, registry.ErrWorktreeUnpushed):
			sendError(wire.ErrCodeWorktreeUnpushed, err.Error())
		case errors.Is(err, registry.ErrBranchUnmerged):
			sendError(wire.ErrCodeBranchUnmerged, err.Error())
		case errors.Is(err, registry.ErrBranchHasWorktree):
			sendError(wire.ErrCodeWorktreeInUse, err.Error())
		default:
			sendError(genericCode, err.Error())
		}
	}
	// finishMutation is how every worktree mutation ends: the refusal,
	// if any, and then the inventory it left behind. The two go
	// together because a mutation can half succeed — the local branch
	// deleted, the remote push refused — and an error alone would leave
	// the browser rendering a row that is already gone.
	finishMutation := func(projectID string, err error, failCode string) {
		if err != nil {
			sendWorktreeError(err, failCode)
			sendWorktrees(projectID, "")
			return
		}
		sendWorktrees(projectID, "list_worktrees_failed")
	}
	ops := controlOps{
		writeJSON:      writeJSON,
		sendError:      sendError,
		sendWorktrees:  sendWorktrees,
		finishMutation: finishMutation,
	}
	for {
		ft, payload, err := wire.ReadFrame(conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("hived: control read: %v", err)
			}
			return
		}
		if d.handleControlFrame(ctx, ops, ft, payload) {
			return
		}
	}
}

// controlOps bundles the per-connection reply helpers serveControl
// builds over the socket's write mutex. Passing them as one value is
// what lets the frame handlers live outside serveControl, which was
// 274 lines of closure soup with a 14-case switch buried at the
// bottom — untestable without a live socket, and the file's longest
// function by a factor of two.
type controlOps struct {
	writeJSON      func(wire.FrameType, any) error
	sendError      func(code, msg string)
	sendWorktrees  func(projectID, failCode string)
	finishMutation func(projectID string, err error, failCode string)
}

// decodeReq parses one request payload, answering `bad_payload` on the
// connection and reporting false when it cannot. Twelve of the
// fourteen control frames opened with this exact four-line prologue.
func decodeReq[T any](payload []byte, sendError func(code, msg string)) (T, bool) {
	var v T
	if err := jsonUnmarshal(payload, &v); err != nil {
		sendError("bad_payload", err.Error())
		return v, false
	}
	return v, true
}

// handleControlFrame dispatches one control frame. It reports true
// when the connection is finished (the client asked the daemon to shut
// down), false to keep reading.
func (d *Daemon) handleControlFrame(ctx context.Context, ops controlOps, ft wire.FrameType, payload []byte) bool {
	switch ft {
	case wire.FrameShutdown:
		// Return afterwards: the listener is about to close and
		// this conn is going away with it. Nothing is written
		// back — the client's proof of shutdown is the socket
		// going quiet, not an ack it would have to trust.
		d.Shutdown()
		return true
	case wire.FrameListSessions:
		_ = ops.writeJSON(wire.FrameSessions, wire.SessionsResp{Sessions: d.reg.List()})
	case wire.FrameCreateSession:
		spec, ok := decodeReq[wire.CreateSpec](payload, ops.sendError)
		if !ok {
			return false
		}
		d.runOp(func() {
			// ErrNotFound here means the user killed the session
			// while it was still being created — an ordinary
			// outcome, not something to flash at them.
			if _, err := d.reg.Create(ctx, spec); err != nil &&
				!errors.Is(err, registry.ErrNotFound) {
				ops.sendError("create_failed", err.Error())
			}
		})
	case wire.FrameKillSession:
		req, ok := decodeReq[wire.KillSessionReq](payload, ops.sendError)
		if !ok {
			return false
		}
		d.runOp(func() {
			kill := d.reg.Kill
			if req.RemoveWorktree {
				kill = d.reg.KillAndRemoveWorktree
			}
			if err := kill(req.SessionID, req.Force); err != nil {
				if errors.Is(err, registry.ErrWorktreeDirty) {
					_ = ops.writeJSON(wire.FrameError, wire.Error{
						Code:      wire.ErrCodeWorktreeDirty,
						Message:   "worktree has uncommitted changes",
						SessionID: req.SessionID,
					})
				} else {
					ops.sendError("kill_failed", err.Error())
				}
			}
		})
	case wire.FrameListClosed:
		_ = ops.writeJSON(wire.FrameClosed, wire.ClosedResp{Closed: d.reg.ListClosed()})
	case wire.FrameRestoreSession:
		req, ok := decodeReq[wire.RestoreSessionReq](payload, ops.sendError)
		if !ok {
			return false
		}
		// Off the read loop like every other lifecycle op: a restore
		// can recreate a worktree, which shells out to git and takes
		// seconds on a large repo.
		d.runOp(func() {
			id := req.SessionID
			if id == "" {
				// "Reopen the last one." Resolved daemon-side so the
				// client cannot race a prune between LIST_CLOSED and
				// the restore it based on that list.
				closed := d.reg.ListClosed()
				if len(closed) == 0 {
					ops.sendError("no_closed_sessions", "no recently closed session to reopen")
					return
				}
				id = closed[0].SessionID
			}
			_, res, err := d.reg.Restore(id, d.cfg.BootstrapSession)
			if err != nil {
				switch {
				case errors.Is(err, registry.ErrNotFound):
					ops.sendError("no_such_closed_session", "that session is no longer restorable")
				case errors.Is(err, registry.ErrExists):
					ops.sendError("session_already_open", "that session is already open")
				default:
					ops.sendError("restore_failed", err.Error())
				}
				// The reopen list is stale either way — a pruned or
				// already-restored tombstone is exactly why this
				// failed, so send it before the client acts again.
				_ = ops.writeJSON(wire.FrameClosed, wire.ClosedResp{Closed: d.reg.ListClosed()})
				return
			}
			// The entry's own "added" event has already gone out to
			// every listener via the registry broadcast. This reports
			// only what the restore could not bring back.
			_ = ops.writeJSON(wire.FrameSessionRestored, wire.RestoredResp{
				SessionID:         id,
				ProjectReassigned: res.ProjectReassigned,
				WorktreeRecreated: res.WorktreeRecreated,
				WorktreeLost:      res.WorktreeLost,
				ConversationLost:  res.ConversationLost,
				AgentFellBack:     res.AgentFellBack,
				PatchPath:         res.PatchPath,
				PatchSkipped:      res.PatchSkipped,
			})
			_ = ops.writeJSON(wire.FrameClosed, wire.ClosedResp{Closed: d.reg.ListClosed()})
		})
	case wire.FrameRestartSession:
		req, ok := decodeReq[wire.RestartSessionReq](payload, ops.sendError)
		if !ok {
			return false
		}
		d.runOp(func() {
			if err := d.reg.Restart(req.SessionID); err != nil {
				ops.sendError("restart_failed", err.Error())
			}
		})
	case wire.FrameUpdateSession:
		req, ok := decodeReq[wire.UpdateSessionReq](payload, ops.sendError)
		if !ok {
			return false
		}
		if _, err := d.reg.Update(req); err != nil {
			ops.sendError("update_failed", err.Error())
		}
	case wire.FrameListProjects:
		_ = ops.writeJSON(wire.FrameProjects, wire.ProjectsResp{Projects: d.reg.ListProjects()})
	case wire.FrameCreateProject:
		req, ok := decodeReq[wire.CreateProjectReq](payload, ops.sendError)
		if !ok {
			return false
		}
		if _, err := d.reg.CreateProject(req); err != nil {
			ops.sendError("create_project_failed", err.Error())
		}
	case wire.FrameKillProject:
		req, ok := decodeReq[wire.KillProjectReq](payload, ops.sendError)
		if !ok {
			return false
		}
		d.runOp(func() {
			if err := d.reg.KillProject(req.ProjectID, req.KillSessions); err != nil {
				ops.sendError("kill_project_failed", err.Error())
			}
		})
	case wire.FrameUpdateProject:
		req, ok := decodeReq[wire.UpdateProjectReq](payload, ops.sendError)
		if !ok {
			return false
		}
		if _, err := d.reg.UpdateProject(req); err != nil {
			ops.sendError("update_project_failed", err.Error())
		}
	case wire.FrameListWorktrees:
		req, ok := decodeReq[wire.ListWorktreesReq](payload, ops.sendError)
		if !ok {
			return false
		}
		// Off the read loop: the inventory shells out to git once
		// per worktree, which on a big repo is slow enough to
		// stall every other control request.
		d.runOp(func() { ops.sendWorktrees(req.ProjectID, "list_worktrees_failed") })
	case wire.FrameRemoveWorktree:
		req, ok := decodeReq[wire.RemoveWorktreeReq](payload, ops.sendError)
		if !ok {
			return false
		}
		d.runOp(func() {
			err := d.reg.RemoveWorktree(req.ProjectID, req.Path,
				req.Force, req.DeleteBranch, req.DeleteRemote)
			ops.finishMutation(req.ProjectID, err, "remove_worktree_failed")
		})
	case wire.FrameCreateWorktree:
		req, ok := decodeReq[wire.CreateWorktreeReq](payload, ops.sendError)
		if !ok {
			return false
		}
		d.runOp(func() {
			_, err := d.reg.CreateWorktreeForBranch(ctx, req.ProjectID, req.Branch)
			ops.finishMutation(req.ProjectID, err, "create_worktree_failed")
		})
	case wire.FrameDeleteBranch:
		req, ok := decodeReq[wire.DeleteBranchReq](payload, ops.sendError)
		if !ok {
			return false
		}
		d.runOp(func() {
			err := d.reg.DeleteBranch(req.ProjectID, req.Branch,
				req.Force, req.DeleteRemote)
			ops.finishMutation(req.ProjectID, err, "delete_branch_failed")
		})
	case wire.FrameRenameWorktree:
		req, ok := decodeReq[wire.RenameWorktreeReq](payload, ops.sendError)
		if !ok {
			return false
		}
		d.runOp(func() {
			err := d.reg.RenameWorktree(req.ProjectID, req.Path, req.NewBranch)
			ops.finishMutation(req.ProjectID, err, "rename_worktree_failed")
		})
	case wire.FrameClientCommand:
		cmd, ok := decodeReq[wire.ClientCommand](payload, ops.sendError)
		if !ok {
			return false
		}
		// Validate, don't interpret. The daemon is the only thing every
		// client holds a connection to, so it is the relay — but the
		// verbs are about client-side UI state and mean nothing here.
		// The allowlist exists so a typo is refused to its sender
		// rather than fanned out as a frame every window has to guess
		// at.
		if !wire.ClientCommands[cmd.Cmd] {
			ops.sendError("unknown_client_command",
				fmt.Sprintf("client command %q is not recognised", cmd.Cmd))
			return false
		}
		d.commands.Publish(cmd)
	default:
		log.Printf("hived: unexpected control frame: %s", ft)
	}
	return false
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
		// Distinguish "not spawned yet" from "dead". SESSION_EVENT
		// (added) now fires before the PTY exists, so an eager attach
		// is a normal race, not a failure: the client should wait for
		// the event that moves the session to wire.PhaseReady.
		if d.reg.Phase(sessionID) != wire.PhaseReady {
			_ = wire.WriteJSON(conn, wire.FrameError, wire.Error{
				Code:      wire.ErrCodeSessionStarting,
				Message:   "session is still starting",
				SessionID: sessionID,
			})
			return
		}
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
		Version:        wire.PROTOCOL_VERSION,
		BuildID:        buildinfo.BuildID(),
		Release:        buildinfo.Version(),
		DaemonContract: buildinfo.DaemonContract,
		Mode:           wire.ModeAttach,
		SessionID:      entry.ID,
		Cols:           cols,
		Rows:           rows,
	}); err != nil {
		return
	}

	sink := &frameSink{conn: conn}
	// SubscribeWithAtomicReplay holds s.mu across both the snapshot
	// capture and the writeReplay call, so deliver cannot fanout to
	// any sink (including this one before it's registered) while the
	// Begin/replay/Done sequence is being written. The sink is
	// registered for live fanout only after writeReplay returns
	// successfully, so live bytes start arriving on the wire strictly
	// after Done.
	unsub, err := sess.SubscribeWithAtomicReplay(sink, func(replay []byte) error {
		return sink.writeReplay(replay, 16<<10)
	})
	if err != nil {
		return
	}
	defer unsub()

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
		case wire.FrameRequestReplay:
			// Client (typically the GUI after a width-changing resize)
			// asks us to re-stream the scrollback. The sink is already
			// registered for live fanout, so EmitAtomicReplay's hold of
			// s.mu blocks deliver entirely until the Begin/replay/Done
			// sequence is on the wire. After release, queued live data
			// resumes in order.
			if err := sess.EmitAtomicReplay(func(replay []byte) error {
				return sink.writeReplay(replay, 16<<10)
			}); err != nil {
				return
			}
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

// writeReplay streams Begin → chunked replay bytes → Done to the
// client under f.mu, so any concurrent non-fanout writes to this sink
// serialize behind us. The caller is expected to run this via the
// session's atomic-replay helpers (SubscribeWithAtomicReplay /
// EmitAtomicReplay) so that s.mu also serializes us against deliver
// — without that outer serialization, a live fanout that started
// before we acquired f.mu would write its byte to the wire BEFORE
// the Begin event, get rendered by xterm in `live` phase, then get
// wiped by term.reset() when Begin arrives. That is the exact
// "live text overwriting scrollback" symptom the replay protocol
// exists to eliminate.
//
// chunk is the max payload size per FrameData; pass 16<<10 to match
// existing snapshot chunking.
func (f *frameSink) writeReplay(replay []byte, chunk int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := wire.WriteJSON(f.conn, wire.FrameEvent, wire.Event{
		Kind: wire.EventScrollbackReplayBegin,
	}); err != nil {
		return err
	}
	for len(replay) > 0 {
		n := min(chunk, len(replay))
		if err := wire.WriteFrame(f.conn, wire.FrameData, replay[:n]); err != nil {
			return err
		}
		replay = replay[n:]
	}
	return wire.WriteJSON(f.conn, wire.FrameEvent, wire.Event{
		Kind: wire.EventScrollbackReplayDone,
	})
}

func (f *frameSink) Close() error { return f.conn.Close() }

// bootstrapWanted reports whether opts has any non-default field set.
// Can't use struct equality because session.Options has a slice field.
func bootstrapWanted(opts session.Options) bool {
	return opts.Shell != "" || opts.Cols != 0 || opts.Rows != 0 || len(opts.Env) > 0
}
