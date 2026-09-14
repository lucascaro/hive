package main

import (
	"runtime"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
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

		// Nothing in Hive handles a dropped file, and without this a file
		// dropped on the window is navigated to, replacing the whole app
		// with a view of that file and no way back.
		//
		// Windows only. There Wails implements it as WebView2's
		// AllowExternalDrag(false), which refuses only drags that
		// originate OUTSIDE the webview, so the frontend's one
		// drag-and-drop — in-page sidebar reordering (drag-placeholder.ts),
		// ordinary HTML5 DnD — is unaffected. On macOS the option is inert
		// anyway: every branch in Wails' WailsWebView.m consults
		// disableWebViewDragAndDrop only after enableDragAndDrop, which we
		// never set. On Linux it is NOT inert — Wails calls
		// gtk_drag_dest_unset on the webview, removing it as a GTK drop
		// target outright — and whether in-page HTML5 drops survive that is
		// unverified, so Linux is left exactly as it was.
		//
		// EnableDefaultContextMenu is deliberately left false everywhere.
		// Wails already turns the webview's own menu off in release builds,
		// and turning it on would hand Windows users WebView2's page menu:
		// the frontend registers no contextmenu handler and xterm draws to
		// a canvas, so a right-click over a terminal gets
		// Back/Refresh/Save as/Print, where Refresh reloads the page and
		// drops the window's state. A Copy/Paste menu Hive owns is a
		// frontend feature, not a Wails flag.
		DragAndDrop: &options.DragAndDrop{
			DisableWebViewDrop: runtime.GOOS == "windows",
		},

		Windows: windowsOptions(),
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
// The zoom pair and the debounce close gaps that existed because
// options.App.Windows was nil: Wails guards its whole zoom block behind
// `if opts := ...Windows; opts != nil`, so with no struct at all WebView2
// kept its own defaults. Theme is different — see its comment.
func windowsOptions() *windows.Options {
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

		// The native titlebar follows the OS. This is what Wails already
		// did for a nil Windows struct (window.go falls back to
		// SystemDefault), so it changes nothing; it is written out so the
		// choice is visible and pinned.
		//
		// It is also the ceiling, not a preference: the in-app theme
		// lives in localStorage under `hive.theme` (frontend/src/theme/
		// theme.ts) and is not readable from Go, least of all before the
		// webview exists. SystemDefault matches the default install, whose
		// shipped theme is 'system' and resolves against the same OS
		// setting. Making the titlebar track an explicitly chosen preset
		// would need the frontend to push the choice down after boot; that
		// is not done here.
		Theme: windows.SystemDefault,

		// No OnResume hook, deliberately. The only useful thing it could do
		// is announce control:disconnect, and that event's frontend handler
		// paints the red "reconnecting…" status and calls ConnectControl,
		// which closes the current connection before redialling whether or
		// not it was healthy — so a hook would turn every wake into a
		// dropped connection and a flashed error. If a wake really does
		// kill the pipe, controlReadLoop announces it when its read fails.
	}
}
