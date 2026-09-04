//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -mmacosx-version-min=11.0
#cgo LDFLAGS: -framework Foundation -framework ServiceManagement
#import <Foundation/Foundation.h>
#import <ServiceManagement/ServiceManagement.h>

// hive_login_item_status returns SMAppServiceStatus for the embedded
// menu-bar helper, or -1 when the OS is too old to have SMAppService.
//
//   0 SMAppServiceStatusNotRegistered
//   1 SMAppServiceStatusEnabled
//   2 SMAppServiceStatusRequiresApproval
//   3 SMAppServiceStatusNotFound
static int hive_login_item_status(const char *identifier) {
  if (@available(macOS 13.0, *)) {
    NSString *ident = [NSString stringWithUTF8String:identifier];
    SMAppService *svc = [SMAppService loginItemServiceWithIdentifier:ident];
    return (int)svc.status;
  }
  return -1;
}

// hive_login_item_set registers or unregisters the helper. Returns NULL
// on success, or a strdup'd error description the caller must free.
//
// The error string matters more here than in most cgo shims: on an
// unsigned build this call is expected to fail, and the user needs to
// see WHY rather than a silent toggle that springs back.
static char *hive_login_item_set(const char *identifier, int enable) {
  if (@available(macOS 13.0, *)) {
    NSString *ident = [NSString stringWithUTF8String:identifier];
    SMAppService *svc = [SMAppService loginItemServiceWithIdentifier:ident];
    NSError *err = nil;
    BOOL ok = enable ? [svc registerAndReturnError:&err]
                     : [svc unregisterAndReturnError:&err];
    if (!ok) {
      NSString *desc = err ? [err localizedDescription] : @"unknown error";
      if (err) {
        desc = [NSString stringWithFormat:@"%@ (%@ %ld)",
                desc, [err domain], (long)[err code]];
      }
      return strdup([desc UTF8String]);
    }
    return NULL;
  }
  return strdup("login items require macOS 13 or later");
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// menuBarBundleID is hivebar's CFBundleIdentifier, and must match
// cmd/hivebar/build/darwin/Info.plist. SMAppService addresses the
// helper by identifier, so a mismatch is a silent "not found" rather
// than a build error.
const menuBarBundleID = "com.wails.hivegui.hivebar"

// MenuBarLoginItemStatus reports whether the menu bar is registered to
// start at login: "enabled", "not-registered", "requires-approval",
// "not-found", or "unsupported" on macOS 12 and earlier.
//
// "not-registered" is the default and is not an error — the menu bar
// still runs, started by hived and hivegui (internal/menubar) rather
// than by launchd. Registering only adds a menu bar at login before
// either of them has run.
//
// "not-found" means macOS cannot see a helper with menuBarBundleID
// inside this app. Reachable in a dev tree: SMAppService resolves the
// identifier against the CALLING process's bundle, so a hivegui run
// outside a .app has no Contents/Library/LoginItems to look in.
func (a *App) MenuBarLoginItemStatus() string {
	ident := C.CString(menuBarBundleID)
	defer C.free(unsafe.Pointer(ident))
	switch C.hive_login_item_status(ident) {
	case 0:
		return "not-registered"
	case 1:
		return "enabled"
	case 2:
		return "requires-approval"
	case 3:
		return "not-found"
	default:
		return "unsupported"
	}
}

// SetMenuBarLoginItem registers or unregisters hivebar as a login item.
//
// This works on the ad-hoc-signed bundles build.sh currently produces —
// verified against a real build, which is worth stating because the
// received wisdom says otherwise. Registering a helper in
// Contents/Library/LoginItems is widely reported to need a Developer ID
// and a matching Team ID, and an early cut of this code was written
// expecting the failure. It does not: LaunchServices accepted the
// registration from the bundle (status NotFound -> Enabled) with no
// signing identity at all.
//
// What DOES matter is the caller's bundle. SMAppService resolves the
// identifier relative to the calling process, so this succeeds from
// hivegui inside Hive.app and fails with "Invalid argument" from a bare
// binary that has no LoginItems directory — which is what a dev-tree
// `wails dev` run is.
//
// The error is returned verbatim rather than flattened into "could not
// enable": the failures here are environmental (wrong bundle, macOS too
// old, approval pending) and the user can act on none of them without
// being told which.
//
// The menu bar works either way: hived and hivegui both start it on
// boot (internal/menubar). What this buys is a menu bar at login before
// either of them has run.
func (a *App) SetMenuBarLoginItem(enable bool) error {
	ident := C.CString(menuBarBundleID)
	defer C.free(unsafe.Pointer(ident))
	on := C.int(0)
	if enable {
		on = 1
	}
	cerr := C.hive_login_item_set(ident, on)
	if cerr == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(cerr))
	return fmt.Errorf("%s", C.GoString(cerr))
}
