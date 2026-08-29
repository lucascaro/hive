package main

import (
	"fmt"
	"testing"
	"time"
)

// stubStaging replaces the platform staging implementation. done is
// closed once the goroutine has called it, so tests can wait without
// sleeping on a guess.
func stubStaging(t *testing.T, bundle string, err error) (started chan struct{}, release chan struct{}) {
	t.Helper()
	started, release = make(chan struct{}), make(chan struct{})
	prev := stageUpdateFn
	stageUpdateFn = func(UpdateInfo, func(string)) (string, error) {
		close(started)
		<-release
		return bundle, err
	}
	t.Cleanup(func() { stageUpdateFn = prev })
	return started, release
}

// waitForStage polls UpdateStatus until it reports want, so the test
// does not race the staging goroutine's final transition.
func waitForStage(t *testing.T, a *App, want string) UpdateInfo {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info := a.UpdateStatus(); info.Stage == want {
			return info
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("Stage never reached %q (last: %+v)", want, a.UpdateStatus())
	return UpdateInfo{}
}

func TestStartUpdateRefusesWhenNothingAvailable(t *testing.T) {
	a := &App{}
	a.rememberCheck(UpdateInfo{Channel: ChannelRelease, Stage: StageIdle})
	if err := a.StartUpdate(); err == nil {
		t.Fatal("StartUpdate = nil error with no update available, want a refusal")
	}
}

func TestStartUpdateIsSingleFlight(t *testing.T) {
	a := &App{}
	a.rememberCheck(UpdateInfo{Available: true, Latest: "9.9.9", Stage: StageAvailable})
	started, release := stubStaging(t, "/staged/hivegui.app", nil)

	if err := a.StartUpdate(); err != nil {
		t.Fatalf("StartUpdate: %v", err)
	}
	<-started
	if err := a.StartUpdate(); err == nil {
		t.Error("a second StartUpdate while staging = nil error, want a refusal")
	}
	// Applying mid-staging must also be refused: there is no bundle yet.
	if err := a.ApplyUpdateAndRestart(); err == nil {
		t.Error("ApplyUpdateAndRestart mid-staging = nil error, want a refusal")
	}

	close(release)
	info := waitForStage(t, a, StageReady)
	if info.Message == "" {
		t.Error("ready state carries no message for the button to show")
	}
}

func TestStartUpdateSurfacesStagingFailure(t *testing.T) {
	a := &App{}
	a.rememberCheck(UpdateInfo{Available: true, Latest: "9.9.9", Stage: StageAvailable})
	_, release := stubStaging(t, "", fmt.Errorf("checksum mismatch"))
	close(release)

	if err := a.StartUpdate(); err != nil {
		t.Fatalf("StartUpdate: %v", err)
	}
	info := waitForStage(t, a, StageError)
	if info.Message != "checksum mismatch" {
		t.Errorf("Message = %q, want the staging error verbatim", info.Message)
	}
	// A failed staging must leave nothing to apply.
	if err := a.ApplyUpdateAndRestart(); err == nil {
		t.Error("ApplyUpdateAndRestart after a failed staging = nil error, want a refusal")
	}
}

// A newer release landing after staging must invalidate the staged
// bundle — otherwise Restart installs a build that is already behind.
func TestRememberCheckDropsStaleStaging(t *testing.T) {
	a := &App{}
	a.rememberCheck(UpdateInfo{Available: true, Latest: "9.9.9", Stage: StageAvailable})
	_, release := stubStaging(t, "/staged/hivegui.app", nil)
	close(release)
	if err := a.StartUpdate(); err != nil {
		t.Fatalf("StartUpdate: %v", err)
	}
	waitForStage(t, a, StageReady)

	// Same version: the ready state survives.
	a.rememberCheck(UpdateInfo{Available: true, Latest: "9.9.9", Stage: StageAvailable})
	if got := a.UpdateStatus().Stage; got != StageReady {
		t.Errorf("Stage = %q after a re-check of the same version, want %q", got, StageReady)
	}

	// Newer version: staging is discarded and the button goes back to
	// offering an update.
	a.rememberCheck(UpdateInfo{Available: true, Latest: "9.9.10", Stage: StageAvailable})
	if got := a.UpdateStatus().Stage; got != StageAvailable {
		t.Errorf("Stage = %q after a newer version appeared, want %q", got, StageAvailable)
	}
	if err := a.ApplyUpdateAndRestart(); err == nil {
		t.Error("ApplyUpdateAndRestart = nil error after the staging went stale, want a refusal")
	}
}
