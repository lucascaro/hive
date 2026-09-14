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
	"log"
	"sync"
	"unsafe"
)

// nsUserNotificationActivationTypeContentsClicked is
// NSUserNotificationActivationTypeContentsClicked from
// Foundation/NSUserNotification.h (value confirmed against the macOS
// SDK header: None=0, ContentsClicked=1, ActionButtonClicked=2,
// Replied=3, AdditionalActionClicked=4). Only this one means "the user
// clicked the banner body", which is the one activation the rest of
// Hive treats as "go to this session".
const nsUserNotificationActivationTypeContentsClicked = 1

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
func hiveOnNotificationActivated(tag *C.char, activationType C.long) {
	tagStr := C.GoString(tag)
	log.Printf("notify: activation tag=%q type=%d", tagStr, activationType)
	// Only a contents-click means "take me there"; ObjC still gates
	// activateIgnoringOtherApps the same way, so this mirrors that
	// decision on the Go side rather than widening what the rest of
	// Hive reacts to.
	if activationType != nsUserNotificationActivationTypeContentsClicked {
		return
	}
	cbMu.RLock()
	fn := activationCallback
	cbMu.RUnlock()
	if fn != nil {
		fn(tagStr)
	}
}
