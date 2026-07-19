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
		c, err := net.DialTimeout("unix", sock, 500*time.Millisecond)
		if err != nil {
			return true
		}
		_ = c.Close()
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}
