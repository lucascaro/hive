//go:build windows

package main

import "errors"

// killRunningHived is unimplemented on Windows: the daemon isn't
// supported there yet, and we don't have a reliable cross-process
// signal path. Return an error so the GUI's "Restart daemon" action
// surfaces the failure instead of silently pretending it worked.
func killRunningHived(_ string) error {
	return errors.New("restart not supported on this platform")
}
