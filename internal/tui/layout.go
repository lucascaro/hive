package tui

const (
	sidebarRatio    = 0.28
	sidebarMinWidth = 32
	sidebarMaxWidth = 48
	statusBarHeight = 2
	minTermWidth    = 60
	minTermHeight   = 15
)

// computeLayout returns sidebarWidth and previewWidth for the given terminal dimensions.
// activityHeight is the number of rows reserved for the preview-activity panel
// (below main, above the status bar). Pass 0 when the panel is not rendered.
func computeLayout(termWidth, termHeight, activityHeight int) (sidebarWidth, previewWidth, contentHeight int) {
	// Use safe defaults until WindowSizeMsg arrives.
	if termWidth <= 0 {
		termWidth = 80
	}
	if termHeight <= 0 {
		termHeight = 24
	}

	sw := int(float64(termWidth) * sidebarRatio)
	if sw < sidebarMinWidth {
		sw = sidebarMinWidth
	}
	if sw > sidebarMaxWidth {
		sw = sidebarMaxWidth
	}
	// When terminal is very narrow, collapse sidebar to minimum.
	if termWidth < minTermWidth {
		sw = 0
	}
	pw := termWidth - sw
	if pw < 0 {
		pw = 0
	}
	ch := termHeight - statusBarHeight - activityHeight
	if ch < 1 {
		ch = 1
	}
	return sw, pw, ch
}
