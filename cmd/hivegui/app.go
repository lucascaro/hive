package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/atotto/clipboard"
	"github.com/lucascaro/hive/internal/activity"
	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/notify"
	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
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

	// update carries the update-check result and any staged build, so
	// the Update/Restart button renders the same state in the banner
	// and in Settings. See update_action.go.
	update updateState

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
