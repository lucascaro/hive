//go:build windows

package qos

import (
	"log"
	"syscall"
	"unsafe"
)

// SetProcessInformation with ProcessPowerThrottling is the EcoQoS opt-out. The
// pairing of the two masks is the whole API: ControlMask names the policy you
// are taking over, StateMask says what you want it set to. EXECUTION_SPEED in
// ControlMask with a zero StateMask therefore reads "I am managing throttling
// for this process, and I do not want it" — as opposed to both bits set (opt IN
// to EcoQoS) or both zero (hand it back, let Windows decide, which is the
// default Hive was living with: a live audit found ControlMask=0x0
// StateMask=0x0 on both hivegui.exe and hived.exe).
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
// indirect dependency that does not wrap SetProcessInformation at the pinned
// version anyway, so bind kernel32 lazily. Lazy matters here beyond style:
// SetProcessInformation arrived in Windows 8 and ProcessPowerThrottling in
// Windows 10 1709, so on anything older the proc lookup fails at first use
// rather than at load, and we simply carry on.
var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	procSetProcessInfo    = kernel32.NewProc("SetProcessInformation")
	procGetCurrentProcess = kernel32.NewProc("GetCurrentProcess")
)

func disableThrottling() {
	if err := procSetProcessInfo.Find(); err != nil {
		// Pre-Windows 8. Nothing to opt out of, and nothing to fix.
		log.Printf("qos: SetProcessInformation unavailable (%v); leaving power throttling to Windows", err)
		return
	}
	if err := procGetCurrentProcess.Find(); err != nil {
		log.Printf("qos: GetCurrentProcess unavailable (%v); leaving power throttling to Windows", err)
		return
	}
	self, _, _ := procGetCurrentProcess.Call()

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
		log.Printf("qos: opt out of Windows power throttling: %v", err)
	}
}
