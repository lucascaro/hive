package main

import (
	"encoding/json"
	"log"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/lucascaro/hive/internal/buildinfo"
)

// stagedIdentityTimeout bounds the `hived --version` probe. The flag
// path prints a struct and returns before any socket or state dir is
// touched, so anything near this bound means the binary is wrong for
// this machine (bad architecture, missing dylib) — in which case the
// answer we want is the conservative one anyway.
var stagedIdentityTimeout = 3 * time.Second

// stagedIdentityFn is seamed for tests, which have no bundle to run.
var stagedIdentityFn = stagedIdentity

// stagedIdentity runs the hived inside a staged .app bundle and reads
// back its build identity.
//
// Running the binary is the only way to ask a build on disk what its
// daemon contract is, and asking is the whole point: without it the
// updater cannot tell a frontend-only release (relaunch the GUI, keep
// every session) from one that replaces the daemon (every session
// ends). The probe is safe to run — `--version` prints and exits
// before the daemon touches a socket or the state dir.
func stagedIdentity(bundle string) (buildinfo.Identity, error) {
	bin := filepath.Join(bundle, "Contents", "MacOS", "hived")
	cmd := exec.Command(bin, "--version", "--json")
	// Belt and braces on top of --version's own early return: a probe
	// must never be able to adopt this GUI's environment and start
	// talking to the live daemon.
	cmd.Env = append(cmd.Environ(), "HIVE_NO_MENUBAR=1")

	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		defer close(done)
		out, err = cmd.Output()
	}()
	select {
	case <-done:
	case <-time.After(stagedIdentityTimeout):
		_ = cmd.Process.Kill()
		<-done
	}
	if err != nil {
		return buildinfo.Identity{}, err
	}
	var id buildinfo.Identity
	if err := json.Unmarshal(out, &id); err != nil {
		return buildinfo.Identity{}, err
	}
	return id, nil
}

// restartKindFor decides what applying the staged bundle will cost.
//
// The comparison is between the STAGED daemon's contract and the
// RUNNING daemon's, not this GUI's. After the swap it is the staged
// GUI that has to drive the running daemon, and the staged GUI carries
// the staged contract — comparing against the current GUI's would give
// the right answer only by accident, whenever this GUI and the running
// daemon already agreed.
//
// Every uncertain path returns RestartFull. Being wrong in that
// direction costs the user a restart they might not have needed;
// being wrong the other way silently reloads a GUI into a daemon it
// does not understand.
func restartKindFor(bundle string, runningDaemonContract int) string {
	if runningDaemonContract == 0 {
		// A daemon that advertised no contract predates the field, so
		// nothing is known about its behavior.
		log.Printf("hivegui: update: running daemon advertises no contract; full restart required")
		return RestartFull
	}
	id, err := stagedIdentityFn(bundle)
	if err != nil {
		log.Printf("hivegui: update: could not read staged daemon identity: %v; assuming full restart", err)
		return RestartFull
	}
	if id.DaemonContract == 0 {
		log.Printf("hivegui: update: staged daemon advertises no contract; full restart required")
		return RestartFull
	}
	if id.DaemonContract != runningDaemonContract {
		log.Printf("hivegui: update: staged daemon contract %d != running %d; full restart required",
			id.DaemonContract, runningDaemonContract)
		return RestartFull
	}
	log.Printf("hivegui: update: staged daemon contract %d matches the running daemon; GUI reload is enough",
		id.DaemonContract)
	return RestartGUI
}
