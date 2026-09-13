//go:build !darwin && !windows

package activity

// macOS has App Nap and Windows has EcoQoS; everywhere else there is nothing
// to opt out of.
func disableThrottling() {}
