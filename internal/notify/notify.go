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

// SetActivationHandler registers a callback fired when the user
// activates (clicks) a notification. Only darwin can deliver such an
// event, so only darwin stores the callback; elsewhere this is a
// genuine no-op and the handler is never held.
//
// The storage lives behind a platform-suffixed setActivationHandler
// rather than in a shared var, per golden principle 3. When the var
// was shared, it was written here and read only from notify_darwin.go,
// so on Linux and Windows it was dead weight — which staticcheck
// reports as U1000, correctly, and only on those platforms.
func SetActivationHandler(fn func(tag string)) {
	setActivationHandler(fn)
}
