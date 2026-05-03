//go:build windows

package main

// killRunningHived is a no-op on Windows: the daemon is not yet
// supported there, and the GUI build doesn't ship a pidfile path
// that we can act on.
func killRunningHived(_ string) error { return nil }
