package escape

import (
	"regexp"
	"strings"
)

// oscTitleRE matches OSC 2 window title sequences: ESC ] 2 ; <title> BEL
var oscTitleRE = regexp.MustCompile(`\x1b\]2;([^\x07\x1b]*)\x07`)

// oscSeqRE matches any OSC sequence (used to strip them before bell detection).
var oscSeqRE = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// DetectBell reports whether raw pane output contains a standalone BEL character
// (i.e. one that is not the terminator of an OSC sequence).
func DetectBell(raw string) bool {
	stripped := oscSeqRE.ReplaceAllString(raw, "")
	return strings.ContainsRune(stripped, '\x07')
}

// nullMarkerRE matches the custom null-byte marker: \x00HIVE_TITLE:{title}\x00
var nullMarkerRE = regexp.MustCompile(`\x00HIVE_TITLE:([^\x00]+)\x00`)

// ExtractTitle returns the last title set in raw pane output, or "" if none found.
// It checks both the OSC 2 sequence and the custom null-byte marker.
func ExtractTitle(raw string) string {
	// Prefer null-byte marker (explicit, agent-agnostic)
	if matches := nullMarkerRE.FindAllStringSubmatch(raw, -1); len(matches) > 0 {
		last := matches[len(matches)-1]
		if len(last) > 1 {
			return last[1]
		}
	}
	// Fall back to OSC 2
	if matches := oscTitleRE.FindAllStringSubmatch(raw, -1); len(matches) > 0 {
		last := matches[len(matches)-1]
		if len(last) > 1 {
			return last[1]
		}
	}
	return ""
}
