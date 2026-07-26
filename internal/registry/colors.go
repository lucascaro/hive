package registry

import (
	"math/rand/v2"
	"slices"
)

// colorPalette is the curated set of "good" hues used for auto-
// assigned project and session colors. All are readable as text on
// the dark sidebar. Users can override via Update.
var colorPalette = []string{
	"#f59e0b", // amber
	"#f97316", // orange
	"#ef4444", // red
	"#ec4899", // pink
	"#d946ef", // fuchsia
	"#a855f7", // purple
	"#8b5cf6", // violet
	"#6366f1", // indigo
	"#3b82f6", // sky
	"#06b6d4", // cyan
	"#14b8a6", // teal
	"#10b981", // emerald
	"#84cc16", // lime
	"#eab308", // yellow
}

// pickColor returns a random palette color, excluding any entry
// listed in avoid. Uses math/rand/v2 top-level helpers so it's
// goroutine-safe without needing a lock at the call site.
func pickColor(avoid ...string) string {
	n := len(colorPalette)
	idx := rand.IntN(n)
	for i := range n {
		c := colorPalette[(idx+i)%n]
		if !slices.Contains(avoid, c) {
			return c
		}
	}
	// Every palette entry is in avoid — fall back to a uniform pick.
	return colorPalette[idx]
}
