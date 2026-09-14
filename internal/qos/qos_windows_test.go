//go:build windows

package qos

import (
	"errors"
	"syscall"
	"testing"
	"unsafe"
)

// Reads the state back through GetProcessInformation so a wrong info class,
// struct layout, or mask pairing fails here instead of degrading silently
// into a log line at startup.
func TestDisableThrottlingOptsOutOfEcoQoS(t *testing.T) {
	getInfo := syscall.NewLazyDLL("kernel32.dll").NewProc("GetProcessInformation")
	if err := getInfo.Find(); err != nil {
		t.Skipf("GetProcessInformation unavailable: %v", err)
	}
	if err := procSetProcessInfo.Find(); err != nil {
		t.Skipf("SetProcessInformation unavailable: %v", err)
	}

	DisableThrottling()

	self, err := syscall.GetCurrentProcess()
	if err != nil {
		t.Fatalf("GetCurrentProcess: %v", err)
	}
	got := processPowerThrottlingState{Version: powerThrottlingCurrentVersion}
	ret, _, callErr := getInfo.Call(
		uintptr(self),
		uintptr(processPowerThrottling),
		uintptr(unsafe.Pointer(&got)),
		unsafe.Sizeof(got),
	)
	if ret == 0 {
		// Builds that predate the class answer ERROR_INVALID_PARAMETER.
		if errors.Is(callErr, syscall.Errno(87)) {
			t.Skipf("ProcessPowerThrottling not queryable on this build: %v", callErr)
		}
		t.Fatalf("GetProcessInformation: %v", callErr)
	}
	if got.Version != powerThrottlingCurrentVersion ||
		got.ControlMask&powerThrottlingExecutionSpeed == 0 ||
		got.StateMask&powerThrottlingExecutionSpeed != 0 {
		t.Fatalf("throttling state = %+v, want Version=1 ControlMask&1=1 StateMask&1=0", got)
	}
}
