package registry

import (
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// A child that exits by itself — the ordinary way a user ends an agent
// session — must reach the same state Kill's path does. Every other
// exit test ends the session from our side (Kill or sess.Close), which
// fails the PTY read directly and so could not see #379: on Linux the
// master read never fails after the child exits, and only the session
// package's reaper turns the exit into Done(). No Close, no Kill here.
func TestSessionExitingOnItsOwnIsSeen(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, _ := liveSession(t, r, wire.CreateSpec{Name: "self-exit", Shell: "/bin/sh", Cmd: []string{"true"}})

	waitFor(t, "the self-exited session to be seen", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return !r.entries[e.ID].Alive()
	})
	r.mu.Lock()
	info := r.entries[e.ID].Info()
	r.mu.Unlock()
	if info.State != wire.StateExited {
		t.Errorf("state = %q, want %q", info.State, wire.StateExited)
	}
}
