package main

import (
	"testing"

	"github.com/lucascaro/hive/internal/buildinfo"
)

// TestDaemonVersionEvent pins the payload the sidebar version footer and
// the stale-daemon banner both consume.
//
// The central invariant: build IDs decide only whether anything changed,
// and the daemon CONTRACT decides what the user is asked to do about it.
// Getting that backwards is the bug this split exists to fix — comparing
// build IDs (git revisions) demanded a full daemon restart, killing every
// session, after a frontend-only rebuild.
func TestDaemonVersionEvent(t *testing.T) {
	t.Cleanup(buildinfo.SetForTest("gui-build"))
	t.Cleanup(buildinfo.SetVersionForTest("v1.2.3"))

	// The GUI's own contract, whatever it currently is. Cases below are
	// written relative to it so a legitimate bump doesn't break them.
	ours := buildinfo.DaemonContract

	cases := []struct {
		name           string
		daemonBuild    string
		daemonRelease  string
		daemonContract int
		wantSeverity   string
	}{
		{"same build", "gui-build", "v1.2.3", ours, "match"},
		{"daemon build unknown", "", "v1.2.3", ours, "unknown"},

		// The case this whole feature exists for: a rebuilt GUI against
		// a daemon whose observable behavior did not change.
		{"different build, same contract", "other-build", "v1.0.0", ours, "reloadable"},

		// A real daemon-side change. Sessions have to end, so the user
		// must be told to restart rather than reload.
		{"different build, newer contract", "other-build", "v1.0.0", ours + 1, "mismatch"},
		{"different build, older contract", "other-build", "v1.0.0", ours - 1, "mismatch"},

		// A daemon predating the contract field advertises 0. Nothing is
		// known about its behavior, so it must never be treated as
		// reloadable — silently reloading into it is the worst outcome
		// available here.
		{"different build, no contract", "other-build", "v1.0.0", 0, "mismatch"},

		// An older daemon predating the Release wire field: the build
		// IDs still decide whether anything changed, and the empty
		// release must survive to the frontend so it can fall back to
		// build-only rendering.
		{"older daemon, no release", "gui-build", "", ours, "match"},

		// Releases differing while builds match cannot happen in
		// practice (build IDs are git revisions), but if it ever does,
		// the match must still follow the build IDs rather than
		// silently gaining a second comparison.
		{"release differs, build matches", "gui-build", "v9.9.9", ours, "match"},

		// Matching builds outrank a contract disagreement: identical
		// git revisions cannot have different contracts, so this is a
		// corrupt input, and "match" keeps it silent rather than
		// nagging the user to restart into the build they already run.
		{"build matches, contract differs", "gui-build", "v1.2.3", ours + 1, "match"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := daemonVersionEvent(c.daemonBuild, c.daemonRelease, c.daemonContract)

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
			if ev.GuiContract != ours {
				t.Errorf("GuiContract: got %d, want %d", ev.GuiContract, ours)
			}
			if ev.DaemonContract != c.daemonContract {
				t.Errorf("DaemonContract: got %d, want %d", ev.DaemonContract, c.daemonContract)
			}
		})
	}
}

// TestDaemonContractIsPositive guards the one way the constant can be
// wrong on its own terms: 0 is the wire's "unknown" sentinel, so a
// daemon must never legitimately advertise it.
func TestDaemonContractIsPositive(t *testing.T) {
	if buildinfo.DaemonContract <= 0 {
		t.Fatalf("DaemonContract must be > 0 (0 means \"unknown\" on the wire); got %d",
			buildinfo.DaemonContract)
	}
}
