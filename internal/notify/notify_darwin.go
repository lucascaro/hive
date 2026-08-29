package notify

/*
#cgo darwin CFLAGS: -x objective-c -fobjc-arc
#cgo darwin LDFLAGS: -framework Cocoa
#include <stdlib.h>

void hiveInstallNotifyDelegate(void);
void hivePostNotification(const char *title,
                          const char *subtitle,
                          const char *body,
                          const char *tag);
*/
import "C"

import (
	"sync"
	"unsafe"
)

var delegateOnce sync.Once

func ensureDelegate() {
	delegateOnce.Do(func() { C.hiveInstallNotifyDelegate() })
}

func platformNotify(title, subtitle, body, tag string) error {
	ensureDelegate()
	cTitle := C.CString(title)
	cSubtitle := C.CString(subtitle)
	cBody := C.CString(body)
	cTag := C.CString(tag)
	defer C.free(unsafe.Pointer(cTitle))
	defer C.free(unsafe.Pointer(cSubtitle))
	defer C.free(unsafe.Pointer(cBody))
	defer C.free(unsafe.Pointer(cTag))
	C.hivePostNotification(cTitle, cSubtitle, cBody, cTag)
	return nil
}

// Activation state lives here because darwin is the only platform that
// can produce an activation event — see SetActivationHandler.
var (
	cbMu               sync.RWMutex
	activationCallback func(tag string)
)

func setActivationHandler(fn func(tag string)) {
	cbMu.Lock()
	activationCallback = fn
	cbMu.Unlock()
}

//export hiveOnNotificationActivated
func hiveOnNotificationActivated(tag *C.char) {
	cbMu.RLock()
	fn := activationCallback
	cbMu.RUnlock()
	if fn != nil {
		fn(C.GoString(tag))
	}
}
