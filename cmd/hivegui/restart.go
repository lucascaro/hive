package main

import (
	"net"
	"time"
)

// socketDead reports whether nothing answers on sock, polling until
// budget expires.
//
// This is the authoritative liveness test for the restart path, and
// deliberately replaces signal-based probing. hived runs as a direct
// child of the GUI that never Wait()s on it, so a SIGTERM'd daemon
// lingers as a zombie: signal(0) keeps succeeding long after the
// process has stopped serving. A zombie holds no socket, so dialing
// answers the question we actually care about — "can a relaunched GUI
// still reach the old daemon?" — which is exactly the failure this
// whole change exists to prevent.
func socketDead(sock string, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for {
		// Clamp the dial timeout to what's left, so the call as a
		// whole honours budget instead of overrunning it by up to a
		// full dial timeout on the last iteration.
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		if remaining > dialTimeout {
			remaining = dialTimeout
		}
		c, err := net.DialTimeout("unix", sock, remaining)
		if err != nil {
			return true
		}
		_ = c.Close()
		time.Sleep(pollInterval)
	}
}

const (
	dialTimeout = 500 * time.Millisecond
	// Every probe dial costs the daemon an accepted connection that
	// hangs up without a HELLO. 200ms keeps the restart responsive
	// while keeping that chatter to a handful of entries over the
	// fallback window.
	pollInterval = 200 * time.Millisecond
)
