// Package activity opts the GUI process out of OS-level throttling.
//
// This is DEFENSIVE hygiene, not the fix for the "GUI frozen" reports — that
// root cause was a synchronous full-ring scrollback replay (see the alt-screen
// replay skip in session-term.ts and ringCap in internal/session/vt.go). But
// macOS App Nap / activity-based timer coalescing can still clamp a
// backgrounded webview's timers, and a terminal multiplexer must keep
// streaming PTY output and repainting even when it is not the foreground app,
// so it asserts a user-initiated, latency-critical activity for its whole
// lifetime. The Info.plist NSAppSleepDisabled key is the static counterpart;
// this is the stronger programmatic assertion. Both are belt-and-braces.
//
// Windows has the same shape of problem under a different name: EcoQoS parks
// processes that are never the foreground window onto E-cores, which describes
// both hivegui and the windowless detached hived exactly. See
// activity_windows.go.
package activity

import "sync"

var once sync.Once

// DisableThrottling asks the OS not to nap/throttle this process. Idempotent
// and safe to call once at startup. Best-effort everywhere: a platform with no
// such knob, or one too old to have the API, is a no-op, never an error.
func DisableThrottling() { once.Do(disableThrottling) }
