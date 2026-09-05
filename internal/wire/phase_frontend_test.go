package wire

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPhaseConstantsMatchFrontend guards the one coupling the type
// systems can't see: the GUI re-declares these phase strings in
// TypeScript (cmd/hivegui/frontend/src/lib/phase-steps.ts), because
// the wire is JSON and nothing generates the frontend's copy.
//
// A rename here fails silently over there — phasePanel() stops
// recognising the phase and simply returns null, so the loading panel
// never appears and no error is raised anywhere. This test turns that
// into a build failure on the Go side, where the rename happens.
func TestPhaseConstantsMatchFrontend(t *testing.T) {
	const rel = "../../cmd/hivegui/frontend/src/lib/phase-steps.ts"
	src, err := os.ReadFile(filepath.Clean(rel))
	if err != nil {
		t.Skipf("frontend source not available (%v)", err)
	}
	ts := string(src)

	// PhaseReady is the empty string by design (omitempty on the
	// wire), so it has nothing to match on; the rest must appear
	// verbatim as quoted strings in the TS module.
	for _, phase := range []string{
		PhaseStarting, PhaseFetching, PhaseWorktree, PhaseSpawning,
		PhaseChecking, PhaseClosing, PhaseRestarting, PhaseReviving,
	} {
		if !strings.Contains(ts, "'"+phase+"'") {
			t.Errorf("phase %q is not in %s — the GUI silently stops rendering it; update PHASE and CREATE_ORDER there",
				phase, rel)
		}
	}
}

// TestStateConstantsMatchFrontend guards the same coupling for session
// states. The GUI re-declares them in TypeScript
// (cmd/hivegui/frontend/src/lib/session-state.ts DAEMON_STATE), because
// the wire is JSON and nothing generates the frontend's copy.
//
// The failure mode is quieter than the phase one: sessionState() simply
// falls through to its default and every session renders idle forever.
// Nothing throws, nothing logs.
func TestStateConstantsMatchFrontend(t *testing.T) {
	const rel = "../../cmd/hivegui/frontend/src/lib/session-state.ts"
	src, err := os.ReadFile(filepath.Clean(rel))
	if err != nil {
		t.Skipf("frontend source not available (%v)", err)
	}
	ts := string(src)

	// StateIdle and StateSourceHeuristic are the empty string by design
	// (omitempty on the wire), so they have nothing to match on.
	for _, state := range []string{
		StateWorking, StateWaitingInput, StateWaitingPermission,
		StateExited, StateError,
	} {
		if !strings.Contains(ts, "'"+state+"'") {
			t.Errorf("state %q is not in %s — the GUI silently renders it as idle; update DAEMON_STATE and sessionState() there",
				state, rel)
		}
	}
}
