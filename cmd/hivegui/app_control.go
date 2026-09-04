// App methods for the control connection: dialing and handshaking the
// daemon, the daemon-version banner, restart, and the control read
// loop that fans daemon events out to the frontend. Split out of
// app.go, which had grown past 1100 lines; see app.go for the App
// type itself.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	hdaemon "github.com/lucascaro/hive/internal/daemon"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/lucascaro/hive/internal/buildinfo"
	"github.com/lucascaro/hive/internal/wire"
)

// ----------------------------- control conn -----------------------------

// dialHandshake dials the daemon socket (spawning hived if needed) and
// performs the HELLO/WELCOME handshake via the shared wire client.
//
// budget bounds the wait for a daemon that is not up yet. It belongs
// to the caller, not to this function: the boot connect can afford to
// wait out a cold daemon, while OpenSession runs behind openMu and
// pays its budget once per session on a grid launch.
func (a *App) dialHandshake(hello wire.Hello, budget time.Duration) (*wire.Client, error) {
	conn, err := dialOrSpawn(hdaemon.SocketPath(), a.launchDir, budget)
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
	}, bootDialBudget)
	if err != nil {
		// A protocol rejection is the one connect failure the user can
		// actually fix, and it is invisible from the error alone: the
		// daemon refused the HELLO, so there is no WELCOME to derive a
		// banner from. Emit the mismatch severity by hand — contract 0
		// on the daemon side, because a daemon we could not handshake
		// with has told us nothing — so the banner offers "Restart
		// Daemon" instead of leaving a bare "could not connect".
		if errors.Is(err, wire.ErrProtocolMismatch) {
			a.emitDaemonVersionStatus("incompatible", "", 0)
		}
		return fmt.Errorf("control: %w", err)
	}

	w := cs.Welcome()
	a.mu.Lock()
	a.control = cs
	a.daemonContract = w.DaemonContract
	a.mu.Unlock()
	go a.controlReadLoop(cs)
	a.emitDaemonVersionStatus(w.BuildID, w.Release, w.DaemonContract)
	return nil
}

// DaemonStaleEvent is the payload of the "daemon:stale" Wails event.
//
// Severity is one of:
//
//	match       both sides are the same build — silent, but still
//	            emitted so the frontend can clear a stale banner.
//	reloadable  the builds differ but the daemon contracts agree, so
//	            the two are compatible. The frontend keeps this SILENT:
//	            reloading cannot change which build hived is, so a
//	            banner offering it would never clear. The sidebar
//	            footer shows both builds instead.
//	mismatch    the contracts differ (or the daemon advertised none),
//	            so the daemon itself must be restarted — which ends
//	            every session.
//	unknown     a build ID is missing; treated like mismatch.
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
	// The two contracts, so the footer and banner can say *why* a
	// restart is required rather than just asserting it. 0 on the
	// daemon side means it predates the field.
	GuiContract    int `json:"guiContract"`
	DaemonContract int `json:"daemonContract"`
}

// daemonVersionEvent builds the "daemon:stale" payload. Split out from
// the emit so it is testable without a live Wails context.
//
// Build IDs decide only whether anything changed at all; they are git
// revisions, so they differ after a CSS tweak. What decides the ACTION
// is the daemon contract: equal contracts mean a GUI-only reload is
// enough, and the user keeps every session. Comparing build IDs for
// that (which is what this did before contracts existed) charged a
// full restart — every PTY killed — for a frontend-only rebuild.
//
// A daemon advertising contract 0 predates the field entirely, so
// nothing is known about its behavior; that is a mismatch, never a
// match. daemonRelease is likewise empty on a daemon built before
// Welcome gained the Release field — consumers fall back to
// build-ID-only display.
func daemonVersionEvent(daemonBuild, daemonRelease string, daemonContract int) DaemonStaleEvent {
	gui := buildinfo.BuildID()
	ev := DaemonStaleEvent{
		GuiBuild:       gui,
		DaemonBuild:    daemonBuild,
		GuiRelease:     buildinfo.Version(),
		DaemonRelease:  daemonRelease,
		GuiContract:    buildinfo.DaemonContract,
		DaemonContract: daemonContract,
	}
	switch {
	case gui == "" || daemonBuild == "":
		ev.Severity = "unknown"
	case gui == daemonBuild:
		ev.Severity = "match"
	case daemonContract != 0 && daemonContract == buildinfo.DaemonContract:
		ev.Severity = "reloadable"
	default:
		ev.Severity = "mismatch"
	}
	return ev
}

// emitDaemonVersionStatus reports the GUI/daemon build relationship to the
// frontend. Both the stale-daemon banner and the sidebar version footer
// listen for this event.
func (a *App) emitDaemonVersionStatus(daemonBuild, daemonRelease string, daemonContract int) {
	wruntime.EventsEmit(a.ctx, "daemon:stale",
		daemonVersionEvent(daemonBuild, daemonRelease, daemonContract))
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

	// The daemon is gone; these conns are dead sockets now.
	return a.relaunchSelf("restart")
}

// relaunchSelf replaces this GUI process with a fresh one and quits.
// It is the tail RestartDaemon and ReloadGUI share: release the
// connections so the outgoing process isn't holding half-open fds
// while the new GUI comes up, spawn the replacement, then quit.
//
// It says nothing about the daemon, deliberately. RestartDaemon has
// already confirmed hived is gone by the time it gets here; ReloadGUI
// requires it to still be running. Keeping the "is the daemon alive"
// decision entirely with the callers is what makes ReloadGUI provably
// unable to kill a session.
//
// why is a log tag naming which caller this is, so the daemon-restart
// and GUI-reload paths stay distinguishable in hivegui.log.
// Seams for the reload/restart tests, for the same reason
// restartDaemonFn is seamed in update_action.go and more urgently:
// spawnNewGUI re-execs os.Executable(), which in a test binary is the
// test binary — a test that reached it once spawned a detached copy of
// the whole suite, against the developer's real state directory.
var (
	spawnNewGUIFn = spawnNewGUI
	quitFn        = func(a *App) { wruntime.Quit(a.ctx) }
	// emitFn is seamed for a different reason than the two above:
	// EventsEmit rejects a context it did not issue, so any test that
	// drives a code path ending in an emit aborts the whole binary
	// rather than failing one case.
	emitFn = func(a *App, name string, data ...any) {
		wruntime.EventsEmit(a.ctx, name, data...)
	}
)

func (a *App) relaunchSelf(why string) error {
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

	if err := spawnNewGUIFn(a.launchDir); err != nil {
		return fmt.Errorf("relaunch GUI: %w", err)
	}
	log.Printf("hivegui: %s: relaunched, quitting", why)
	quitFn(a)
	return nil
}

// ReloadGUI relaunches this GUI process and leaves hived — and every
// PTY it owns — running.
//
// This is the cheap half of what used to be a single "Restart Hive".
// hived is spawned detached and has no idle exit, so it outlives a GUI
// quit on its own; picking up new frontend code never needed to kill
// the user's sessions, and doing so was costing a full restart for
// what is usually a CSS change.
//
// It must never send FrameShutdown and never call killRunningHived.
// Those are RestartDaemon's, and a reload that reaches either of them
// is a reload that ended someone's agent run. TestReloadGUINeverKills
// pins that.
//
// Callers reach this through RequestReloadAllGUIs so every window
// relaunches together; calling it directly reloads only this one and
// leaves siblings on the old binary.
func (a *App) ReloadGUI() error {
	if !a.beginReload() {
		// A reload is already in flight. The broadcast reaches every
		// window including the sender, and a user can click twice, so
		// this is reachable in normal use: spawning a second
		// replacement would leave the user with two new windows.
		log.Printf("hivegui: reload: already in flight, ignoring")
		return nil
	}
	log.Printf("hivegui: reload: relaunching GUI, leaving hived alone")
	return a.relaunchSelf("reload")
}

// beginReload latches the reload flag, returning false if one is
// already running.
func (a *App) beginReload() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.reloading {
		return false
	}
	a.reloading = true
	return true
}

// RequestReloadAllGUIs asks the daemon to tell every GUI window to
// relaunch, this one included.
//
// It goes through the daemon rather than reloading in place because
// each window is its own process (OpenNewWindow re-execs), so a local
// reload would leave the other windows running the old binary against
// the new daemon — the exact drift this feature exists to remove.
func (a *App) RequestReloadAllGUIs() error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	if err := cs.WriteJSON(wire.FrameClientCommand,
		wire.ClientCommand{Cmd: wire.CmdReloadGUI}); err != nil {
		return fmt.Errorf("request reload: %w", err)
	}
	return nil
}

// handleClientCommand acts on a command another client (or this one)
// asked the daemon to relay.
//
// reload_gui is handled here in Go rather than forwarded to the
// frontend: the reload is a process relaunch, which the webview cannot
// perform, and routing it through JS would put a page that is about to
// be destroyed in charge of destroying itself. Everything else is
// frontend business and goes out as a Wails event.
func (a *App) handleClientCommand(payload []byte) {
	var cmd wire.ClientCommand
	if err := json.Unmarshal(payload, &cmd); err != nil {
		log.Printf("hivegui: control: bad client command payload: %v", err)
		return
	}
	switch cmd.Cmd {
	case wire.CmdReloadGUI:
		if err := a.ReloadGUI(); err != nil {
			log.Printf("hivegui: reload: %v", err)
		}
	default:
		emitFn(a, "client:command", string(payload))
	}
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
		if ft == wire.FrameClientBroadcast {
			a.handleClientCommand(payload)
			continue
		}
		if name, ok := wire.ControlEventName(ft); ok {
			wruntime.EventsEmit(a.ctx, name, string(payload))
		} else {
			log.Printf("hivegui: control unexpected frame %s", ft)
		}
	}
}

// AgentInfo is the JSON shape the frontend uses to render the launcher.
