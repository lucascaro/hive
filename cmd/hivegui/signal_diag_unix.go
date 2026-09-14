//go:build !windows

package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

// installSignalDiag logs terminal signals, so a kill is distinguishable
// from a user quit in hivegui.log.
//
// SIGTERM and SIGINT only log. Wails (v2 internal/signal) already
// Notifies for both and turns them into an orderly quit, which runs
// OnBeforeClose — so without this line a `killall hivegui` reads exactly
// like Cmd+Q. Registering a second channel does not steal the signal
// from Wails: os/signal delivers a copy to every channel registered for
// it.
//
// SIGHUP has no Wails handler, so after logging it is reset to the
// default disposition and re-raised: the process still dies the way it
// would have without this hook.
func installSignalDiag() {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		for sig := range ch {
			log.Printf("hivegui: received signal %v", sig)
			if sig == syscall.SIGHUP {
				signal.Reset(syscall.SIGHUP)
				_ = syscall.Kill(os.Getpid(), syscall.SIGHUP)
				return
			}
		}
	}()
}
