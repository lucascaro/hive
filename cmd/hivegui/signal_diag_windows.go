//go:build windows

package main

// installSignalDiag is a no-op on Windows: SIGHUP/SIGTERM/SIGINT don't
// exist there in the POSIX sense, and Wails' own signal handling (see
// signal_diag_unix.go) is unix-only too.
func installSignalDiag() {}
