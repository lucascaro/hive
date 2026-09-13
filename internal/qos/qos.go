// Package qos opts a process out of OS quality-of-service throttling.
//
// This is the sibling of internal/activity, split out for one reason: it must
// be importable by every Hive binary, and internal/activity is not. That
// package's macOS implementation is cgo (-framework Foundation), but build.sh
// cross-compiles hived for darwin/amd64 and darwin/arm64 from a single host
// with cgo off and lipo's the slices together — so the daemon, the process
// that most needs the opt-out, is exactly the one that cannot import it. qos is
// cgo-free on every platform by construction, so hived can call it.
//
// The problem it solves is Windows EcoQoS. Windows demotes processes that are
// not the foreground window onto the efficiency cores, and Hive is a standing
// candidate: hivegui sits behind whatever the user is actually typing into, and
// hived is spawned DETACHED_PROCESS with no window at all (see
// cmd/hivegui/spawn_windows.go), so it can never be foreground. The daemon is
// where that hurts, because the PTY read → VT parse → broadcast path carrying
// every byte of agent output is single-threaded: a demotion to E-cores lands
// directly on keystroke-to-paint latency.
//
// Nothing here may import internal/activity, and internal/activity does not
// import this. On macOS the two are complementary — App Nap and EcoQoS are
// different mechanisms — and the GUI calls both.
package qos

import "sync"

var once sync.Once

// DisableThrottling asks the OS not to throttle this process' execution speed.
// Idempotent and safe to call once at startup. Best-effort everywhere: a
// platform with no such knob, or one too old to have the API, is a no-op. This
// is an optimisation and must never block startup.
func DisableThrottling() { once.Do(disableThrottling) }
