//go:build windows

package plugin

import (
	"os"
	"testing"
	"time"
)

// assertDead waits for pid to exit. On Windows FindProcess opens a
// handle, and Wait on a live process would block, so poll with a
// signal-free probe: Release after a failed open means it is gone.
func assertDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p, err := os.FindProcess(pid)
		if err != nil {
			return
		}
		_ = p.Release()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("process %d still alive", pid)
}
