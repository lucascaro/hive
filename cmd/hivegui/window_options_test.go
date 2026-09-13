package main

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// The zoom pair is the whole reason options.App.Windows has to exist at
// all: Wails guards PutIsZoomControlEnabled / PutIsPinchZoomEnabled behind
// `if opts := frontendOptions.Windows; opts != nil`, so a nil struct leaves
// WebView2 on its own defaults (both zooms live) no matter what we would
// have wanted. False here is therefore load-bearing, not a zero value left
// alone — hence asserting it.
func TestWindowsOptionsDisablesWebviewZoom(t *testing.T) {
	o := windowsOptions(NewApp(""))
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
	o := windowsOptions(NewApp(""))
	// Wails installs a debouncer only for a value > 0; 0 means every
	// intermediate size during a resize drag repaints the webview.
	if o.ResizeDebounceMS == 0 {
		t.Fatal("ResizeDebounceMS = 0; Wails installs no resize debouncer at all")
	}
	if o.ResizeDebounceMS != resizeDebounceMS {
		t.Errorf("ResizeDebounceMS = %d, want %d", o.ResizeDebounceMS, resizeDebounceMS)
	}
}

// SystemDefault is the only value derivable before the webview exists: the
// preset lives in the frontend's localStorage, which Go cannot read. It is
// also the correct answer for a default install, whose shipped theme is
// 'system'. Pinned so a future edit to Dark/Light has to justify itself.
func TestWindowsOptionsThemeFollowsSystem(t *testing.T) {
	if got := windowsOptions(NewApp("")).Theme; got != windows.SystemDefault {
		t.Errorf("Theme = %v, want windows.SystemDefault (%v)", got, windows.SystemDefault)
	}
}

func TestWindowsOptionsWiresResume(t *testing.T) {
	if windowsOptions(NewApp("")).OnResume == nil {
		t.Error("OnResume = nil; a control connection killed by sleep would sit undetected until the next read errors")
	}
}

// A dropped file has no handler anywhere in Hive, and an unhandled drop
// navigates the webview to the file — the app is replaced by a view of it
// with no way back.
func TestAppOptionsDisablesWebviewFileDrop(t *testing.T) {
	o := appOptions(NewApp(""), 1024, 700)
	if o.DragAndDrop == nil {
		t.Fatal("DragAndDrop = nil; the webview accepts file drops nothing handles")
	}
	if !o.DragAndDrop.DisableWebViewDrop {
		t.Error("DisableWebViewDrop = false; a dropped file navigates the webview away from the app")
	}
	// EnableFileDrop must stay false: it is what makes DisableWebViewDrop
	// inert on macOS (WailsWebView.m checks it first in every branch), and
	// turning it on would start posting drop messages nothing listens for.
	if o.DragAndDrop.EnableFileDrop {
		t.Error("EnableFileDrop = true; nothing consumes Wails' file-drop messages")
	}
}

// Gated, not unconditional: Wails applies EnableDefaultContextMenu on macOS
// too, where buildAppMenu already supplies a native Edit menu with
// Cut/Copy/Paste. Windows gets no native menu at all (menu_other.go), so
// the webview menu is its only one.
func TestAppOptionsContextMenuIsWindowsOnly(t *testing.T) {
	o := appOptions(NewApp(""), 1024, 700)
	want := runtime.GOOS == "windows"
	if o.EnableDefaultContextMenu != want {
		t.Errorf("EnableDefaultContextMenu = %v on %s, want %v",
			o.EnableDefaultContextMenu, runtime.GOOS, want)
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

// onSystemResume must announce the disconnect rather than dial itself, so
// the frontend's existing guarded reconnect loop owns the retry.
func TestOnSystemResumeAnnouncesDisconnect(t *testing.T) {
	prev := emitFn
	t.Cleanup(func() { emitFn = prev })

	got := make(chan string, 4)
	emitFn = func(_ *App, name string, _ ...any) { got <- name }

	a := NewApp("")
	a.ctx = context.Background()
	a.onSystemResume()

	select {
	case name := <-got:
		if name != "control:disconnect" {
			t.Errorf("emitted %q, want %q", name, "control:disconnect")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onSystemResume emitted nothing; a connection killed by sleep would never be redialled")
	}
}

// Wails calls OnResume from WndProc on the message-pump thread. A resume
// can arrive before OnStartup has handed us a context, and EventsEmit on a
// nil context aborts the process rather than returning an error.
func TestOnSystemResumeWithoutContext(t *testing.T) {
	prev := emitFn
	t.Cleanup(func() { emitFn = prev })

	emitFn = func(*App, string, ...any) {
		t.Error("emitted with a nil ctx; EventsEmit would abort the process")
	}

	a := NewApp("") // ctx deliberately left nil
	a.onSystemResume()
	// The emit is spawned in a goroutine, so give a regression a chance to
	// land before the test returns.
	time.Sleep(50 * time.Millisecond)
}

func TestSetWindowThemeWithoutContext(t *testing.T) {
	a := NewApp("") // ctx deliberately left nil
	// Both directions: a nil ctx must be a no-op, not a panic inside the
	// Wails runtime's context lookup.
	a.SetWindowTheme(true)
	a.SetWindowTheme(false)
}
