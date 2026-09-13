---
type: fixed
bump: patch
---

Ctrl+wheel and trackpad pinch no longer zoom the page out from under the
terminals on Windows. Hive has always owned zoom itself — Ctrl +/- drives a
font size every xterm re-reads — but the GUI never passed Wails any
Windows-platform options, so WebView2 kept its own page zoom running
alongside. The two stacked: pinching resized the page without telling the
terminals, leaving their canvases and their reported rows and columns
disagreeing, and the shell drawing to the wrong geometry.

The same missing options block cost Windows four other things, now fixed:
right-click offers the webview's Cut/Copy/Paste menu, which is the only such
menu Windows has (the native menu bar is macOS-only, so there was previously
no menu-driven copy at all); resizing the window with several terminals open
is debounced instead of repainting at every intermediate size; waking from
sleep re-establishes the daemon connection rather than leaving a dead one
that looks alive until the next command silently does nothing; and the
titlebar follows the system light/dark setting.

Dropping a file on the window is also ignored now, on every platform.
Nothing in Hive consumes a dropped file, and the webview's default was to
navigate to it — replacing the whole app with a view of that file and no way
back. Dragging sessions around the sidebar is unaffected.
