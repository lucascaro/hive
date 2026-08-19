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
		PhaseChecking, PhaseClosing, PhaseRestarting,
	} {
		if !strings.Contains(ts, "'"+phase+"'") {
			t.Errorf("phase %q is not in %s — the GUI silently stops rendering it; update PHASE and CREATE_ORDER there",
				phase, rel)
		}
	}
}
