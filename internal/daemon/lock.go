package daemon

import (
	"errors"
	"time"
)

// ErrAlreadyRunning is what acquireStateLock returns when another hived
// already holds this state directory.
var ErrAlreadyRunning = errors.New("another hived is already running against this state directory")

// stateLockBudget is how long a starting daemon waits for the state
// lock before giving up, and stateLockPoll how often it retries.
//
// Non-zero because of the restart handoff: the outgoing daemon closes
// its listeners first and releases the lock last, after draining its
// in-flight ops. The GUI reads the dead socket as "daemon gone" and
// spawns a replacement inside that gap — and dialOrSpawn spawns at most
// once per GUI process, so a replacement that exits on a held lock
// leaves the app with no daemon at all. Waiting a few seconds costs a
// slow start in the rare case; not waiting costs the restart.
//
// Vars, not consts, so the tests can shrink them. Declared here rather
// than beside the POSIX implementation so the tests that shrink them
// still compile on Windows, where acquireStateLock is a no-op.
var (
	stateLockBudget = 5 * time.Second
	stateLockPoll   = 50 * time.Millisecond
)
