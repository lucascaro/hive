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

// acquireStateLock takes an exclusive, non-blocking flock on
// <stateDir>/hived.lock. The returned file must be kept open for the
// daemon's lifetime — closing it releases the lock — and closed on
// teardown.
//
// The socket file used to be the singleton guard: New dialled it and
// refused to start if anything answered. That is a proxy for the thing
// that actually has to be single — there can only be one writer of the
// state directory — and it only holds while the socket path is stable.
// This change moves the path, so the proxy stopped being reliable at
// exactly the moment two daemons became easier to get. Lock the state
// directory instead; it is what the guard was always reaching for.
//
// Note what this does NOT cover: the upgrade itself. A daemon built
// before this takes no lock at all, so a new one starting beside a
// still-running old one acquires uncontested and both run. Closing
// that would mean probing the old default path — migration cruft with
// a one-release shelf life. What covers it in practice is the contract
// bump: the GUI shuts the old daemon down in-band before starting the
// new one. A hand-started hived during the upgrade is the residual
// gap, and it is a one-time one.
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
