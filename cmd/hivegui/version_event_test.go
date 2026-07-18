package main

import (
	"testing"

	"github.com/lucascaro/hive/internal/buildinfo"
)

// TestDaemonVersionEvent pins the payload the sidebar version footer and
// the stale-daemon banner both consume. The central invariant: Severity
// is derived from build IDs only, never from the release strings.
func TestDaemonVersionEvent(t *testing.T) {
	t.Cleanup(buildinfo.SetForTest("gui-build"))
	t.Cleanup(buildinfo.SetVersionForTest("v1.2.3"))

	cases := []struct {
		name          string
		daemonBuild   string
		daemonRelease string
		wantSeverity  string
	}{
		{"same build", "gui-build", "v1.2.3", "match"},
		{"different build", "other-build", "v1.0.0", "mismatch"},
		{"daemon build unknown", "", "v1.2.3", "unknown"},
		// An older daemon predating the Release wire field: the build
		// IDs still decide severity, and the empty release must survive
		// to the frontend so it can fall back to build-only rendering.
		{"older daemon, no release", "gui-build", "", "match"},
		// Releases differing while builds match cannot happen in
		// practice (build IDs are git revisions), but if it ever does,
		// severity must still follow the build IDs rather than silently
		// gaining a second comparison.
		{"release differs, build matches", "gui-build", "v9.9.9", "match"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := daemonVersionEvent(c.daemonBuild, c.daemonRelease)

			if ev.Severity != c.wantSeverity {
				t.Errorf("Severity: got %q, want %q", ev.Severity, c.wantSeverity)
			}
			if ev.GuiBuild != "gui-build" {
				t.Errorf("GuiBuild: got %q, want %q", ev.GuiBuild, "gui-build")
			}
			if ev.GuiRelease != "v1.2.3" {
				t.Errorf("GuiRelease: got %q, want %q", ev.GuiRelease, "v1.2.3")
			}
			if ev.DaemonBuild != c.daemonBuild {
				t.Errorf("DaemonBuild: got %q, want %q", ev.DaemonBuild, c.daemonBuild)
			}
			if ev.DaemonRelease != c.daemonRelease {
				t.Errorf("DaemonRelease: got %q, want %q", ev.DaemonRelease, c.daemonRelease)
			}
		})
	}
}
