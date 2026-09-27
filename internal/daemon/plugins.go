package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/lucascaro/hive/internal/plugin"
	"github.com/lucascaro/hive/internal/wire"
)

// pluginTag marks a connection accepted on a plugin's own socket. The
// daemon knows it is a plugin by WHICH socket it dialed, never by what
// its HELLO claims: a plugin written without the SDK, or any process it
// spawns (they inherit HIVE_SOCKET), is tagged exactly the same. A
// tagged connection is served by the ordinary dispatch — plugins get
// every verb a GUI does — and differs in two ways only: it is never
// counted as a client that can answer a dialog (canAnswer), and it
// spends from its plugin's rate budget.
type pluginTag struct {
	id  string
	lim *plugin.Limiter
	// ctx ends when the daemon starts shutting down or this plugin run
	// ends, releasing any connection sleeping in wait.
	ctx context.Context
	// pressure reports how full the fullest client event listener is
	// (Daemon.fanoutPressure). nil in tests that exercise the budget alone.
	pressure func() float64
}

// Backpressure on a plugin's broadcasts (#467). Every broadcast lands in
// every client's event listener, and a listener that fills is dropped
// and its client hung up on. A plugin paced only by its token budget can
// out-run a GUI that is briefly off-CPU; one whose broadcasting requests
// wait while any listener is half full cannot, whatever the frame fans
// out to, however many plugins are sending, and whichever buffer is the
// small one. The wait is bounded: a client that has stopped reading
// keeps its listener full, and after backpressureMax the plugin goes
// ahead and that client is dropped by the ordinary slow-listener rule
// (#461) rather than stalling every plugin forever.
const (
	backpressureHigh = 0.5
	// ponytail: polls, because the listeners have no drain signal to wait
	// on; 5ms is ~200 checks/s per throttled plugin, only while throttled.
	backpressurePoll = 5 * time.Millisecond
)

// backpressureMax is a var so tests can shorten it.
var backpressureMax = 2 * time.Second

// wait spends cost from the plugin's budget, sleeping when it is spent.
// It gives up — and the caller drops the connection — when the daemon
// shuts down or the run ends, so a throttled plugin never pins Close.
// A nil tag (an ordinary client) never waits.
func (t *pluginTag) wait(cost float64) error {
	if t == nil {
		return nil
	}
	if err := t.lim.Wait(t.ctx, cost); err != nil {
		return err
	}
	// Cost-1 frames are reads (LIST_*, GET_*, SEARCH_*) and attach
	// input; they broadcast nothing, so they never wait on readers.
	if cost <= 1 || t.pressure == nil {
		return nil
	}
	deadline := time.Now().Add(backpressureMax)
	for t.pressure() >= backpressureHigh && time.Now().Before(deadline) {
		select {
		case <-t.ctx.Done():
			return t.ctx.Err()
		case <-time.After(backpressurePoll):
		}
	}
	return nil
}

// listenPlugin opens a fresh socket for one run of plugin id, next to
// the control socket (so it inherits that directory's 0700 check and
// fits the AF_UNIX path limit the control path was sized for). Every
// connection accepted on it is tagged with id and shares one Limiter.
// The Manager calls closeFn when the run ends; the random suffix means
// a restarted plugin never gets the same path, so a process orphaned
// from an earlier run — or an earlier daemon — cannot reconnect.
func (d *Daemon) listenPlugin(id string) (string, func(), error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", nil, err
	}
	path := d.sock + pluginSockInfix + hex.EncodeToString(b[:])
	// The control socket's path was sized for its own ".events" sibling,
	// and a plugin socket's suffix is longer. Refuse up front with the
	// reason, rather than surfacing bind's "invalid argument".
	if limit := maxSockPath(); len(path) >= limit {
		return "", nil, fmt.Errorf("plugin socket path %s is %d bytes; the OS limit is %d — set HIVE_SOCKET to a shorter path", path, len(path), limit-1)
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return "", nil, err
	}
	ctx, cancel := d.stopCtx(context.Background())
	tag := &pluginTag{id: id, lim: plugin.NewLimiter(), ctx: ctx, pressure: d.fanoutPressure}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					log.Printf("hived: plugin %s accept: %v", id, err)
				}
				return
			}
			go d.serveConn(d.runCtx, conn, tag)
		}
	}()
	return path, func() {
		cancel()
		_ = ln.Close()
		_ = os.Remove(path)
	}, nil
}

// pluginSockInfix names per-run plugin sockets: <control sock>.plugin-<8 hex>.
// Distinct enough that the boot sweep of stale ones can never match a
// sibling file such as the daemon's <sock>.pid.
const pluginSockInfix = ".plugin-"

// maxSockPath is sizeof(sun_path): the longest AF_UNIX path, including
// its terminating NUL, the platform accepts.
func maxSockPath() int {
	switch runtime.GOOS {
	case "darwin", "freebsd", "openbsd", "netbsd":
		return 104
	default: // linux, windows
		return 108
	}
}

// pluginModeAllowed is what a plugin socket serves: the modes an
// out-of-process client uses. The in-session modes (event, session,
// plan_review) belong to agents and are refused.
func pluginModeAllowed(m wire.Mode) bool {
	return m == wire.ModeControl || m == wire.ModeAttach || m == wire.ModeCreate
}

// stopCtx returns a context cancelled when the daemon starts shutting
// down, so a slow plugin install (a git clone) is cut short by Close
// rather than waited out.
func (d *Daemon) stopCtx(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		select {
		case <-d.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// handlePluginFrame serves the four plugin-management verbs. Install,
// enable/disable and remove can block (a clone, a plugin's grace period
// on stop), so they run as ops off the read loop; their result reaches
// every client as a PLUGIN_EVENT from the Manager.
func (d *Daemon) handlePluginFrame(ctx context.Context, ops controlOps, ft wire.FrameType, payload []byte) {
	if d.plugins == nil {
		ops.sendError("plugins_unavailable", "plugin support failed to load; see the daemon log")
		return
	}
	switch ft {
	case wire.FrameListPlugins:
		_ = ops.writeJSON(wire.FramePlugins, wire.PluginsResp{Plugins: d.plugins.List()})
	case wire.FrameInstallPlugin:
		req, ok := decodeReq[wire.InstallPluginReq](payload, ops.sendError)
		if !ok {
			return
		}
		d.runOp(func() {
			ictx, cancel := d.stopCtx(ctx)
			defer cancel()
			if _, err := d.plugins.Install(ictx, req.Source, req.Nonce); err != nil {
				_ = ops.writeJSON(wire.FrameError, wire.Error{
					Code: wire.ErrCodePluginInstallFailed, Message: err.Error(), Nonce: req.Nonce,
				})
			}
		})
	case wire.FrameSetPluginEnabled:
		req, ok := decodeReq[wire.SetPluginEnabledReq](payload, ops.sendError)
		if !ok {
			return
		}
		d.runOp(func() {
			if _, err := d.plugins.SetEnabled(req.ID, req.Enabled); err != nil {
				ops.sendError(pluginErrorCode(err, "set_plugin_enabled_failed"), err.Error())
			}
		})
	case wire.FrameRemovePlugin:
		req, ok := decodeReq[wire.RemovePluginReq](payload, ops.sendError)
		if !ok {
			return
		}
		d.runOp(func() {
			if err := d.plugins.Remove(req.ID); err != nil {
				ops.sendError(pluginErrorCode(err, "remove_plugin_failed"), err.Error())
			}
		})
	}
}

func pluginErrorCode(err error, generic string) string {
	if errors.Is(err, plugin.ErrNotFound) {
		return wire.ErrCodePluginNotFound
	}
	return generic
}

// fanoutPressure is how full the fullest client event listener is,
// across every kind the daemon fans out: registry session, project,
// idea and activity events, PLUGIN_EVENT, and client commands.
func (d *Daemon) fanoutPressure() float64 {
	p := max(d.reg.Pressure(), d.commands.pressure())
	if d.plugins != nil {
		p = max(p, d.plugins.Pressure())
	}
	return p
}
