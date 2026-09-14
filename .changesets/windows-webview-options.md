---
type: fixed
bump: patch
pr: 405
---

Ctrl+wheel and trackpad pinch no longer zoom the page out from under the
terminals on Windows. Hive has always owned zoom itself — Ctrl +/- drives a
font size every xterm re-reads — but the GUI never passed Wails any
Windows-platform options, so WebView2 kept its own page zoom running
alongside. The two stacked: pinching resized the page without telling the
terminals, leaving their canvases and their reported rows and columns
disagreeing, and the shell drawing to the wrong geometry.

Two more Windows fixes ride on the same options block: resizing the window
with several terminals open is debounced instead of repainting at every
intermediate size, and dropping a file on the window is ignored, where before
the webview navigated to it — replacing the whole app with a view of that file
and no way back. Dragging sessions around the sidebar is unaffected. macOS and
Linux behave exactly as they did.
