// Package activity opts the GUI process out of OS-level throttling.
//
// Debug traces of the "GUI frozen" reports showed the renderer's timers
// clamped to ~1 Hz (250ms heartbeat firing once per second) with rendering
// suspended, while the page still reported document.visibilityState ===
// "visible" — the signature of the OS throttling the process rather than a
// busy loop or a hung thread. On macOS that is App Nap / activity-based
// timer coalescing. A terminal multiplexer must keep streaming PTY output
// and repainting even when it is not the foreground app, so it asserts a
// user-initiated, latency-critical activity for its whole lifetime.
//
// The Info.plist NSAppSleepDisabled key is the static counterpart; this is
// the stronger programmatic assertion (covers latency/timer coalescing that
// the plist key alone may not). Both are belt-and-braces.
package activity

import "sync"

var once sync.Once

// DisableThrottling asks the OS not to nap/throttle this process. Idempotent
// and safe to call once at startup. No-op on platforms without App Nap.
func DisableThrottling() { once.Do(disableThrottling) }
