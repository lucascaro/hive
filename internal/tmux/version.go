package tmux

import (
	"fmt"
	"strings"
)

// Version returns the major and minor version of the installed tmux binary.
// For example, "tmux 3.4" returns (3, 4, nil).
func Version() (major, minor int, err error) {
	out, err := Exec("-V")
	if err != nil {
		return 0, 0, fmt.Errorf("tmux -V: %w", err)
	}
	// Expected format: "tmux 3.4" or "tmux 3.4a" (letter suffixes are ignored).
	out = strings.TrimSpace(out)
	var name string
	var vstr string
	if _, scanErr := fmt.Sscanf(out, "%s %s", &name, &vstr); scanErr != nil {
		return 0, 0, fmt.Errorf("parse tmux version %q: %w", out, scanErr)
	}
	// Strip any trailing non-numeric suffix (e.g. "3.4a" → "3.4").
	cleaned := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r == '.' {
			return r
		}
		return -1
	}, vstr)
	if _, scanErr := fmt.Sscanf(cleaned, "%d.%d", &major, &minor); scanErr != nil {
		return 0, 0, fmt.Errorf("parse tmux version numbers %q: %w", cleaned, scanErr)
	}
	return major, minor, nil
}

// SupportsDisplayPopup reports whether the installed tmux supports
// the display-popup command (requires tmux ≥ 3.2).
func SupportsDisplayPopup() bool {
	major, minor, err := Version()
	if err != nil {
		return false
	}
	return major > 3 || (major == 3 && minor >= 2)
}
