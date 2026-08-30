package main

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestMain points the two platform seams at stubs that fail loudly.
// Several tests below assert that StartUpdate/ApplyUpdateAndRestart
// *refuse* — and the production seams are a GitHub download (or a
// `git pull` + `./build.sh` over the developer's own checkout) and an
// in-place swap of the running Hive.app followed by a real daemon
// restart. The moment one of those guards regresses, the test that
// exists to catch it would instead replace the developer's install.
// Tests that need staging to do something install their own stub.
func TestMain(m *testing.M) {
	stageUpdateFn = func(UpdateInfo, func(string)) (string, error) {
		return "", fmt.Errorf("stageUpdate must not run in tests")
	}
	applyStagedBundleFn = func(string) error {
		return fmt.Errorf("applyStagedBundle must not run in tests")
	}
	// RestartDaemon is the most dangerous of the three: it dials the
	// real hived socket, escalates to SIGKILL against whatever answers,
	// and finishes with spawnNewGUI, which re-execs this test binary as
	// a detached child that runs the entire suite again. Leaving it live
	// is how a single test spawned a daemon against the developer's real
	// state directory.
	restartDaemonFn = func(*App) error {
		return fmt.Errorf("RestartDaemon must not run in tests")
	}
	os.Exit(m.Run())
}

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

// A newer release landing *while* staging runs must not be presented as
// ready: the bundle on disk is the older one, and Restart would install
// it under the newer version's name.
func TestStartUpdateDropsStagingOvertakenByNewerVersion(t *testing.T) {
	a := &App{}
	a.rememberCheck(UpdateInfo{Available: true, Latest: "9.9.9", Stage: StageAvailable})
	started, release := stubStaging(t, "/staged/hivegui.app", nil)

	if err := a.StartUpdate(); err != nil {
		t.Fatalf("StartUpdate: %v", err)
	}
	<-started
	// Mid-staging check finds a newer release.
	a.rememberCheck(UpdateInfo{Available: true, Latest: "9.9.10", Stage: StageAvailable})
	close(release)

	info := waitForStage(t, a, StageAvailable)
	if info.Latest != "9.9.10" {
		t.Errorf("Latest = %q, want the newer version", info.Latest)
	}
	if err := a.ApplyUpdateAndRestart(); err == nil {
		t.Error("ApplyUpdateAndRestart = nil error after the staging was overtaken, want a refusal")
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

// A settings change landing mid-staging is the case the Latest identity
// check cannot see: the bundle does not exist yet, and a channel or
// source-repo switch can leave Latest looking untouched. The generation
// counter is what closes it.
func TestStagingDiscardedWhenSettingsChangeMidFlight(t *testing.T) {
	a := &App{}
	a.rememberCheck(UpdateInfo{
		Available: true, Latest: "2.5.0", Channel: ChannelRelease, Stage: StageAvailable,
	})
	started, release := stubStaging(t, "/staged/hivegui.app", nil)

	if err := a.StartUpdate(); err != nil {
		t.Fatalf("StartUpdate: %v", err)
	}
	<-started
	// The user switches settings while the download/build is running.
	a.forgetUpdateState()
	close(release)

	waitForStage(t, a, StageIdle)
	if err := a.ApplyUpdateAndRestart(); err == nil {
		t.Fatal("ApplyUpdateAndRestart = nil error, want the superseded bundle to have been discarded")
	}
	if got := a.UpdateStatus(); got.Stage == StageReady {
		t.Errorf("Stage = %q, want the button not to offer Restart", got.Stage)
	}
}

// RestartDaemon normally quits this process, but it is built to refuse
// and leave a working window when the daemon will not die. On that path
// the staged bundle has already been installed and pruned, so the state
// must not still advertise Restart — a second click would try to
// install from a directory that no longer exists.
func TestApplyClearsStagedStateAfterASuccessfulSwap(t *testing.T) {
	a := &App{}
	a.rememberCheck(UpdateInfo{
		Available: true, Latest: "2.5.0", Channel: ChannelRelease, Stage: StageAvailable,
	})
	_, release := stubStaging(t, "/staged/hivegui.app", nil)
	close(release)
	if err := a.StartUpdate(); err != nil {
		t.Fatalf("StartUpdate: %v", err)
	}
	waitForStage(t, a, StageReady)

	swapped := 0
	prev := applyStagedBundleFn
	applyStagedBundleFn = func(string) error { swapped++; return nil }
	t.Cleanup(func() { applyStagedBundleFn = prev })

	// RestartDaemon fails here — no wire client on a bare App — which is
	// precisely the "left a working window" path this guards.
	_ = a.ApplyUpdateAndRestart()
	if swapped != 1 {
		t.Fatalf("applyStagedBundle called %d times, want 1", swapped)
	}
	if got := a.UpdateStatus(); got.Stage == StageReady {
		t.Errorf("Stage = %q after the swap landed, want the Restart offer withdrawn", got.Stage)
	}
	if err := a.ApplyUpdateAndRestart(); err == nil {
		t.Error("a second apply = nil error, want a refusal rather than a retry from a pruned path")
	}
	if swapped != 1 {
		t.Errorf("applyStagedBundle called %d times total, want the second click refused", swapped)
	}
}
