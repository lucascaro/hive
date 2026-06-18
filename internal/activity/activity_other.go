//go:build !darwin

package activity

// No App Nap outside macOS; nothing to opt out of.
func disableThrottling() {}
