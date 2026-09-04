package main

import (
	"errors"
	"testing"

	"github.com/lucascaro/hive/internal/buildinfo"
)

// stubStagedIdentity replaces the probe for the lifetime of the test.
// The real one runs a binary out of a .app bundle, which no unit test
// has.
func stubStagedIdentity(t *testing.T, id buildinfo.Identity, err error) {
	t.Helper()
	prev := stagedIdentityFn
	stagedIdentityFn = func(string) (buildinfo.Identity, error) { return id, err }
	t.Cleanup(func() { stagedIdentityFn = prev })
}

// The comparison is STAGED daemon vs RUNNING daemon — not vs this
// GUI's own constant. After the swap it is the staged GUI driving the
// running daemon, so those are the two that have to agree.
func TestRestartKindGUIWhenStagedMatchesRunningDaemon(t *testing.T) {
	stubStagedIdentity(t, buildinfo.Identity{DaemonContract: 4}, nil)

	if got := restartKindFor("/tmp/staged.app", 4); got != RestartGUI {
		t.Errorf("restartKindFor = %q, want %q", got, RestartGUI)
	}
}

func TestRestartKindFullWhenStagedDiffersFromRunningDaemon(t *testing.T) {
	stubStagedIdentity(t, buildinfo.Identity{DaemonContract: 5}, nil)

	if got := restartKindFor("/tmp/staged.app", 4); got != RestartFull {
		t.Errorf("restartKindFor = %q, want %q", got, RestartFull)
	}
}

// Every uncertain path must land on the destructive answer. Being
// wrong that way costs a restart the user might not have needed; being
// wrong the other way silently reloads a GUI into a daemon it does not
// understand, which is unrecoverable from the user's side.
func TestRestartKindFullOnEveryUncertainty(t *testing.T) {
	cases := []struct {
		name     string
		staged   buildinfo.Identity
		probeErr error
		running  int
	}{
		{
			name:    "running daemon predates the contract field",
			staged:  buildinfo.Identity{DaemonContract: 4},
			running: 0,
		},
		{
			name:    "staged daemon predates the contract field",
			staged:  buildinfo.Identity{DaemonContract: 0},
			running: 4,
		},
		{
			name:     "staged binary could not be run",
			probeErr: errors.New("exec format error"),
			running:  4,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stubStagedIdentity(t, c.staged, c.probeErr)
			if got := restartKindFor("/tmp/staged.app", c.running); got != RestartFull {
				t.Errorf("restartKindFor = %q, want %q", got, RestartFull)
			}
		})
	}
}

// ApplyUpdateAndRestart must route on the kind recorded at staging
// time. Reaching RestartDaemon for a GUI-only update would kill every
// session the update did not need to touch.
func TestApplyUpdateRoutesOnRestartKind(t *testing.T) {
	for _, c := range []struct {
		kind        string
		wantReload  int
		wantRestart int
	}{
		{RestartGUI, 1, 0},
		{RestartFull, 0, 1},
		// A bundle staged before the kind was known must take the safe
		// path rather than defaulting to reload.
		{"", 0, 1},
	} {
		t.Run("kind="+c.kind, func(t *testing.T) {
			var reloads, restarts int
			prevApply, prevRestart, prevReload := applyStagedBundleFn, restartDaemonFn, requestReloadAllGUIsFn
			applyStagedBundleFn = func(string) error { return nil }
			restartDaemonFn = func(*App) error { restarts++; return nil }
			requestReloadAllGUIsFn = func(*App) error { reloads++; return nil }
			t.Cleanup(func() {
				applyStagedBundleFn, restartDaemonFn, requestReloadAllGUIsFn = prevApply, prevRestart, prevReload
			})

			a := &App{}
			a.update.bundle = "/tmp/staged.app"
			a.update.restartKind = c.kind

			if err := a.ApplyUpdateAndRestart(); err != nil {
				t.Fatalf("ApplyUpdateAndRestart: %v", err)
			}
			if reloads != c.wantReload || restarts != c.wantRestart {
				t.Errorf("reloads=%d restarts=%d, want %d/%d",
					reloads, restarts, c.wantReload, c.wantRestart)
			}
		})
	}
}
