//go:build windows

package activity

import (
	"log"
	"syscall"
	"unsafe"
)

// Windows has its own flavour of the App Nap problem: EcoQoS. The scheduler
// watches for processes that are not the foreground window and demotes them to
// "efficiency mode" — parked on E-cores, clocked down. Hive is a standing
// candidate for exactly that treatment: hivegui sits behind whatever the user
// is actually typing into, and hived is spawned DETACHED_PROCESS with no window
// at all (see cmd/hivegui/spawn_windows.go), so it can never be foreground.
// Demoting the daemon costs real latency, because the PTY read → VT parse →
// broadcast path that carries every byte of agent output is single-threaded.
//
// SetProcessInformation with ProcessPowerThrottling is the opt-out. The pairing
// of the fields is the whole API: ControlMask names the policy you are taking
// over, StateMask says what you want it set to. EXECUTION_SPEED in ControlMask
// with a zero StateMask therefore reads "I am managing throttling for this
// process, and I do not want it" — as opposed to both bits set (opt IN to
// EcoQoS) or both zero (hand it back, let Windows decide, which is the default
// Hive was living with).
const (
	// PROCESS_INFORMATION_CLASS.ProcessPowerThrottling.
	processPowerThrottling = 4

	powerThrottlingCurrentVersion = 1
	powerThrottlingExecutionSpeed = 0x1
)

// processPowerThrottlingState mirrors PROCESS_POWER_THROTTLING_STATE from
// processthreadsapi.h: three ULONGs, no padding.
type processPowerThrottlingState struct {
	Version     uint32
	ControlMask uint32
	StateMask   uint32
}

// The repo makes its other Win32 calls through plain syscall (see
// internal/proc/hide_windows.go), and golang.org/x/sys/windows is only an
// indirect dependency that does not wrap SetProcessInformation anyway, so bind
// kernel32 lazily. Lazy matters here: SetProcessInformation arrived in Windows
// 8, and ProcessPowerThrottling in Windows 10 1709. On anything older the proc
// lookup fails at first use rather than at load, and we just carry on.
var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procSetProcessInfo      = kernel32.NewProc("SetProcessInformation")
	procGetCurrentProcessFn = kernel32.NewProc("GetCurrentProcess")
)

func disableThrottling() {
	if err := procSetProcessInfo.Find(); err != nil {
		// Pre-Windows 8. Nothing to opt out of, and nothing to fix.
		log.Printf("activity: SetProcessInformation unavailable (%v); leaving power throttling to Windows", err)
		return
	}
	if err := procGetCurrentProcessFn.Find(); err != nil {
		log.Printf("activity: GetCurrentProcess unavailable (%v); leaving power throttling to Windows", err)
		return
	}
	self, _, _ := procGetCurrentProcessFn.Call()

	state := processPowerThrottlingState{
		Version:     powerThrottlingCurrentVersion,
		ControlMask: powerThrottlingExecutionSpeed,
		StateMask:   0,
	}
	ret, _, err := procSetProcessInfo.Call(
		self,
		uintptr(processPowerThrottling),
		uintptr(unsafe.Pointer(&state)),
		unsafe.Sizeof(state),
	)
	if ret == 0 {
		// Windows 10 before 1709 answers ERROR_INVALID_PARAMETER here: the
		// entry point exists but does not know the class. Either way this is
		// an optimisation, never a reason to fail startup.
		log.Printf("activity: opt out of Windows power throttling: %v", err)
	}
}
