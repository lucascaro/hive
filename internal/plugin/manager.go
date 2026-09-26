package plugin

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lucascaro/hive/internal/proc"
	"github.com/lucascaro/hive/internal/wire"
)

// Supervision policy. Vars so tests can shrink them.
var (
	// crashWindow / maxCrashes: more than maxCrashes exits inside
	// crashWindow marks the plugin failed and stops restarting it.
	crashWindow = 60 * time.Second
	maxCrashes  = 5
	// backoffMin / backoffMax bound the wait before a restart; it
	// doubles on each consecutive crash.
	backoffMin = time.Second
	backoffMax = 30 * time.Second
	// termGrace is how long a stopping plugin gets between SIGTERM and
	// SIGKILL.
	termGrace = 3 * time.Second
)

// ErrManagerStopped is returned by every mutating call after Stop. Stop
// is terminal: nothing may spawn once the daemon is shutting down.
var ErrManagerStopped = errors.New("plugin: manager stopped")

// ErrNotFound is returned for an id that is not installed.
var ErrNotFound = errors.New("plugin: not installed")

// ListenFunc opens a fresh socket for one run of plugin id and returns
// its path, which the plugin receives as HIVE_SOCKET. The daemon tags
// every connection accepted on it with id. closeFn closes the listener
// and removes the socket file; the Manager calls it when the run ends,
// so a restart always gets a new path and an orphan of an earlier run
// cannot reconnect.
type ListenFunc func(id string) (path string, closeFn func(), err error)

// Config configures a Manager.
type Config struct {
	// StateDir is where plugins.json, plugins/ and plugin-data/ live.
	StateDir string
	// Listen opens a per-run plugin socket. Required.
	Listen ListenFunc
}

// Manager owns the installed plugin set and one supervising goroutine
// per enabled plugin. With no plugin enabled it runs nothing at all.
type Manager struct {
	stateDir string
	listen   ListenFunc

	mu        sync.Mutex
	started   bool
	stopped   bool
	plugins   map[string]*entry
	order     []string // install order, for a stable List
	listeners map[chan wire.PluginEvent]struct{}
	runners   sync.WaitGroup
}

type entry struct {
	rec      record
	man      Manifest
	manErr   error // manifest problem found at load; the plugin is refused
	status   string
	detail   string
	restarts int
	run      *runner // non-nil while a supervising goroutine exists
	// op serializes SetEnabled and Remove on this plugin. Both drop m.mu
	// while they wait for a runner to stop, and without op a second call
	// could slip into that window: an enable that starts a runner Remove
	// is about to orphan, or an enable that sees the stopping runner,
	// starts nothing, and leaves the plugin enabled but stopped. Held
	// outside m.mu (op before mu, never the other way round).
	op sync.Mutex
	// removed is set, under m.mu, once Remove has taken the entry out of
	// the map; an operation that looked the entry up before then sees it
	// and gives up.
	removed bool
}

type runner struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// signal asks the supervisor to stop. Idempotent: Stop, SetEnabled and
// Remove can all reach the same runner during shutdown.
func (r *runner) signal() { r.once.Do(func() { close(r.stop) }) }

// New loads the installed set from cfg.StateDir. It starts nothing:
// Start does that once the daemon is accepting connections.
func New(cfg Config) (*Manager, error) {
	if cfg.Listen == nil {
		return nil, errors.New("plugin: Config.Listen is required")
	}
	recs, err := loadStore(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	m := &Manager{
		stateDir:  cfg.StateDir,
		listen:    cfg.Listen,
		plugins:   map[string]*entry{},
		listeners: map[chan wire.PluginEvent]struct{}{},
	}
	for _, r := range recs {
		e := &entry{rec: r, status: wire.PluginStopped}
		e.man, e.manErr = LoadManifest(m.installPath(r.ID))
		if e.manErr != nil {
			e.status, e.detail = wire.PluginRefused, e.manErr.Error()
		}
		m.plugins[r.ID] = e
		m.order = append(m.order, r.ID)
	}
	return m, nil
}

func (m *Manager) installPath(id string) string {
	return filepath.Join(m.stateDir, installDir, id)
}

// DataPath is the plugin's own data directory, handed to it as
// HIVE_PLUGIN_DATA_DIR. It survives remove and reinstall.
func (m *Manager) DataPath(id string) string {
	return filepath.Join(m.stateDir, dataDir, id)
}

// Start launches a runner for every enabled plugin. Later enables start
// their own runner. Calling Start after Stop does nothing.
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped || m.started {
		return
	}
	m.started = true
	for _, id := range m.order {
		if e := m.plugins[id]; e.rec.Enabled {
			m.startLocked(e)
		}
	}
}

// Stop kills every running plugin and waits for its supervisor to exit.
// It is terminal: afterwards Install and SetEnabled return
// ErrManagerStopped and nothing is ever spawned again.
func (m *Manager) Stop() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	for _, e := range m.plugins {
		if e.run != nil {
			e.run.signal()
		}
	}
	for ch := range m.listeners {
		close(ch)
	}
	m.listeners = map[chan wire.PluginEvent]struct{}{}
	m.mu.Unlock()
	m.runners.Wait()
}

// RunnerCount reports how many plugin supervisors exist. Zero whenever
// no plugin is enabled — the Manager's whole idle cost.
func (m *Manager) RunnerCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range m.plugins {
		if e.run != nil {
			n++
		}
	}
	return n
}

// List reports every installed plugin in install order.
func (m *Manager) List() []wire.PluginInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]wire.PluginInfo, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.infoLocked(m.plugins[id]))
	}
	return out
}

func (m *Manager) infoLocked(e *entry) wire.PluginInfo {
	info := wire.PluginInfo{
		ID:           e.rec.ID,
		Name:         e.man.Name,
		Version:      e.man.Version,
		APIVersion:   e.man.APIVersion,
		Description:  e.man.Description,
		Source:       e.rec.Source,
		Commit:       e.rec.Commit,
		Enabled:      e.rec.Enabled,
		Status:       e.status,
		StatusDetail: e.detail,
		Restarts:     e.restarts,
		Command:      []string{},
	}
	if e.man.Main != nil {
		info.Command = e.man.Main.Command
	}
	if info.Name == "" {
		info.Name = e.rec.ID
	}
	return info
}

// Subscribe returns a channel that receives every PluginEvent, and a
// cleanup that unsubscribes. A consumer that falls behind is dropped and
// its channel closed, the same policy as the registry's listeners.
func (m *Manager) Subscribe() (chan wire.PluginEvent, func()) {
	ch := make(chan wire.PluginEvent, 32)
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	m.listeners[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		if _, ok := m.listeners[ch]; ok {
			delete(m.listeners, ch)
			close(ch)
		}
		m.mu.Unlock()
	}
}

func (m *Manager) emitLocked(kind string, e *entry, nonce string) {
	ev := wire.PluginEvent{Kind: kind, Plugin: m.infoLocked(e), Nonce: nonce}
	for ch := range m.listeners {
		select {
		case ch <- ev:
		default:
			log.Printf("plugin: dropping slow plugin-event listener (buffer %d full); closing it so the daemon hangs up on that client", cap(ch))
			delete(m.listeners, ch)
			close(ch)
		}
	}
}

func (m *Manager) saveLocked() error {
	recs := make([]record, 0, len(m.order))
	for _, id := range m.order {
		recs = append(recs, m.plugins[id].rec)
	}
	return saveStore(m.stateDir, recs)
}

// Install fetches source — an absolute directory path or a git URL —
// validates its manifest, and stores it disabled. Nothing runs until
// SetEnabled: the caller shows the user what they are about to trust
// first. A manifest for another API version still installs, as
// "refused", so the user can see why it will not run. nonce is echoed
// on the "added" event.
func (m *Manager) Install(ctx context.Context, source, nonce string) (wire.PluginInfo, error) {
	source = strings.TrimSpace(source)
	if m.isStopped() {
		return wire.PluginInfo{}, ErrManagerStopped
	}
	root := filepath.Join(m.stateDir, installDir)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return wire.PluginInfo{}, err
	}
	stage, err := os.MkdirTemp(root, ".staging-")
	if err != nil {
		return wire.PluginInfo{}, err
	}
	defer os.RemoveAll(stage)
	src := filepath.Join(stage, "src")

	var commit string
	isURL, err := isGitURL(source)
	switch {
	case err != nil:
		return wire.PluginInfo{}, err
	case isURL:
		if commit, err = gitClone(ctx, source, src); err != nil {
			return wire.PluginInfo{}, err
		}
	default:
		local, err := resolveLocal(source)
		if err != nil {
			return wire.PluginInfo{}, err
		}
		if st, err := os.Stat(local); err != nil || !st.IsDir() {
			return wire.PluginInfo{}, fmt.Errorf("plugin: %s is not a directory", local)
		}
		if err := copyTree(local, src); err != nil {
			return wire.PluginInfo{}, fmt.Errorf("plugin: copy %s: %w", local, err)
		}
		source = local
	}

	man, manErr := LoadManifest(src)
	if manErr != nil && !errors.Is(manErr, ErrAPIVersion) {
		return wire.PluginInfo{}, manErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return wire.PluginInfo{}, ErrManagerStopped
	}
	if _, dup := m.plugins[man.ID]; dup {
		return wire.PluginInfo{}, fmt.Errorf("plugin: %q is %w", man.ID, errDuplicate)
	}
	dst := m.installPath(man.ID)
	_ = os.RemoveAll(dst) // an uninstalled leftover, never a live plugin
	if err := os.Rename(src, dst); err != nil {
		return wire.PluginInfo{}, err
	}
	e := &entry{
		rec:    record{ID: man.ID, Source: source, Commit: commit},
		man:    man,
		manErr: manErr,
		status: wire.PluginStopped,
	}
	if manErr != nil {
		e.status, e.detail = wire.PluginRefused, manErr.Error()
	}
	m.plugins[man.ID] = e
	m.order = append(m.order, man.ID)
	if err := m.saveLocked(); err != nil {
		delete(m.plugins, man.ID)
		m.order = m.order[:len(m.order)-1]
		_ = os.RemoveAll(dst)
		return wire.PluginInfo{}, err
	}
	m.emitLocked(wire.PluginEventAdded, e, nonce)
	return m.infoLocked(e), nil
}

// SetEnabled persists the flag and starts or stops the plugin. Enabling
// a plugin that is already enabled but failed or refused starts it
// afresh, with its restart count reset.
func (m *Manager) SetEnabled(id string, enabled bool) (wire.PluginInfo, error) {
	e, err := m.lockOp(id)
	if err != nil {
		return wire.PluginInfo{}, err
	}
	defer e.op.Unlock()

	m.mu.Lock()
	e.rec.Enabled = enabled
	if err := m.saveLocked(); err != nil {
		e.rec.Enabled = !enabled
		m.mu.Unlock()
		return wire.PluginInfo{}, err
	}
	if enabled {
		if e.run == nil && m.started {
			e.restarts = 0
			m.startLocked(e)
		}
		m.emitLocked(wire.PluginEventUpdated, e, "")
		info := m.infoLocked(e)
		m.mu.Unlock()
		return info, nil
	}
	r := e.run
	m.mu.Unlock()
	m.stopRunner(r)
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.manErr == nil {
		e.status, e.detail = wire.PluginStopped, ""
	}
	m.emitLocked(wire.PluginEventUpdated, e, "")
	return m.infoLocked(e), nil
}

// lockOp looks id up and takes its op lock, then re-checks that the
// manager is still running and the plugin still installed — either can
// change while a caller waits for op. The caller unlocks e.op.
func (m *Manager) lockOp(id string) (*entry, error) {
	m.mu.Lock()
	e, ok := m.plugins[id]
	m.mu.Unlock()
	if !ok {
		return nil, ErrNotFound
	}
	e.op.Lock()
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case m.stopped:
		e.op.Unlock()
		return nil, ErrManagerStopped
	case e.removed:
		e.op.Unlock()
		return nil, ErrNotFound
	}
	return e, nil
}

// Remove stops the plugin and deletes its installed copy. Its data dir
// (config, log) is kept, so a reinstall picks its configuration back up.
func (m *Manager) Remove(id string) error {
	// After Stop this is refused like every other mutation: the daemon
	// is going away, and the plugin simply stays installed.
	e, err := m.lockOp(id)
	if err != nil {
		return err
	}
	defer e.op.Unlock()

	m.mu.Lock()
	r := e.run
	m.mu.Unlock()
	m.stopRunner(r)

	m.mu.Lock()
	defer m.mu.Unlock()
	if e.removed {
		return ErrNotFound
	}
	// Persist first: if plugins.json cannot be written the plugin stays
	// installed — in memory and on disk alike — rather than vanishing
	// from the list only to come back on the next daemon start.
	idx := -1
	for i, oid := range m.order {
		if oid == id {
			idx = i
			break
		}
	}
	m.order = append(m.order[:idx], m.order[idx+1:]...)
	delete(m.plugins, id)
	if err := m.saveLocked(); err != nil {
		m.plugins[id] = e
		m.order = append(m.order[:idx], append([]string{id}, m.order[idx:]...)...)
		if e.manErr == nil {
			e.status, e.detail = wire.PluginStopped, ""
		}
		m.emitLocked(wire.PluginEventUpdated, e, "")
		return err
	}
	e.removed = true
	if err := os.RemoveAll(m.installPath(id)); err != nil {
		log.Printf("plugin %s: remove install dir: %v", id, err)
	}
	e.status, e.detail = wire.PluginStopped, ""
	m.emitLocked(wire.PluginEventRemoved, e, "")
	return nil
}

func (m *Manager) isStopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped
}

// stopRunner signals r to stop and waits for its supervisor to exit.
// Must be called without m.mu held: the supervisor takes it to report.
func (m *Manager) stopRunner(r *runner) {
	if r == nil {
		return
	}
	r.signal()
	<-r.done
}

func (m *Manager) startLocked(e *entry) {
	r := &runner{stop: make(chan struct{}), done: make(chan struct{})}
	e.run = r
	m.runners.Add(1)
	go m.supervise(e, r)
}

// setStatus records a status change from the supervisor and fans it out.
func (m *Manager) setStatus(e *entry, status, detail string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e.status, e.detail = status, detail
	if m.plugins[e.rec.ID] == e {
		m.emitLocked(wire.PluginEventUpdated, e, "")
	}
}

// supervise runs one plugin until it is stopped, refused, or fails.
func (m *Manager) supervise(e *entry, r *runner) {
	defer m.runners.Done()
	defer close(r.done)
	defer func() {
		m.mu.Lock()
		if e.run == r {
			e.run = nil
		}
		m.mu.Unlock()
	}()

	var crashes []time.Time
	backoff := backoffMin
	for {
		cmd, closeListener, err := m.spawn(e)
		if err != nil {
			m.setStatus(e, wire.PluginRefused, err.Error())
			return
		}
		m.setStatus(e, wire.PluginRunning, "")

		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		var exitErr error
		select {
		case <-r.stop:
			termTree(cmd.Process)
			select {
			case <-exited:
			case <-time.After(termGrace):
				_ = killTree(cmd.Process)
				<-exited
			}
			_ = killTree(cmd.Process) // reap anything left in the group
			closeListener()
			return
		case exitErr = <-exited:
			_ = killTree(cmd.Process)
			closeListener()
		}

		now := time.Now()
		kept := crashes[:0]
		for _, t := range crashes {
			if now.Sub(t) < crashWindow {
				kept = append(kept, t)
			}
		}
		crashes = append(kept, now)
		reason := "exited"
		if exitErr != nil {
			reason = exitErr.Error()
		}
		m.mu.Lock()
		e.restarts++
		m.mu.Unlock()
		if len(crashes) > maxCrashes {
			m.setStatus(e, wire.PluginFailed, fmt.Sprintf("stopped after %d exits within %s (last: %s); see %s",
				len(crashes), crashWindow, reason, filepath.Join(m.DataPath(e.rec.ID), logFile)))
			return
		}
		m.setStatus(e, wire.PluginCrashed, reason+"; restarting")
		select {
		case <-r.stop:
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, backoffMax)
	}
}

// spawn starts one run of e: re-reads the manifest (a refused plugin
// never gets here with a stale one), resolves the command, opens the
// run's socket and starts the process. The error is a refusal — a
// problem restarting cannot fix.
func (m *Manager) spawn(e *entry) (*exec.Cmd, func(), error) {
	dir := m.installPath(e.rec.ID)
	man, err := LoadManifest(dir)
	if err != nil {
		return nil, nil, err
	}
	argv := man.Main.Command
	bin, err := resolveCommand(dir, argv[0])
	if err != nil {
		return nil, nil, err
	}
	data := m.DataPath(e.rec.ID)
	if err := os.MkdirAll(data, 0o700); err != nil {
		return nil, nil, err
	}
	logw, err := openCappedLog(filepath.Join(data, logFile))
	if err != nil {
		return nil, nil, err
	}
	sock, closeListener, err := m.listen(e.rec.ID)
	if err != nil {
		logw.Close()
		return nil, nil, fmt.Errorf("plugin socket: %w", err)
	}
	cmd := proc.Command(bin, argv[1:]...)
	cmd.Dir = dir
	cmd.Env = pluginEnv(os.Environ(),
		"HIVE_SOCKET="+sock,
		"HIVE_PLUGIN_ID="+e.rec.ID,
		"HIVE_PLUGIN_API="+APIVersion,
		"HIVE_PLUGIN_DIR="+dir,
		"HIVE_PLUGIN_DATA_DIR="+data,
	)
	cmd.Stdout, cmd.Stderr = logw, logw
	// A grandchild that keeps the output pipe open must not pin Wait.
	cmd.WaitDelay = termGrace
	ownGroup(cmd)
	if err := cmd.Start(); err != nil {
		closeListener()
		logw.Close()
		return nil, nil, err
	}
	return cmd, func() { closeListener(); logw.Close() }, nil
}

// resolveCommand finds argv[0]: "./x" is relative to the plugin dir,
// anything else is looked up on the daemon's PATH. A missing command is
// reported with the PATH searched, since a daemon started outside the
// GUI may not have the login shell's PATH.
func resolveCommand(dir, name string) (string, error) {
	if rest, ok := strings.CutPrefix(name, "./"); ok {
		p := filepath.Join(dir, filepath.FromSlash(rest))
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("command %s not found in the plugin", name)
		}
		return p, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s not found on PATH=%s", name, os.Getenv("PATH"))
	}
	return p, nil
}

// pluginEnv is the daemon's environment with Hive's own session and
// plugin variables replaced by the plugin's.
func pluginEnv(base []string, add ...string) []string {
	out := make([]string, 0, len(base)+len(add))
	for _, kv := range base {
		if strings.HasPrefix(kv, "HIVE_SOCKET=") || strings.HasPrefix(kv, "HIVE_SESSION_ID=") ||
			strings.HasPrefix(kv, "HIVE_PLUGIN_") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, add...)
}
