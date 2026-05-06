package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/buildinfo"
	hdaemon "github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/notify"
	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails-bound type. Multi-session model:
//   - one control connection (ConnectControl)
//   - one attach connection per session the user has opened
//     (OpenSession), keyed by session ID
type App struct {
	ctx       context.Context
	launchDir string // captured at process start; passed to hived as --cwd

	// Restored window geometry. Set by main() before Wails starts.
	// Position can't be applied until we have the runtime ctx, so it
	// happens in startup().
	initialX, initialY int
	haveInitialPos     bool

	mu       sync.Mutex
	control  *connState                // control connection (or nil)
	attaches map[string]*connState     // session id → attach connection
}

// connState wraps a connection with a write mutex so multiple goroutines
// (frontend writes vs. internal pings) don't interleave bytes.
type connState struct {
	conn    net.Conn
	writeMu sync.Mutex
}

func (c *connState) writeJSON(t wire.FrameType, v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return wire.WriteJSON(c.conn, t, v)
}

func (c *connState) writeFrame(t wire.FrameType, p []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return wire.WriteFrame(c.conn, t, p)
}

// Notify fires a native OS notification. Wails' webview lacks the HTML5
// Notification API on macOS (WKWebView), so the frontend calls into Go
// instead. tag round-trips back to the frontend via the "bell-click"
// Wails event when the user clicks the notification (darwin only).
// Errors are logged but not surfaced — notifications are best-effort UX.
func (a *App) Notify(title, subtitle, body, tag string) error {
	if err := notify.Notify(title, subtitle, body, tag); err != nil {
		log.Printf("hivegui: notify failed: %v", err)
		return err
	}
	return nil
}

func NewApp(launchDir string) *App {
	return &App{
		launchDir: launchDir,
		attaches:  make(map[string]*connState),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if a.haveInitialPos {
		wruntime.WindowSetPosition(ctx, a.initialX, a.initialY)
	}
	// Click on a notification → ObjC delegate has already called
	// [NSApp activateIgnoringOtherApps:YES] to bring Hive forward.
	// We just need to tell the frontend which session to switch to.
	// Do it from a goroutine so the cgo callback returns immediately
	// and we don't risk reentering Wails on the AppKit thread.
	notify.SetActivationHandler(func(tag string) {
		go wruntime.EventsEmit(ctx, "bell-click", tag)
	})
	go a.persistGeometryLoop(ctx)
}

func (a *App) shutdown(ctx context.Context) {
	a.saveGeometry()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.control != nil {
		_ = a.control.conn.Close()
	}
	for _, c := range a.attaches {
		_ = c.conn.Close()
	}
}

// persistGeometryLoop polls window position + size every 2s and
// writes a fresh window.json whenever they change. Cheap, and means
// a SIGKILL'd GUI still keeps most of its geometry next launch
// (worst case the last 2s of moves are lost).
func (a *App) persistGeometryLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	var last windowGeometry
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			x, y := wruntime.WindowGetPosition(ctx)
			w, h := wruntime.WindowGetSize(ctx)
			cur := windowGeometry{X: x, Y: y, W: w, H: h}
			if cur != last && cur.W >= 320 && cur.H >= 240 {
				if err := saveWindowGeometry(cur); err == nil {
					last = cur
				}
			}
		}
	}
}

// saveGeometry writes the current window geometry once. Called at
// shutdown so the very last position survives a clean quit.
func (a *App) saveGeometry() {
	if a.ctx == nil {
		return
	}
	x, y := wruntime.WindowGetPosition(a.ctx)
	w, h := wruntime.WindowGetSize(a.ctx)
	if w < 320 || h < 240 {
		return
	}
	_ = saveWindowGeometry(windowGeometry{X: x, Y: y, W: w, H: h})
}

// ----------------------------- control conn -----------------------------

// ConnectControl opens (or reuses) the control connection. The
// daemon will push an unsolicited SESSIONS snapshot followed by
// SESSION_EVENT messages — these are forwarded to the frontend as
// "session:list" and "session:event" events.
func (a *App) ConnectControl() error {
	a.mu.Lock()
	if a.control != nil {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()

	conn, err := dialOrSpawn(hdaemon.SocketPath(), a.launchDir)
	if err != nil {
		return err
	}
	if err := wire.WriteJSON(conn, wire.FrameHello, wire.Hello{
		Version: wire.PROTOCOL_VERSION,
		Client:  "hivegui/0.2",
		BuildID: buildinfo.BuildID(),
		Mode:    wire.ModeControl,
	}); err != nil {
		_ = conn.Close()
		return fmt.Errorf("control hello: %w", err)
	}
	var welcome wire.Welcome
	ft, err := wire.ReadJSON(conn, &welcome)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("control welcome: %w", err)
	}
	if ft != wire.FrameWelcome {
		_ = conn.Close()
		return fmt.Errorf("control: expected WELCOME, got %s", ft)
	}

	cs := &connState{conn: conn}
	a.mu.Lock()
	a.control = cs
	a.mu.Unlock()
	go a.controlReadLoop(cs)
	a.emitDaemonVersionStatus(welcome.BuildID)
	return nil
}

// DaemonStaleEvent is the payload of the "daemon:stale" Wails event.
// Severity is "match" (silent — emitted so the frontend can clear a
// previously-shown banner), "mismatch" (both builds known and differ),
// or "unknown" (one or both sides did not advertise a build).
type DaemonStaleEvent struct {
	Severity    string `json:"severity"`
	GuiBuild    string `json:"guiBuild"`
	DaemonBuild string `json:"daemonBuild"`
}

func (a *App) emitDaemonVersionStatus(daemonBuild string) {
	gui := buildinfo.BuildID()
	ev := DaemonStaleEvent{GuiBuild: gui, DaemonBuild: daemonBuild}
	switch {
	case gui == "" || daemonBuild == "":
		ev.Severity = "unknown"
	case gui == daemonBuild:
		ev.Severity = "match"
	default:
		ev.Severity = "mismatch"
	}
	wruntime.EventsEmit(a.ctx, "daemon:stale", ev)
}

// RestartDaemon kills the running hived and relaunches the GUI as a
// detached child, then quits this process. Reconnecting in-place left
// the existing window holding stale session state (xterm buffers,
// attach conns) that no longer matched the fresh daemon; a full GUI
// restart sidesteps that by starting from a clean slate.
//
// killRunningHived blocks until the previous daemon is actually gone
// so the relaunched GUI's dialOrSpawn binds a fresh socket. We kill
// before spawning the child for the same reason — otherwise the new
// GUI would happily reconnect to the old daemon.
func (a *App) RestartDaemon() error {
	a.mu.Lock()
	if a.control != nil {
		_ = a.control.conn.Close()
		a.control = nil
	}
	for _, c := range a.attaches {
		_ = c.conn.Close()
	}
	a.attaches = make(map[string]*connState)
	a.mu.Unlock()

	if err := killRunningHived(hdaemon.SocketPath()); err != nil {
		return fmt.Errorf("kill stale hived: %w", err)
	}
	if err := spawnNewGUI(a.launchDir); err != nil {
		return fmt.Errorf("relaunch GUI: %w", err)
	}
	wruntime.Quit(a.ctx)
	return nil
}

func (a *App) controlReadLoop(cs *connState) {
	defer func() {
		a.mu.Lock()
		if a.control == cs {
			a.control = nil
		}
		a.mu.Unlock()
		_ = cs.conn.Close()
		wruntime.EventsEmit(a.ctx, "control:disconnect", "")
	}()
	for {
		ft, payload, err := wire.ReadFrame(cs.conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("hivegui: control read: %v", err)
			}
			return
		}
		switch ft {
		case wire.FrameSessions:
			wruntime.EventsEmit(a.ctx, "session:list", string(payload))
		case wire.FrameSessionEvent:
			wruntime.EventsEmit(a.ctx, "session:event", string(payload))
		case wire.FrameProjects:
			wruntime.EventsEmit(a.ctx, "project:list", string(payload))
		case wire.FrameProjectEvent:
			wruntime.EventsEmit(a.ctx, "project:event", string(payload))
		case wire.FrameError:
			wruntime.EventsEmit(a.ctx, "control:error", string(payload))
		default:
			log.Printf("hivegui: control unexpected frame %s", ft)
		}
	}
}

// AgentInfo is the JSON shape the frontend uses to render the launcher.
type AgentInfo struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Color      string   `json:"color"`
	Available  bool     `json:"available"`
	InstallCmd []string `json:"installCmd,omitempty"`
}

// ListAgents returns every built-in agent definition. The frontend
// uses this to populate the launcher menu.
func (a *App) ListAgents() []AgentInfo {
	defs := agent.All()
	out := make([]AgentInfo, 0, len(defs))
	for _, d := range defs {
		out = append(out, AgentInfo{
			ID:         string(d.ID),
			Name:       d.Name,
			Color:      d.Color,
			Available:  d.Available(),
			InstallCmd: d.InstallCmd,
		})
	}
	return out
}

// CreateSession asks the daemon to create a new session. agentID is
// the canonical ID from ListAgents (e.g. "claude") or "" for a
// generic shell. projectID is the owning project ("" = default).
// useWorktree, when true and the project's cwd is a git repo, makes
// the daemon spawn the session inside a fresh git worktree under
// <gitRoot>/.worktrees/. The daemon broadcasts a SESSION_EVENT(added)
// over the control connection; the frontend updates the sidebar from
// that.
func (a *App) CreateSession(agentID, projectID, name, color string, cols, rows int, useWorktree bool) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.writeJSON(wire.FrameCreateSession, wire.CreateSpec{
		Agent:       agentID,
		ProjectID:   projectID,
		Name:        name,
		Color:       color,
		Cols:        cols,
		Rows:        rows,
		UseWorktree: useWorktree,
	})
}

// CreateProject creates a new project.
func (a *App) CreateProject(name, color, cwd string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.writeJSON(wire.FrameCreateProject, wire.CreateProjectReq{
		Name: name, Color: color, Cwd: cwd,
	})
}

// KillProject removes a project. If killSessions is true, every
// session in the project is also killed; otherwise sessions are
// reassigned to the default project.
func (a *App) KillProject(id string, killSessions bool) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.writeJSON(wire.FrameKillProject, wire.KillProjectReq{
		ProjectID: id, KillSessions: killSessions,
	})
}

// UpdateProject patches name/color/cwd/order. Empty strings on
// name/color/cwd mean "no change"; -1 on order means "no change".
func (a *App) UpdateProject(id, name, color, cwd string, order int) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	req := wire.UpdateProjectReq{ProjectID: id}
	if name != "" {
		req.Name = &name
	}
	if color != "" {
		req.Color = &color
	}
	if cwd != "" {
		req.Cwd = &cwd
	}
	if order >= 0 {
		req.Order = &order
	}
	return cs.writeJSON(wire.FrameUpdateProject, req)
}

// LaunchDir returns the cwd captured at GUI startup; useful for the
// new-project default cwd.
func (a *App) LaunchDir() string { return a.launchDir }

// PickDirectory opens the OS native folder picker and returns the
// selected path, or "" if the user cancelled. defaultDir, if
// non-empty, sets the dialog's starting location.
func (a *App) PickDirectory(defaultDir string) (string, error) {
	// macOS NSOpenPanel silently fails when DefaultDirectory points
	// at a missing path, so fall back to launchDir if the saved cwd
	// no longer exists.
	if defaultDir != "" {
		if st, err := os.Stat(defaultDir); err != nil || !st.IsDir() {
			defaultDir = ""
		}
	}
	if defaultDir == "" {
		defaultDir = a.launchDir
	}
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:                "Choose project directory",
		DefaultDirectory:     defaultDir,
		CanCreateDirectories: true,
	})
}

// Confirm shows a native yes/no dialog and reports the user's choice.
// Wails' WebKit on macOS silently no-ops window.confirm(), so the
// frontend routes confirmations through here instead.
func (a *App) Confirm(title, message string) bool {
	res, err := wruntime.MessageDialog(a.ctx, wruntime.MessageDialogOptions{
		Type:          wruntime.QuestionDialog,
		Title:         title,
		Message:       message,
		Buttons:       []string{"OK", "Cancel"},
		DefaultButton: "OK",
		CancelButton:  "Cancel",
	})
	if err != nil {
		return false
	}
	return res == "OK"
}

// OpenNewWindow spawns a second Hive GUI process. Wails v2 does not
// natively support multiple windows in a single process, so we
// re-exec the GUI binary as a detached child. The two GUIs share
// the same hived (single-instance daemon enforced by the socket
// lock), so sessions are visible from either window — each window
// can independently maximize a different session.
func (a *App) OpenNewWindow() error {
	return spawnNewGUI(a.launchDir)
}

// CloseWindow quits this GUI process. Because each window is its own
// process (multi-window is implemented by re-exec), closing the last
// window naturally ends Hive — no explicit "quit app" plumbing
// needed.
func (a *App) CloseWindow() {
	wruntime.Quit(a.ctx)
}

// KillSession asks the daemon to terminate a session. force=true
// skips the dirty-worktree safety check and discards uncommitted
// changes. Without force, killing a session whose worktree has
// uncommitted changes returns a "worktree_dirty" control error so
// the GUI can confirm with the user.
func (a *App) KillSession(id string, force bool) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.writeJSON(wire.FrameKillSession, wire.KillSessionReq{
		SessionID: id, Force: force,
	})
}

// IsGitRepo reports whether path is inside a git repository. The GUI
// uses this to gate the launcher's worktree checkbox.
func (a *App) IsGitRepo(path string) bool {
	return worktree.IsGitRepo(path)
}

// OpenURL hands a URL to the OS default browser. Used by the
// xterm web-links addon when the user cmd-clicks a URL in a session.
func (a *App) OpenURL(url string) {
	if url == "" {
		return
	}
	wruntime.BrowserOpenURL(a.ctx, url)
}

// UpdateSession patches name/color/order. Empty strings on name/color
// mean "do not change"; -1 on order means "do not change".
func (a *App) UpdateSession(id, name, color string, order int) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	req := wire.UpdateSessionReq{SessionID: id}
	if name != "" {
		req.Name = &name
	}
	if color != "" {
		req.Color = &color
	}
	if order >= 0 {
		req.Order = &order
	}
	return cs.writeJSON(wire.FrameUpdateSession, req)
}

func (a *App) requireControl() (*connState, error) {
	a.mu.Lock()
	cs := a.control
	a.mu.Unlock()
	if cs == nil {
		return nil, errors.New("no control connection")
	}
	return cs, nil
}

// ----------------------------- attach conns -----------------------------

// AttachInfo is what the frontend gets back from OpenSession.
type AttachInfo struct {
	SessionID string `json:"sessionId"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

// OpenSession opens an attach connection to the given session. The
// frontend should call this once per session it wants to render.
// PTY bytes arrive as "pty:data" events tagged with the session id.
func (a *App) OpenSession(id string, cols, rows int) (*AttachInfo, error) {
	a.mu.Lock()
	if _, ok := a.attaches[id]; ok {
		a.mu.Unlock()
		return &AttachInfo{SessionID: id, Cols: cols, Rows: rows}, nil // already open
	}
	a.mu.Unlock()

	conn, err := dialOrSpawn(hdaemon.SocketPath(), a.launchDir)
	if err != nil {
		return nil, err
	}
	if err := wire.WriteJSON(conn, wire.FrameHello, wire.Hello{
		Version:   wire.PROTOCOL_VERSION,
		Client:    "hivegui/0.2",
		Mode:      wire.ModeAttach,
		SessionID: id,
	}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("attach hello: %w", err)
	}
	ft, payload, err := wire.ReadFrame(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("attach welcome: %w", err)
	}
	if ft == wire.FrameError {
		_ = conn.Close()
		var werr wire.Error
		if json.Unmarshal(payload, &werr) == nil && werr.Message != "" {
			return nil, fmt.Errorf("attach failed: %s", werr.Message)
		}
		return nil, errors.New("attach failed: daemon rejected attach")
	}
	if ft != wire.FrameWelcome {
		_ = conn.Close()
		return nil, fmt.Errorf("attach: unexpected frame %s", ft)
	}
	var welcome wire.Welcome
	if err := json.Unmarshal(payload, &welcome); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("attach welcome: %w", err)
	}

	cs := &connState{conn: conn}
	a.mu.Lock()
	a.attaches[id] = cs
	a.mu.Unlock()
	go a.attachReadLoop(id, cs)

	// Issue the frontend's preferred size right after the handshake;
	// the daemon's WELCOME reports its current size which may differ.
	if cols > 0 && rows > 0 && (cols != welcome.Cols || rows != welcome.Rows) {
		_ = cs.writeJSON(wire.FrameResize, wire.Resize{Cols: cols, Rows: rows})
	}

	return &AttachInfo{
		SessionID: id, Cols: welcome.Cols, Rows: welcome.Rows,
	}, nil
}

func (a *App) attachReadLoop(id string, cs *connState) {
	defer func() {
		a.mu.Lock()
		if a.attaches[id] == cs {
			delete(a.attaches, id)
		}
		a.mu.Unlock()
		_ = cs.conn.Close()
		wruntime.EventsEmit(a.ctx, "pty:disconnect", id)
	}()
	for {
		ft, payload, err := wire.ReadFrame(cs.conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("hivegui: attach %s read: %v", id, err)
			}
			return
		}
		switch ft {
		case wire.FrameData:
			wruntime.EventsEmit(a.ctx, "pty:data", id, base64.StdEncoding.EncodeToString(payload))
		case wire.FrameEvent:
			wruntime.EventsEmit(a.ctx, "pty:event", id, string(payload))
		case wire.FrameError:
			wruntime.EventsEmit(a.ctx, "pty:error", id, string(payload))
		default:
			log.Printf("hivegui: attach %s unexpected frame %s", id, ft)
		}
	}
}

// CloseAttach drops the GUI's attach connection without killing the
// underlying session. Equivalent to "stop rendering this tab" — useful
// once we have N sessions and want to free the connection slot.
func (a *App) CloseAttach(id string) error {
	a.mu.Lock()
	cs, ok := a.attaches[id]
	if ok {
		delete(a.attaches, id)
	}
	a.mu.Unlock()
	if !ok {
		return nil
	}
	return cs.conn.Close()
}

// WriteStdin forwards keystrokes to the attached session.
func (a *App) WriteStdin(id, b64 string) error {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return err
	}
	cs, err := a.attachFor(id)
	if err != nil {
		return err
	}
	return cs.writeFrame(wire.FrameData, data)
}

// ResizeSession sends a RESIZE control frame on the attach connection.
func (a *App) ResizeSession(id string, cols, rows int) error {
	cs, err := a.attachFor(id)
	if err != nil {
		return err
	}
	return cs.writeJSON(wire.FrameResize, wire.Resize{Cols: cols, Rows: rows})
}

func (a *App) attachFor(id string) (*connState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cs, ok := a.attaches[id]
	if !ok {
		return nil, fmt.Errorf("not attached to %s", id)
	}
	return cs, nil
}

// ----------------------------- daemon spawn ------------------------------

// dialOrSpawn dials hived; on failure spawns it as a detached child
// and retries with backoff for up to ~3s. cwd, when non-empty, is
// passed to hived as --cwd so newly-created sessions default to that
// directory.
func dialOrSpawn(sock, cwd string) (net.Conn, error) {
	if c, err := net.Dial("unix", sock); err == nil {
		return c, nil
	}
	if err := spawnHived(sock, cwd); err != nil {
		return nil, fmt.Errorf("spawn hived: %w", err)
	}
	delays := []time.Duration{100, 200, 400, 800, 1600}
	for _, ms := range delays {
		time.Sleep(ms * time.Millisecond)
		if c, err := net.Dial("unix", sock); err == nil {
			return c, nil
		}
	}
	return nil, fmt.Errorf("hived did not come up at %s", sock)
}
