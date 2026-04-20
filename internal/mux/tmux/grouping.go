package muxtmux

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"sync"

	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/tmux"
)

// CanonicalSession is the authoritative tmux session that owns the shared
// window list. Every hive instance reads its windows from this session.
const CanonicalSession = mux.HiveSession

// groupedSessionPattern matches per-instance grouped sessions:
// "hive-sessions-<pid>-<4 hex chars>".
var groupedSessionPattern = regexp.MustCompile(`^hive-sessions-(\d+)-[0-9a-f]{4}$`)

// InstanceSession holds the grouped-session name for this process. Empty when
// the backend has not initialised an instance (e.g. canonical-only test mode),
// in which case attach falls back to the canonical session.
var (
	instanceMu   sync.RWMutex
	instanceName string
)

// InstanceSession returns the active grouped-session name for this process,
// or an empty string if not initialised.
func (b *Backend) InstanceSession() string {
	instanceMu.RLock()
	defer instanceMu.RUnlock()
	return instanceName
}

// InitInstance creates a per-process tmux session that shares its window list
// with the canonical hive session via `new-session -t`. Each instance gets its
// own current-window selection so attaching in one hive does not mirror the
// view in another.
//
// The canonical session is created first if it does not exist. The returned
// error is non-nil only if tmux refuses to create either session.
func (b *Backend) InitInstance() error {
	if err := ensureCanonical(); err != nil {
		return fmt.Errorf("ensure canonical session: %w", err)
	}
	name := newInstanceName(os.Getpid())
	if err := tmux.ExecSilent(
		"new-session", "-d",
		"-t", CanonicalSession,
		"-s", name,
	); err != nil {
		return fmt.Errorf("create grouped session %q: %w", name, err)
	}
	instanceMu.Lock()
	instanceName = name
	instanceMu.Unlock()
	return nil
}

// ShutdownInstance kills this process's grouped session. Safe to call
// multiple times and when InitInstance was never called (no-op).
func (b *Backend) ShutdownInstance() error {
	instanceMu.Lock()
	name := instanceName
	instanceName = ""
	instanceMu.Unlock()
	if name == "" {
		return nil
	}
	// Ignore "session not found" errors — the server or session may already
	// have been torn down.
	_ = tmux.KillSession(name)
	return nil
}

// SweepOrphanInstances enumerates grouped hive sessions and kills any whose
// owning pid is no longer alive. Called at startup before InitInstance to
// reclaim sessions left behind by crashed hive processes.
func (b *Backend) SweepOrphanInstances() error {
	names, err := tmux.ListSessionNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		pid, ok := parseInstancePID(name)
		if !ok {
			continue
		}
		if pidAlive(pid) {
			continue
		}
		_ = tmux.KillSession(name)
	}
	return nil
}

// CanonicalExists reports whether the canonical hive session is still alive.
// Used by the canonical-gone watcher to detect tmux-level teardown (server
// restart, external kill-session) so the TUI can exit cleanly rather than
// drift into a state where its grouped session points at nothing.
func (b *Backend) CanonicalExists() bool {
	return tmux.SessionExists(CanonicalSession)
}

// ensureCanonical creates the canonical session if absent. new-session -A is
// idempotent: if the session already exists, tmux attaches in-place
// (harmless because -d keeps it detached and we discard the result).
func ensureCanonical() error {
	if tmux.SessionExists(CanonicalSession) {
		return nil
	}
	// Create an empty holder session. The first window will be created by
	// the usual CreateSession path when the user creates their first session.
	// Using `-A` (attach-if-exists) makes concurrent startup benign.
	return tmux.ExecSilent(
		"new-session", "-A", "-d",
		"-s", CanonicalSession,
	)
}

// newInstanceName returns "hive-sessions-<pid>-<4 hex chars>".
func newInstanceName(pid int) string {
	var buf [2]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fall back to pid only. The regex still matches (parseInstancePID
		// uses 4 hex chars), so downstream code keeps working; callers that
		// hit this path just lose the pid-reuse disambiguation bit.
		return fmt.Sprintf("hive-sessions-%d-0000", pid)
	}
	return fmt.Sprintf("hive-sessions-%d-%s", pid, hex.EncodeToString(buf[:]))
}

// parseInstancePID returns the pid embedded in a grouped session name, or
// ok=false if name is not a grouped session (e.g. the canonical session,
// unrelated tmux sessions).
func parseInstancePID(name string) (int, bool) {
	m := groupedSessionPattern.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	var pid int
	if _, err := fmt.Sscanf(m[1], "%d", &pid); err != nil {
		return 0, false
	}
	return pid, true
}

// pidAlive reports whether a process with the given pid is currently running.
// Platform-specific implementations live in pidalive_unix.go and
// pidalive_windows.go.
