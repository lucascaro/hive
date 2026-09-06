// Dialing hived and, when it is not running, spawning it. Split out of
// app.go; see app.go for the App type itself.
package main

import (
	"fmt"
	"net"
	"sync/atomic"
	"time"

	hdaemon "github.com/lucascaro/hive/internal/daemon"
)

// ----------------------------- daemon spawn ------------------------------

// Dial budgets. The daemon binds its socket last, once it can answer,
// so the retry loop below is the whole wait — but how long that wait
// is worth depends on the caller.
//
// bootDialBudget covers a cold start: spawning the binary and opening
// the registry on a slow machine is seconds, not milliseconds.
// attachDialBudget is deliberately short — OpenSession runs behind
// openMu, so on an N-session grid launch its budget is paid N times
// in series, and by then the daemon is known to be up anyway. Vars so
// tests can shrink them.
var (
	bootDialBudget   = 15 * time.Second
	attachDialBudget = 3 * time.Second
)

// spawnedHived records that this GUI process has already spawned a
// daemon. The boot path retries forever (reconnectControl), and
// without this a hived that crashes on startup would be re-forked
// every few seconds for the life of the app.
var spawnedHived atomic.Bool

// dialOrSpawn dials hived; on the first failure it spawns hived as a
// detached child, then retries with backoff until budget is spent.
// cwd, when non-empty, is passed to hived as --cwd so newly-created
// sessions default to that directory.
//
// The retry loop is the whole wait: the daemon's socket does not
// exist until it is ready to handshake, so a dial that connects is a
// daemon that answers.
// dialSock is what an already-running daemon is expected to answer on;
// spawnSock is where a daemon this function starts will bind. They
// differ only across the 2026-09 socket move, where dialSock can be the
// OLD default because a pre-move daemon is still serving it. Nothing
// this process starts ever binds that path — migrating forward is the
// whole point — so a legacy daemon that dies between the two is
// replaced at the new path, not the old one.
func dialOrSpawn(dialSock, spawnSock, cwd string, budget time.Duration) (net.Conn, error) {
	// Trust an answering socket only when its directory is ours: one in
	// a directory someone else owns is an impostor that would see every
	// keystroke. A failed check here is not fatal on its own — it just
	// means there is nothing to connect to, and the spawn path below
	// verifies (and creates) the directory it is actually going to use.
	if hdaemon.CheckSocketDir(dialSock) == nil {
		if c, err := net.Dial("unix", dialSock); err == nil {
			return c, nil
		}
	}
	if err := hdaemon.EnsureSocketDir(spawnSock); err != nil {
		return nil, fmt.Errorf("socket dir: %w", err)
	}
	// Spawn at most once per GUI process. A daemon that dies on
	// startup is a bug to surface, not to retry into a fork loop.
	if spawnedHived.CompareAndSwap(false, true) {
		if err := spawnHived(spawnSock, cwd); err != nil {
			return nil, fmt.Errorf("spawn hived: %w", err)
		}
	}
	deadline := time.Now().Add(budget)
	delay := 100 * time.Millisecond
	for {
		time.Sleep(delay)
		if c, err := net.Dial("unix", spawnSock); err == nil {
			return c, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("hived did not come up at %s within %s", spawnSock, budget)
		}
		if delay < 1600*time.Millisecond {
			delay *= 2
		}
	}
}
