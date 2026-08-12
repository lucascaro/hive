package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/atotto/clipboard"
	"github.com/lucascaro/hive/internal/activity"
	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/buildinfo"
	hdaemon "github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/notify"
	"github.com/lucascaro/hive/internal/registry"
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
	control  *wire.Client            // control connection (or nil)
	attaches map[string]*wire.Client // session id → attach connection

	// openMu serializes OpenSession calls. Without it, two concurrent
	// OpenSession(id) calls both observe an empty attaches[id], both
	// dial the daemon, and both register attach subscribers — the
	// session then fan-outs every byte (and the scrollback snapshot)
	// twice, producing visibly duplicated output in xterm.
	openMu sync.Mutex

	// debugTrace mirrors the frontend's `hive.debug` localStorage flag so
	// the Debug menu can say which state it will move to. Owned by the main
	// thread: written only by SetDebugTrace (a Wails binding call, which
	// Wails dispatches on the main thread), read only by buildAppMenu.
	debugTrace bool
}

// SetDebugTrace records whether the frontend's scroll/replay tracer is
// currently armed and relabels the Debug menu accordingly.
//
// The flag lives in the webview's localStorage, which Go cannot read, so the
// frontend pushes it up at startup — and the toggle reloads the page, so the
// new value arrives the same way. Without this the item read "Toggle Debug
// Trace" forever with no way to tell whether tracing was already on; the
// tracer is deliberately invisible when armed, so the menu is the only
// indicator there is.
func (a *App) SetDebugTrace(on bool) {
	a.debugTrace = on
	if a.ctx == nil {
		return
	}
	m := buildAppMenu(a)
	if m == nil {
		return // no native menu on this platform — see menu_other.go
	}
	wruntime.MenuSetApplicationMenu(a.ctx, m)
	wruntime.MenuUpdateApplicationMenu(a.ctx)
}

// Notify fires a native OS notification. Wails' webview lacks the HTML5
// Notification API on macOS (WKWebView), so the frontend calls into Go
// instead. tag round-trips back to the frontend via the "bell-click"
// Wails event when the user clicks the notification (darwin only).
// Errors are logged but not surfaced — notifications are best-effort UX.
// SetClipboardText writes text to the system clipboard.
//
// Replaces wails runtime.ClipboardSetText, which is broken on Windows:
// the JS-bridged call runs on a non-STA goroutine, so OpenClipboard
// silently fails and nothing reaches the clipboard. Reads
// (ClipboardGetText) work since they don't require clipboard ownership.
// atotto/clipboard shells out to clip.exe on Windows, sidestepping the
// threading constraint entirely.
func (a *App) SetClipboardText(s string) error {
	if err := clipboard.WriteAll(s); err != nil {
		log.Printf("hivegui: clipboard write failed: %v", err)
		return err
	}
	return nil
}

func (a *App) Notify(title, subtitle, body, tag string) error {
	if err := notify.Notify(title, subtitle, body, tag); err != nil {
		log.Printf("hivegui: notify failed: %v", err)
		return err
	}
	return nil
}

// LogFrontend tees a frontend diagnostic line to hivegui.log. The webview's
// own console goes to /dev/null under LaunchServices, so freeze/renderer
// hypotheses (WebGL context-loss storms, reconnect loops) have nowhere to
// land otherwise. Kept dead simple: one prefixed line per call.
func (a *App) LogFrontend(msg string) {
	log.Printf("hivegui[fe]: %s", msg)
}

func NewApp(launchDir string) *App {
	// Point the agent catalog at the user's agents.json. hived does
	// the same with the same directory; each process reloads on mtime
	// change, so the GUI writing the file is all the daemon needs to
	// see a new custom agent.
	agent.SetCustomDir(registry.StateDir())
	return &App{
		launchDir: launchDir,
		attaches:  make(map[string]*wire.Client),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Opt out of macOS App Nap / activity-based timer throttling. Defensive
	// hygiene so a backgrounded webview keeps streaming PTY output and
	// repainting — NOT the fix for the reported freeze (that was a synchronous
	// full-ring scrollback replay; see session-term.ts). See internal/activity.
	activity.DisableThrottling()
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
	a.startUpdateCheckLoop(ctx)
}

func (a *App) shutdown(ctx context.Context) {
	a.saveGeometry()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.control != nil {
		_ = a.control.Close()
	}
	for _, c := range a.attaches {
		_ = c.Close()
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

// dialHandshake dials the daemon socket (spawning hived if needed) and
// performs the HELLO/WELCOME handshake via the shared wire client.
func (a *App) dialHandshake(hello wire.Hello) (*wire.Client, error) {
	conn, err := dialOrSpawn(hdaemon.SocketPath(), a.launchDir)
	if err != nil {
		return nil, err
	}
	return wire.Handshake(conn, hello)
}

// ConnectControl opens a fresh control connection, replacing any existing
// one. The daemon pushes an unsolicited PROJECTS + SESSIONS snapshot on
// handshake, followed by SESSION_EVENT messages — forwarded to the frontend
// as "session:list" and "session:event" events.
//
// It REPLACES rather than reuses, and that is the whole point. Every caller
// is a frontend with no session state that needs the snapshot: main.ts runs
// this once per page load, and the reconnect loop runs it after a drop. The
// snapshot only ever arrives on handshake, so reusing a live connection
// returned success while leaving a freshly-loaded page with a permanently
// empty session list — the "no sessions" screen after any location.reload(),
// which in practice means every time the Debug menu's trace toggle is used
// (the only reload in the app). A redial is cheap; a silently empty UI is
// not.
//
// The webview surviving a reload while the Go process does not is exactly
// what makes this reachable: a.control outlives the page that opened it.
func (a *App) ConnectControl() error {
	// Detach the old connection BEFORE closing it, so its read loop sees
	// itself superseded and stays quiet — see controlReadLoop. Closing it
	// while still installed would emit control:disconnect, which starts the
	// reconnect loop, which calls back into here: an endless redial.
	if old := a.detachControl(); old != nil {
		_ = old.Close()
	}

	cs, err := a.dialHandshake(wire.Hello{
		Client:  "hivegui/0.2",
		BuildID: buildinfo.BuildID(),
		Mode:    wire.ModeControl,
	})
	if err != nil {
		return fmt.Errorf("control: %w", err)
	}

	a.mu.Lock()
	a.control = cs
	a.mu.Unlock()
	go a.controlReadLoop(cs)
	a.emitDaemonVersionStatus(cs.Welcome().BuildID, cs.Welcome().Release)
	return nil
}

// DaemonStaleEvent is the payload of the "daemon:stale" Wails event.
// Severity is "match" (silent — emitted so the frontend can clear a
// previously-shown banner), "mismatch" (both builds known and differ),
// or "unknown" (one or both sides did not advertise a build).
//
// The *Release fields carry the human-readable versions (buildinfo.Version)
// so the sidebar footer can display them; they are informational only and
// deliberately take no part in the Severity decision — see below.
type DaemonStaleEvent struct {
	Severity      string `json:"severity"`
	GuiBuild      string `json:"guiBuild"`
	DaemonBuild   string `json:"daemonBuild"`
	GuiRelease    string `json:"guiRelease"`
	DaemonRelease string `json:"daemonRelease"`
}

// daemonVersionEvent builds the "daemon:stale" payload. Split out from
// the emit so it is testable without a live Wails context.
//
// Severity is computed from build IDs alone: those are git revisions, so
// equal build IDs already imply equal releases, and comparing releases too
// would only add a second source of truth to keep in sync. daemonRelease is
// empty when talking to a daemon built before Welcome gained the Release
// field — consumers fall back to build-ID-only display.
func daemonVersionEvent(daemonBuild, daemonRelease string) DaemonStaleEvent {
	gui := buildinfo.BuildID()
	ev := DaemonStaleEvent{
		GuiBuild:      gui,
		DaemonBuild:   daemonBuild,
		GuiRelease:    buildinfo.Version(),
		DaemonRelease: daemonRelease,
	}
	switch {
	case gui == "" || daemonBuild == "":
		ev.Severity = "unknown"
	case gui == daemonBuild:
		ev.Severity = "match"
	default:
		ev.Severity = "mismatch"
	}
	return ev
}

// emitDaemonVersionStatus reports the GUI/daemon build relationship to the
// frontend. Both the stale-daemon banner and the sidebar version footer
// listen for this event.
func (a *App) emitDaemonVersionStatus(daemonBuild, daemonRelease string) {
	wruntime.EventsEmit(a.ctx, "daemon:stale", daemonVersionEvent(daemonBuild, daemonRelease))
}

// restartKillBudget bounds each of the two kill channels' wait for
// the socket to go quiet. hived's shutdown is a listener close plus a
// registry flush, so this is generous.
const restartKillBudget = 3 * time.Second

// RestartDaemon stops the running hived, relaunches the GUI as a
// detached child, and quits this process. Reconnecting in-place left
// the existing window holding stale session state (xterm buffers,
// attach conns) that no longer matched the fresh daemon; a full GUI
// restart sidesteps that by starting from a clean slate.
//
// The daemon is stopped over the control connection we already hold
// (FrameShutdown) and, failing that, by signalling the pid recorded
// in <sock>.pid. Either way the socket is probed afterwards: only
// once nothing answers do we relaunch and quit. That ordering is the
// whole point — killRunningHived can return nil without having killed
// anything (missing pidfile, unrecognised process name), and the
// relaunched GUI's dialOrSpawn would then reconnect to the very
// daemon the user asked to replace, silently.
//
// If the daemon survives both channels we return an error and stay
// put. A visible failure in a working window beats quitting into a
// window that looks restarted and isn't.
func (a *App) RestartDaemon() error {
	sock := hdaemon.SocketPath()

	a.mu.Lock()
	control := a.control
	a.mu.Unlock()

	// Nothing is torn down until the daemon is confirmed gone. The
	// error path below has to leave a *working* window behind, and
	// there is no recovery route back: ConnectControl runs once from
	// the frontend's boot path, and the control:disconnect handler
	// only sets a status line (and is suppressed outright while a
	// restart is in flight). Closing conns up front would strand the
	// user in a dead window on exactly the path meant to protect
	// them. Sending FrameShutdown does not require closing the conn,
	// and socketDead dials its own.
	dead := false
	if control != nil {
		// writeFrame, not wire.WriteFrame: the header and payload are
		// two Write calls, and the frontend can be writing to this
		// same conn concurrently. Every other writer takes writeMu.
		if err := control.WriteFrame(wire.FrameShutdown, nil); err != nil {
			log.Printf("hivegui: restart: send shutdown frame: %v", err)
		}
		dead = socketDead(sock, restartKillBudget)
		log.Printf("hivegui: restart: in-band shutdown left socket dead=%v", dead)
	} else {
		log.Printf("hivegui: restart: no control conn, skipping in-band shutdown")
	}

	if !dead {
		// A kill error is logged, not returned: hived is a child the
		// GUI never Wait()s on, so a SIGTERM'd daemon lingers as a
		// zombie and the signal-based wait reports "still alive" for a
		// process that has already released the socket. The socket
		// probe below is the arbiter.
		if err := killRunningHived(sock); err != nil {
			log.Printf("hivegui: restart: kill hived: %v", err)
		}
		dead = socketDead(sock, restartKillBudget)
		log.Printf("hivegui: restart: signal path left socket dead=%v", dead)
	}
	if !dead {
		// Everything is still wired up — the window the user is
		// looking at keeps working, and the banner shows why.
		return fmt.Errorf("hived still answering on %s after shutdown and signal; not restarting", sock)
	}

	// The daemon is gone; these conns are dead sockets now. Release
	// them before the relaunch so the outgoing process isn't holding
	// half-open fds while the new GUI comes up.
	a.mu.Lock()
	if a.control != nil {
		_ = a.control.Close()
		a.control = nil
	}
	for _, c := range a.attaches {
		_ = c.Close()
	}
	a.attaches = make(map[string]*wire.Client)
	a.mu.Unlock()

	if err := spawnNewGUI(a.launchDir); err != nil {
		return fmt.Errorf("relaunch GUI: %w", err)
	}
	log.Printf("hivegui: restart: relaunched, quitting")
	wruntime.Quit(a.ctx)
	return nil
}

// detachControl clears the installed control connection and returns it, so
// the caller can close a connection that no read loop still considers
// current. Returns nil when there was none.
func (a *App) detachControl() *wire.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.control
	a.control = nil
	return old
}

// retireControl clears cs if it is still the installed control connection,
// and reports whether it was. False means cs was superseded — ConnectControl
// already replaced it deliberately, so its ending is not a lost daemon and
// must not be announced as one.
func (a *App) retireControl(cs *wire.Client) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.control != cs {
		return false
	}
	a.control = nil
	return true
}

func (a *App) controlReadLoop(cs *wire.Client) {
	defer func() {
		current := a.retireControl(cs)
		_ = cs.Close()
		// Only the CURRENT connection ending means the GUI lost the daemon.
		// A superseded one was closed deliberately by ConnectControl, and
		// announcing that as a disconnect would start the reconnect loop,
		// which redials, which supersedes again — a redial that never
		// settles. Stay quiet: a live replacement is already installed.
		if current {
			wruntime.EventsEmit(a.ctx, "control:disconnect", "")
		}
	}()
	for {
		ft, payload, err := cs.ReadFrame()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("hivegui: control read: %v", err)
			}
			return
		}
		if name, ok := wire.ControlEventName(ft); ok {
			wruntime.EventsEmit(a.ctx, name, string(payload))
		} else {
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

// ListAgents returns every agent definition — built-ins plus the
// user's custom agents. The frontend uses this to populate the
// launcher menu.
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

// CustomAgent is the JSON shape the settings modal edits. It mirrors
// agent.Custom; camelCase tags match AgentInfo above (the snake_case
// convention applies to the daemon's wire payloads, not to these
// Wails bindings).
type CustomAgent struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Cmd   []string `json:"cmd"`
	Color string   `json:"color"`
}

// ListCustomAgents returns the user's custom agent definitions as
// stored on disk, for the settings modal to edit. Invalid entries are
// included deliberately — the user has to see a broken row to fix it.
//
// A malformed agents.json is an error, not an empty list. Returning
// empty would render as "no custom agents yet" and a subsequent Save
// would overwrite the very file the user needs to repair.
func (a *App) ListCustomAgents() ([]CustomAgent, error) {
	list, err := agent.LoadCustom()
	if err != nil {
		return nil, err
	}
	out := make([]CustomAgent, 0, len(list))
	for _, c := range list {
		out = append(out, CustomAgent{ID: c.ID, Name: c.Name, Cmd: c.Cmd, Color: c.Color})
	}
	return out, nil
}

// SaveCustomAgents validates and writes the full custom-agent list,
// assigning IDs to new entries. It returns a validation error rather
// than silently dropping bad entries so the modal can show the user
// what was wrong — a warning in hived.log would be invisible to them.
//
// The daemon picks the change up on its next agent.Get; no reload
// message is needed.
func (a *App) SaveCustomAgents(list []CustomAgent) error {
	in := make([]agent.Custom, 0, len(list))
	for _, c := range list {
		in = append(in, agent.Custom{ID: c.ID, Name: c.Name, Cmd: c.Cmd, Color: c.Color})
	}
	return agent.SaveCustom(in)
}

// CreateSession asks the daemon to create a new session. agentID is
// the canonical ID from ListAgents (e.g. "claude") or "" for a
// generic shell. projectID is the owning project ("" = default).
// useWorktree, when true and the project's cwd is a git repo, makes
// the daemon spawn the session inside a fresh git worktree under
// <gitRoot>/.worktrees/. The daemon broadcasts a SESSION_EVENT(added)
// over the control connection; the frontend updates the sidebar from
// that.
// insertAfter names the session the new one should sit directly beneath
// in the display order (usually the active session); "" appends.
func (a *App) CreateSession(agentID, projectID, name, color string, cols, rows int, useWorktree bool, insertAfter string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameCreateSession, wire.CreateSpec{
		Agent:                agentID,
		ProjectID:            projectID,
		Name:                 name,
		Color:                color,
		Cols:                 cols,
		Rows:                 rows,
		UseWorktree:          useWorktree,
		InsertAfterSessionID: insertAfter,
	})
}

// DuplicateSession creates a new session pinned to an explicit cwd —
// used by the GUI's ⌘P / ⇧⌘P shortcuts to fork the active session into
// the same project + directory (and same worktree, if the source had
// one). The caller resolves the cwd on the JS side from the source
// session's worktree path or its project's cwd.
//
// UseWorktree is forced to false here: when cwd already points inside a
// worktree, we want to *reuse* it, not stack a nested worktree on top.
// Passing agentID="" creates a generic shell session.
// insertAfter is normally the id of the session being duplicated, so
// the copy lands directly beneath its source; "" appends.
func (a *App) DuplicateSession(agentID, projectID, cwd string, insertAfter string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameCreateSession, wire.CreateSpec{
		Agent:                agentID,
		ProjectID:            projectID,
		Cwd:                  cwd,
		UseWorktree:          false,
		InsertAfterSessionID: insertAfter,
	})
}

// CreateProject creates a new project.
func (a *App) CreateProject(name, color, cwd string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameCreateProject, wire.CreateProjectReq{
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
	return cs.WriteJSON(wire.FrameKillProject, wire.KillProjectReq{
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
	return cs.WriteJSON(wire.FrameUpdateProject, req)
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
	return cs.WriteJSON(wire.FrameKillSession, wire.KillSessionReq{
		SessionID: id, Force: force,
	})
}

// RestartSession asks the daemon to recycle the agent process for
// the given session in place. The session entry (name/color/order/
// worktree) is preserved; the new process uses the agent's resume
// flag (e.g. `claude --continue`) when available so the prior
// conversation is picked back up.
func (a *App) RestartSession(id string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameRestartSession, wire.RestartSessionReq{
		SessionID: id,
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
	return cs.WriteJSON(wire.FrameUpdateSession, req)
}

func (a *App) requireControl() (*wire.Client, error) {
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
	// Serialize across all in-flight OpenSession calls so the dial +
	// handshake below can't race against itself for the same id and
	// register two daemon subscribers. See openMu's doc for why.
	a.openMu.Lock()
	defer a.openMu.Unlock()

	a.mu.Lock()
	if _, ok := a.attaches[id]; ok {
		a.mu.Unlock()
		return &AttachInfo{SessionID: id, Cols: cols, Rows: rows}, nil // already open
	}
	a.mu.Unlock()

	dialStart := time.Now()
	cs, err := a.dialHandshake(wire.Hello{
		Client:    "hivegui/0.2",
		Mode:      wire.ModeAttach,
		SessionID: id,
	})
	if err != nil {
		return nil, fmt.Errorf("attach failed: %w", err)
	}
	welcome := cs.Welcome()
	// Startup-latency probe: dial+handshake is a network round-trip to
	// hived per session. On a many-session grid launch these run behind
	// openMu (serialized), so a slow daemon shows here as the sum that
	// stalls startup. Logged to hivegui.log next to the frontend probes.
	log.Printf("hivegui[fe]: OpenSession id=%s dialHandshake=%dms", id, time.Since(dialStart).Milliseconds())

	a.mu.Lock()
	a.attaches[id] = cs
	a.mu.Unlock()
	go a.attachReadLoop(id, cs)

	// Issue the frontend's preferred size right after the handshake;
	// the daemon's WELCOME reports its current size which may differ.
	if cols > 0 && rows > 0 && (cols != welcome.Cols || rows != welcome.Rows) {
		_ = cs.WriteJSON(wire.FrameResize, wire.Resize{Cols: cols, Rows: rows})
	}

	return &AttachInfo{
		SessionID: id, Cols: welcome.Cols, Rows: welcome.Rows,
	}, nil
}

func (a *App) attachReadLoop(id string, cs *wire.Client) {
	defer func() {
		a.mu.Lock()
		if a.attaches[id] == cs {
			delete(a.attaches, id)
		}
		a.mu.Unlock()
		_ = cs.Close()
		wruntime.EventsEmit(a.ctx, "pty:disconnect", id)
	}()
	// Startup-flood probe: sum the FrameData bytes in the first second
	// after attach and log once. The initial subscribe replays the
	// session's scrollback ring (up to a few MB); many sessions doing
	// this at once floods pty:data events into the webview and can stall
	// its main thread. This quantifies the initial burst per session.
	loopStart := time.Now()
	var initBytes int
	var initFrames int
	initLogged := false
	logInitBurst := func() {
		if initLogged {
			return
		}
		initLogged = true
		log.Printf("hivegui[fe]: attach id=%s initial burst frames=%d bytes=%d in %dms",
			id, initFrames, initBytes, time.Since(loopStart).Milliseconds())
	}
	for {
		ft, payload, err := cs.ReadFrame()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("hivegui: attach %s read: %v", id, err)
			}
			return
		}
		if !initLogged {
			if ft == wire.FrameData {
				initFrames++
				initBytes += len(payload)
			}
			// Flush the burst summary once the initial replay settles
			// (1s quiet-ish window) or on the first non-data frame.
			if time.Since(loopStart) > time.Second || ft == wire.FrameEvent {
				logInitBurst()
			}
		}
		name, ok := wire.AttachEventName(ft)
		switch {
		case !ok:
			log.Printf("hivegui: attach %s unexpected frame %s", id, ft)
		case ft == wire.FrameData:
			wruntime.EventsEmit(a.ctx, name, id, base64.StdEncoding.EncodeToString(payload))
		default:
			wruntime.EventsEmit(a.ctx, name, id, string(payload))
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
	return cs.Close()
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
	return cs.WriteFrame(wire.FrameData, data)
}

// ResizeSession sends a RESIZE control frame on the attach connection.
func (a *App) ResizeSession(id string, cols, rows int) error {
	cs, err := a.attachFor(id)
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameResize, wire.Resize{Cols: cols, Rows: rows})
}

// RequestScrollbackReplay asks the daemon to re-stream the session's
// scrollback byte ring. The GUI uses this after a width-changing
// resize (single ↔ grid transitions) because xterm.js does not reflow
// scrollback when its column count changes — replaying the raw bytes
// into a freshly-reset terminal gets the history rendered at the new
// width. The daemon serializes the replay against live PTY fanout, so
// the client sees a clean Begin/bytes/Done sequence even under heavy
// streaming.
//
// Distinct from RenderSnapshot / SubscribeAtomicSnapshot — the bytes
// streamed back are the raw PTY ring, not the vt10x-synthesized
// repaint.
func (a *App) RequestScrollbackReplay(id string) error {
	cs, err := a.attachFor(id)
	if err != nil {
		return err
	}
	return cs.WriteFrame(wire.FrameRequestReplay, nil)
}

func (a *App) attachFor(id string) (*wire.Client, error) {
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
