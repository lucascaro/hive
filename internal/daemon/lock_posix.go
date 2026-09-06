//go:build !windows

package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrAlreadyRunning is what acquireStateLock returns when another hived
// already holds this state directory.
var ErrAlreadyRunning = errors.New("another hived is already running against this state directory")

// stateLockBudget is how long a starting daemon waits for the lock
// before giving up, and stateLockPoll how often it retries.
//
// Non-zero because of the restart handoff: the outgoing daemon closes
// its listeners first and releases this lock last, after draining its
// in-flight ops. The GUI reads the dead socket as "daemon gone" and
// spawns a replacement inside that gap — and dialOrSpawn spawns at most
// once per GUI process, so a replacement that exits on a held lock
// leaves the app with no daemon at all. Waiting a few seconds costs a
// slow start in the rare case; not waiting costs the restart.
//
// Vars, not consts, so the tests can shrink them.
var (
	stateLockBudget = 5 * time.Second
	stateLockPoll   = 50 * time.Millisecond
)

// acquireStateLock takes an exclusive, non-blocking flock on
// <stateDir>/hived.lock. The returned file must be kept open for the
// daemon's lifetime — closing it releases the lock — and closed on
// teardown.
//
// The socket file used to be the singleton guard: New dialled it and
// refused to start if anything answered. That only ever worked while
// the path was stable, and this change moves it, so across the upgrade
// an old daemon on /tmp/hive-<uid> and a new one on $TMPDIR/hive cannot
// see each other and would both revive every persisted session against
// one registry. The state directory is the thing there can only be one
// writer of, so lock that instead of the socket — it is what the guard
// was always reaching for.
func acquireStateLock(stateDir string) (*os.File, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(stateDir, "hived.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(stateLockBudget)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("%w: %s", ErrAlreadyRunning, stateDir)
		}
		time.Sleep(stateLockPoll)
	}
}
