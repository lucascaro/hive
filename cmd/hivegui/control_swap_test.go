package main

import (
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// The control connection is the GUI's only source of the session list, and
// the daemon sends that list ONCE, unsolicited, on handshake. So a frontend
// with no state — every page load — must get a new handshake, and the old
// ConnectControl's "reuse if non-nil" fast path is what left a reloaded page
// permanently empty ("no sessions" after the Debug menu's trace toggle, the
// only location.reload() in the app; the Go process survives it, so
// a.control outlives the page that opened it).
//
// Replacing the connection instead introduces one hazard, which these tests
// pin: the displaced read loop must not announce its own death. If it does,
// the frontend's control:disconnect handler starts the reconnect loop, which
// calls ConnectControl, which supersedes again — a redial that never
// settles.

func TestDetachControlHandsBackTheOldConnection(t *testing.T) {
	c1 := &wire.Client{}
	a := &App{control: c1}

	if got := a.detachControl(); got != c1 {
		t.Errorf("detachControl() = %p, want %p", got, c1)
	}
	if a.control != nil {
		t.Error("detachControl must leave no installed connection for a read loop to claim")
	}
	if got := a.detachControl(); got != nil {
		t.Errorf("detachControl() on an empty App = %p, want nil", got)
	}
}

func TestRetireControlOnlyReportsTheCurrentConnection(t *testing.T) {
	c1, c2 := &wire.Client{}, &wire.Client{}
	a := &App{control: c1}

	// The live connection ending IS a lost daemon: report it, and clear it
	// so the reconnect loop can install a replacement.
	if !a.retireControl(c1) {
		t.Error("retireControl(current) = false, want true — a real disconnect must be announced")
	}
	if a.control != nil {
		t.Error("retireControl(current) must clear the installed connection")
	}

	// Now the supersede case: c2 is installed, c1's read loop drains and
	// exits. c1 must report false, and must NOT clear c2 on its way out —
	// clearing it would strand the GUI with a live socket it thinks is gone.
	a.control = c2
	if a.retireControl(c1) {
		t.Error("retireControl(superseded) = true — would emit control:disconnect and start an endless redial")
	}
	if a.control != c2 {
		t.Error("a superseded read loop must not disturb the connection that replaced it")
	}
}
