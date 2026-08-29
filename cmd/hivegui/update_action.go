package main

import (
	"fmt"
	"log"
	"sync"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Seams so the state machine below can be tested without downloading a
// release or replacing an app bundle. Production values are the
// platform implementations in update_apply_{darwin,other}.go.
var (
	stageUpdateFn       = stageUpdate
	applyStagedBundleFn = applyStagedBundle
)

// updateState is everything the Update/Restart button needs to know,
// shared between the check, the staging goroutine, and the apply call.
//
// It hangs off App rather than living at package scope so the tests can
// drive a fresh one per case, and so a second GUI window (a separate
// process) can never see half of this one's staging.
type updateState struct {
	mu sync.Mutex
	// last is the most recent check result, with Stage/Message kept
	// current by the staging goroutine. It is what UpdateStatus returns
	// and what the button renders from.
	last UpdateInfo
	// busy is true from StartUpdate until the staging goroutine exits.
	// Guarded by mu rather than an atomic because every transition also
	// touches last, and two locks around one state machine is how the
	// button ends up disagreeing with reality.
	busy bool
	// bundle is the staged hivegui.app, and stagedFor the Latest it was
	// staged for. Keeping the identity lets a later check throw away a
	// staging that a newer release has already obsoleted.
	bundle    string
	stagedFor string
}

// rememberCheck records a check result. A staged bundle that no longer
// matches the newest available version is dropped: the button must not
// offer "Restart" into a build that is already behind again.
func (a *App) rememberCheck(info UpdateInfo) {
	a.update.mu.Lock()
	defer a.update.mu.Unlock()
	if a.update.busy {
		// A check that lands mid-staging must not rewind the button
		// from "Updating…" back to "Update".
		a.update.last.Available = info.Available
		a.update.last.Latest = info.Latest
		return
	}
	if a.update.bundle != "" {
		if a.update.stagedFor == info.Latest {
			// Already staged for exactly this version — keep the ready
			// state instead of overwriting it with a fresh "available".
			info.Stage = StageReady
			info.Message = a.update.last.Message
			a.update.last = info
			return
		}
		a.update.bundle = ""
		a.update.stagedFor = ""
	}
	a.update.last = info
}

// forgetUpdateState discards the last check and any staged bundle. Used
// when something invalidates them wholesale — today, a channel change.
// A staging goroutine in flight is left alone: it holds its own copy of
// the info it started with, and its completion re-populates state that
// the next check will correct.
func (a *App) forgetUpdateState() {
	a.update.mu.Lock()
	defer a.update.mu.Unlock()
	a.update.last = UpdateInfo{}
	a.update.bundle = ""
	a.update.stagedFor = ""
}

// UpdateStatus returns the current state of the update action, so the
// frontend can render the button without re-running a check (and so a
// second window's Settings modal shows the same thing the banner does).
func (a *App) UpdateStatus() UpdateInfo {
	a.update.mu.Lock()
	defer a.update.mu.Unlock()
	return a.update.last
}

// setStage updates the button state and pushes it to the frontend.
// Every transition in the staging goroutine goes through here so the
// event and the polled status can never diverge.
func (a *App) setStage(stage, message string) {
	a.update.mu.Lock()
	a.update.last.Stage = stage
	a.update.last.Message = message
	snapshot := a.update.last
	a.update.mu.Unlock()
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "update:progress", snapshot)
	}
}

// StartUpdate stages the available update in the background: download +
// verify on the release channel, pull + build on the latest channel.
// It returns as soon as the goroutine is running; progress arrives as
// "update:progress" events and the button becomes Restart on success.
//
// Nothing is downloaded or built until this is called — the periodic
// check only ever reports.
func (a *App) StartUpdate() error {
	a.update.mu.Lock()
	if a.update.busy {
		a.update.mu.Unlock()
		return fmt.Errorf("an update is already in progress")
	}
	info := a.update.last
	if a.update.bundle != "" && a.update.stagedFor == info.Latest {
		a.update.mu.Unlock()
		return nil // already staged; the button is showing Restart
	}
	if !info.Available {
		a.update.mu.Unlock()
		return fmt.Errorf("no update available")
	}
	a.update.busy = true
	a.update.last.Stage = StageStaging
	a.update.last.Message = "Starting…"
	a.update.mu.Unlock()

	go func() {
		bundle, err := stageUpdateFn(info, func(msg string) { a.setStage(StageStaging, msg) })

		a.update.mu.Lock()
		a.update.busy = false
		if err != nil {
			a.update.mu.Unlock()
			log.Printf("hivegui: staging update failed: %v", err)
			a.setStage(StageError, err.Error())
			return
		}
		if a.update.last.Latest != info.Latest {
			// A newer version landed while we were staging. rememberCheck
			// could not drop this bundle — it did not exist yet — so drop
			// it here: offering Restart would install the build we staged
			// while the UI is already reporting the newer one.
			a.update.mu.Unlock()
			a.setStage(StageAvailable, "")
			return
		}
		a.update.bundle = bundle
		a.update.stagedFor = info.Latest
		a.update.mu.Unlock()
		a.setStage(StageReady, "Update ready — restart to apply")
	}()
	return nil
}

// ApplyUpdateAndRestart swaps the staged bundle over the installed one
// and relaunches.
//
// Order matters: the swap happens first and the restart second, so a
// swap that fails leaves the user looking at a working window running
// the old build rather than a half-replaced app.
func (a *App) ApplyUpdateAndRestart() error {
	a.update.mu.Lock()
	bundle := a.update.bundle
	busy := a.update.busy
	a.update.mu.Unlock()
	if busy {
		return fmt.Errorf("an update is still being prepared")
	}
	if bundle == "" {
		return fmt.Errorf("no staged update to apply")
	}
	if err := applyStagedBundleFn(bundle); err != nil {
		a.setStage(StageError, err.Error())
		return err
	}
	// The staged copy has served its purpose and is ~150MB. Pruning here
	// rather than on a timer keeps <stateDir>/updates from accumulating
	// one directory per version the user ever installed.
	pruneStagingDirs()
	// RestartDaemon owns the whole teardown: in-band shutdown frame,
	// socket-liveness probe, signal escalation, refuse-if-still-alive,
	// relaunch, quit. Reusing it is also what picks up the *new* hived,
	// which ships inside the bundle we just swapped.
	return a.RestartDaemon()
}
