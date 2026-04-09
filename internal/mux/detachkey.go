package mux

import (
	"fmt"
	"strings"
)

// DefaultDetachKey is the default key combination used to detach from an
// attached session and return to the Hive TUI.
const DefaultDetachKey = "ctrl+q"

// DetachKeySpec describes a configured detach shortcut in the three forms
// the rest of the codebase needs:
//
//   - Display: human-readable form for help screens and status bars (e.g. "Ctrl+Q").
//   - Tmux:    tmux bind-key syntax used to install a server-side binding (e.g. "C-q").
//   - Byte:    raw control byte the native PTY client intercepts on stdin (e.g. 0x11).
//
// Only single-letter Ctrl combinations are supported in v1 (e.g. "ctrl+q",
// "ctrl+d"). Alt, function keys, and multi-key sequences are intentionally
// rejected by ParseDetachKey.
type DetachKeySpec struct {
	Raw     string // canonical config form, e.g. "ctrl+q"
	Display string // user-facing, e.g. "Ctrl+Q"
	Tmux    string // tmux bind-key syntax, e.g. "C-q"
	Byte    byte   // ASCII control byte, e.g. 0x11
}

// ParseDetachKey parses a config-string detach key and returns the matching
// DetachKeySpec. The accepted form is "ctrl+<lowercase-letter>" with the
// letter in the range a-z.
//
// Only ctrl+<letter> is supported in v1; alt, uppercase letters, function
// keys, and multi-key sequences return a descriptive error so misconfigured
// users get immediate feedback at startup.
func ParseDetachKey(s string) (DetachKeySpec, error) {
	if s == "" {
		return DetachKeySpec{}, fmt.Errorf("detach key is empty")
	}
	parts := strings.Split(s, "+")
	if len(parts) != 2 {
		return DetachKeySpec{}, fmt.Errorf("detach key %q must be of the form ctrl+<letter>", s)
	}
	mod, key := parts[0], parts[1]
	if mod != "ctrl" {
		return DetachKeySpec{}, fmt.Errorf("detach key %q: only ctrl+<letter> is supported (got modifier %q)", s, mod)
	}
	if len(key) != 1 {
		return DetachKeySpec{}, fmt.Errorf("detach key %q: key part must be a single letter (got %q)", s, key)
	}
	c := key[0]
	if c < 'a' || c > 'z' {
		return DetachKeySpec{}, fmt.Errorf("detach key %q: letter must be lowercase a-z (got %q)", s, key)
	}
	return DetachKeySpec{
		Raw:     s,
		Display: "Ctrl+" + strings.ToUpper(key),
		Tmux:    "C-" + key,
		Byte:    c - 'a' + 1,
	}, nil
}
