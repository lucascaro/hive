//go:build !windows

package qos

// EcoQoS is a Windows mechanism and has no counterpart to opt out of
// elsewhere. macOS App Nap is the nearest analogue, but that is a different
// API with a different lifetime model and it lives in internal/activity, which
// the GUI calls alongside this. Deliberately cgo-free so hived — cross-compiled
// for darwin from a Linux/Windows host with cgo off — can import this package.
func disableThrottling() {}
