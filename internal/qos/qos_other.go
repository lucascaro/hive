//go:build !windows

package qos

// EcoQoS is a Windows mechanism and has no counterpart to opt out of
// elsewhere. macOS App Nap is the nearest analogue, but that is a different
// API with a different lifetime model and it lives in internal/activity, which
// the GUI calls alongside this. Deliberately cgo-free so hived — built for both
// darwin arches with cgo off and merged by lipo — can import this package.
func disableThrottling() {}
