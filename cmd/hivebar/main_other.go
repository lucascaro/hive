//go:build !darwin

// hivebar is macOS-only: it is a status-bar agent, and a menu bar is
// the one thing the other platforms do not have a shared equivalent
// for (Windows has a notification-area icon, Linux has whatever the
// desktop's StatusNotifierItem support happens to be). Rather than
// exclude the package entirely — which leaves `go build ./...` and
// staticcheck staring at a directory with no buildable files — it
// compiles to a binary that says so.
package main

import "fmt"

func main() {
	fmt.Println("hivebar is macOS-only; the Hive menu bar is not available on this platform")
}
