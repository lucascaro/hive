package main

import (
	"runtime"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// The zoom pair is the whole reason options.App.Windows has to exist at
// all: Wails guards PutIsZoomControlEnabled / PutIsPinchZoomEnabled behind
// `if opts := frontendOptions.Windows; opts != nil`, so a nil struct leaves
// WebView2 on its own defaults (both zooms live) no matter what we would
// have wanted. False here is therefore load-bearing, not a zero value left
// alone — hence asserting it.
func TestWindowsOptionsDisablesWebviewZoom(t *testing.T) {
	o := windowsOptions()
	if o == nil {
		t.Fatal("windowsOptions returned nil; Wails skips its whole Windows block on nil")
	}
	if o.IsZoomControlEnabled {
		t.Error("IsZoomControlEnabled = true; Ctrl+wheel would apply a WebView2 page zoom on top of the app's own font zoom")
	}
	if !o.DisablePinchZoom {
		t.Error("DisablePinchZoom = false; trackpad pinch would desync every xterm from its PTY geometry")
	}
}

func TestWindowsOptionsResizeDebounce(t *testing.T) {
	o := windowsOptions()
	// Wails installs a debouncer only for a value > 0; 0 means every
	// intermediate size during a resize drag repaints the webview.
	if o.ResizeDebounceMS == 0 {
		t.Fatal("ResizeDebounceMS = 0; Wails installs no resize debouncer at all")
	}
	if o.ResizeDebounceMS != resizeDebounceMS {
		t.Errorf("ResizeDebounceMS = %d, want %d", o.ResizeDebounceMS, resizeDebounceMS)
	}
}

// SystemDefault is what Wails already used for a nil Windows struct, and
// the only value derivable before the webview exists: the preset lives in
// the frontend's localStorage, which Go cannot read. Pinned so a future
// edit to Dark/Light has to justify itself — the in-app theme picker would
// disagree with it for every user who chose the other family.
func TestWindowsOptionsThemeFollowsSystem(t *testing.T) {
	if got := windowsOptions().Theme; got != windows.SystemDefault {
		t.Errorf("Theme = %v, want windows.SystemDefault (%v)", got, windows.SystemDefault)
	}
}

// No sleep/wake hook, deliberately. The only thing it could do is emit
// control:disconnect, whose frontend handler paints the red
// "reconnecting…" status and calls ConnectControl — which closes the
// current connection before redialling, healthy or not. A hook there
// would turn every wake into a dropped connection and a flashed error.
// If a wake really does kill the pipe, controlReadLoop already announces
// it when its read fails.
func TestWindowsOptionsLeavesResumeUnwired(t *testing.T) {
	if windowsOptions().OnResume != nil {
		t.Error("OnResume is set; every wake would tear down a healthy control connection via ConnectControl")
	}
}

// A dropped file has no handler anywhere in Hive, and an unhandled drop
// navigates the webview to the file — the app is replaced by a view of it
// with no way back.
//
// Windows only: there the option maps to WebView2's AllowExternalDrag,
// which leaves in-page HTML5 drags alone. On Linux Wails unsets the
// webview as a GTK drop target altogether, and whether the sidebar's
// in-page reorder survives that is unverified — so it stays off there.
func TestAppOptionsDisablesWebviewFileDrop(t *testing.T) {
	o := appOptions(NewApp(""), 1024, 700)
	if o.DragAndDrop == nil {
		t.Fatal("DragAndDrop = nil; the webview accepts file drops nothing handles")
	}
	want := runtime.GOOS == "windows"
	if o.DragAndDrop.DisableWebViewDrop != want {
		t.Errorf("DisableWebViewDrop = %v on %s, want %v",
			o.DragAndDrop.DisableWebViewDrop, runtime.GOOS, want)
	}
	// EnableFileDrop must stay false: it is what makes DisableWebViewDrop
	// inert on macOS (WailsWebView.m checks it first in every branch), and
	// turning it on would start posting drop messages nothing listens for.
	if o.DragAndDrop.EnableFileDrop {
		t.Error("EnableFileDrop = true; nothing consumes Wails' file-drop messages")
	}
}

// Off on every platform, and pinned rather than merely left alone: Wails
// disables the webview's own context menu in release builds unless this is
// set, and turning it on hands the user WebView2's page menu — the
// frontend registers no contextmenu handler, and xterm draws to a canvas,
// so a right-click over a terminal gets Back/Refresh/Save as/Print, where
// Refresh reloads the page and drops every window's state. A Copy/Paste
// menu Hive actually owns is a frontend feature, not a Wails flag.
func TestAppOptionsKeepsWebviewContextMenuOff(t *testing.T) {
	o := appOptions(NewApp(""), 1024, 700)
	if o.EnableDefaultContextMenu {
		t.Error("EnableDefaultContextMenu = true; right-click over a terminal would offer WebView2's Refresh, which reloads the app")
	}
}

// The geometry restored in main() has to survive the extraction into
// appOptions, since that is the only thing sizing the window at startup.
func TestAppOptionsCarriesGeometry(t *testing.T) {
	o := appOptions(NewApp(""), 1280, 800)
	if o.Width != 1280 || o.Height != 800 {
		t.Errorf("size = %dx%d, want 1280x800", o.Width, o.Height)
	}
	if o.Title != "Hive" {
		t.Errorf("Title = %q, want %q", o.Title, "Hive")
	}
	if o.Windows == nil {
		t.Error("Windows = nil; the Windows options never reach Wails")
	}
}
