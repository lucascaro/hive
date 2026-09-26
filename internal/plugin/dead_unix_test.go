//go:build !windows

package plugin

import (
	"errors"
	"syscall"
	"testing"
	"time"
)

// assertDead waits for pid to stop existing. A killed grandchild is
// reparented and reaped by init, so give it a moment.
func assertDead(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d still alive", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
