package main

import (
	"runtime"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// appOptions builds the whole options.App handed to wails.Run.
//
// Split out of main() so the options are assertable without starting a
// webview — main() itself is unreachable from a test.
func appOptions(a *App, width, height int) *options.App {
	return &options.App{
		Title:            "Hive",
		Width:            width,
		Height:           height,
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 1},
		AssetServer:      &assetserver.Options{Assets: assets},
		Menu:             buildAppMenu(a),
		OnStartup:        a.startup,
		OnShutdown:       a.shutdown,
		Bind:             []interface{}{a},

		// Nothing in Hive handles a dropped file. The frontend's only
		// drag-and-drop is in-page sidebar reordering (drag-placeholder.ts),
		// which is ordinary HTML5 DnD and unaffected by this: on Windows
		// Wails implements it as WebView2's AllowExternalDrag(false), which
		// only refuses drags that originate OUTSIDE the webview. Without it
		// a file dropped on the window is navigated to, replacing the whole
		// app with a view of that file and no way back.
		//
		// Deliberately not GOOS-gated, because it is already inert on
		// macOS: every branch in Wails' WailsWebView.m consults
		// disableWebViewDragAndDrop only after EnableFileDrop has been
		// checked, and we never set EnableFileDrop.
		DragAndDrop: &options.DragAndDrop{DisableWebViewDrop: true},

		// WebView2's own Cut/Copy/Paste menu, which is the only such menu
		// Windows gets: buildAppMenu returns nil on every non-darwin
		// platform (menu_other.go), so today there is no menu-driven copy
		// at all there.
		//
		// GOOS-gated because this option is NOT Windows-scoped — Wails
		// applies it on macOS too (Application.h's defaultContextMenuEnabled)
		// where the native Edit menu already owns Cut/Copy/Paste. Gating
		// leaves the macOS context menu exactly as it is today.
		EnableDefaultContextMenu: runtime.GOOS == "windows",

		Windows: windowsOptions(a),
	}
}

// resizeDebounceMS is how long Wails coalesces WebView2 redraws while a
// resize drag is in flight.
//
// Wails only installs a debouncer at all when this is > 0, so leaving it
// at the zero value means every intermediate size during a drag repaints
// the whole webview — with several xterm canvases live that is visibly
// rough. 24ms is a touch over one frame at 60Hz: long enough to drop the
// storm of intermediate sizes, short enough that the content still tracks
// the window edge rather than lagging behind it.
const resizeDebounceMS = 24

// windowsOptions builds the Wails Windows-platform options.
//
// Kept in untagged (shared) code on purpose. Wails reads options.App.Windows
// only in its Windows frontend and ignores it everywhere else, and
// pkg/options/windows is a single file with no imports and no build tags —
// pure Go constants and structs — so constructing it compiles for darwin and
// linux exactly as it does for Windows. Splitting it behind build tags would
// buy nothing and cost a darwin/linux stub to keep in sync.
//
// Everything here closes a gap that exists because options.App.Windows was
// nil: Wails guards its whole zoom block behind `if opts := ...Windows; opts
// != nil`, so with no struct at all WebView2 kept its own defaults.
func windowsOptions(a *App) *windows.Options {
	return &windows.Options{
		// Hive owns zoom itself: Ctrl +/- drives a font-size token that
		// every xterm instance re-reads. WebView2's page zoom is a second,
		// independent scale factor stacked on top of that, so Ctrl+wheel
		// and pinch used to resize the page out from under the terminals —
		// their canvases and their reported rows/cols disagreed, and the
		// PTY kept being told the pre-zoom geometry.
		//
		// Wails already calls PutAreBrowserAcceleratorKeysEnabled(false)
		// unconditionally, which is why Ctrl+plus/minus were not affected;
		// these two are what close the wheel and trackpad routes.
		IsZoomControlEnabled: false,
		DisablePinchZoom:     true,

		ResizeDebounceMS: resizeDebounceMS,

		// The native titlebar follows the OS.
		//
		// This is the honest ceiling, not a preference: the in-app theme
		// lives in localStorage under `hive.theme` (frontend/src/theme/
		// theme.ts) and is not readable from Go, least of all before the
		// webview exists. SystemDefault is also the right answer for the
		// default install, because the shipped default theme is 'system',
		// which resolves against the same OS setting. An explicitly chosen
		// preset is NOT reflected yet: (*App).SetWindowTheme exists for
		// that push-down but the frontend does not call it.
		Theme: windows.SystemDefault,

		OnResume: a.onSystemResume,
	}
}

// onSystemResume is Wails' OnResume hook, fired when Windows comes back
// from sleep or hibernation (PBT_APMRESUMEAUTOMATIC).
//
// Sleep kills the control connection, but nothing on either side notices
// promptly: the GUI's read loop sits in a blocking read on a pipe whose
// peer is gone, so until it errors the app looks connected and every
// daemon-backed action silently does nothing. Waking is the one moment we
// know for certain to re-check.
//
// It deliberately does NOT call ConnectControl directly. The reconnect
// loop already exists in the frontend (reconnectControl in app/events.ts,
// behind a `_reconnecting` guard with 500ms→5s backoff, and it stands down
// while the daemon is deliberately restarting). Announcing the disconnect
// lets that owner do its job and dedupe against an attempt already in
// flight; dialling from here in parallel would race it, and ConnectControl
// unconditionally tears down and redials rather than no-opping when
// healthy. Emitting into the existing path also means a redial that
// supersedes a still-live connection stays quiet, which controlReadLoop
// and control_swap_test.go already guarantee.
//
// Two constraints from where Wails calls this:
//   - it runs on the Win32 message-pump thread, inside WndProc, so it must
//     not block — hence the goroutine.
//   - a resume can in principle arrive before OnStartup has handed us a
//     context, and EventsEmit on a nil ctx aborts the process.
func (a *App) onSystemResume() {
	if a.ctx == nil {
		return
	}
	go emitFn(a, "control:disconnect", "")
}

// SetWindowTheme points the native window chrome at the app's current
// theme. Windows-only in effect: it drives the titlebar and border colour,
// which are painted by the OS and so know nothing about the webview's CSS.
//
// Like SetDebugTrace, this is a push from the frontend rather than a read
// from Go: the choice lives in localStorage under `hive.theme`, and the
// nineteen presets collapse to dark-or-light only by rules the frontend
// owns ('system' additionally resolving against prefers-color-scheme). Go
// therefore cannot work it out at startup, which is why windowsOptions
// starts at windows.SystemDefault. Nothing in the frontend calls this yet.
//
// Safe to call on macOS and Linux: Wails' non-Windows frontends implement
// WindowSetDarkTheme/WindowSetLightTheme as empty methods, so this is a
// no-op there rather than something needing a GOOS guard.
func (a *App) SetWindowTheme(dark bool) {
	if a.ctx == nil {
		return
	}
	if dark {
		wruntime.WindowSetDarkTheme(a.ctx)
		return
	}
	wruntime.WindowSetLightTheme(a.ctx)
}
