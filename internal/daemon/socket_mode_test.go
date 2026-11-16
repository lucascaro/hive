// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package daemon

import (
	"os"
	"runtime"
	"testing"
)

// Every unix socket the daemon binds must be private to the user.
func TestDaemonSocketsAre0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix mode bits on Windows")
	}
	d := startTestDaemon(t)
	path, closeFn, err := d.listenPlugin("modetest")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeFn)
	for name, p := range map[string]string{"control": d.SocketPath(), "events": d.EventSocketPath(), "plugin": path} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := fi.Mode().Perm(); got != 0o600 {
			t.Errorf("%s socket mode = %o, want 600", name, got)
		}
	}
}
