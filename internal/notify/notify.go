// Package notify fires native OS notifications from the GUI.
//
// Wails uses WKWebView on macOS and an embedded webview on Linux/Windows;
// none of these expose the HTML5 Notification API reliably. We dispatch
// per-platform from Go instead.
//
// On macOS notifications fire from the running app's bundle, so they
// pick up the Hive icon automatically and clicks route back through
// the registered activation handler.
package notify

import "sync"

// Notify shows a native OS notification. tag is platform-specific:
//   - darwin: surfaces back to SetActivationHandler when the user clicks
//     the notification, and dedupes repeated notifications with the same
//     id at the OS level.
//   - linux/windows: currently advisory only.
//
// Errors from the underlying platform mechanism are returned but
// callers should treat notifications as best-effort.
func Notify(title, subtitle, body, tag string) error {
	return platformNotify(title, subtitle, body, tag)
}

var (
	cbMu               sync.RWMutex
	activationCallback func(tag string)
)

// SetActivationHandler registers a callback fired when the user
// activates (clicks) a notification. Currently wired only on darwin.
// On other platforms this is a no-op record-keeping call — safe to
// always invoke from the app.
func SetActivationHandler(fn func(tag string)) {
	cbMu.Lock()
	activationCallback = fn
	cbMu.Unlock()
}
