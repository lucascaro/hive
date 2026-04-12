package changelog

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
)

// versionHeader matches lines like "## [0.6.0] — 2026-04-11" or "## [Unreleased]".
var versionHeader = regexp.MustCompile(`^## \[([^\]]+)\]`)

// ParseSince extracts changelog entries for versions released after lastVersion.
// If lastVersion is empty, only the most recent release is returned.
// If lastVersion is not found in the changelog, all released entries are returned.
// The [Unreleased] section is always skipped.
// Returns an empty string if there are no new entries.
func ParseSince(changelog, lastVersion string) string {
	lines := strings.Split(changelog, "\n")

	type section struct {
		version string
		lines   []string
	}

	var sections []section
	var current *section

	for _, line := range lines {
		if m := versionHeader.FindStringSubmatch(line); m != nil {
			ver := m[1]
			if strings.EqualFold(ver, "Unreleased") {
				current = nil
				continue
			}
			sections = append(sections, section{version: ver})
			current = &sections[len(sections)-1]
			current.lines = append(current.lines, line)
			continue
		}
		if current != nil {
			current.lines = append(current.lines, line)
		}
	}

	if len(sections) == 0 {
		return ""
	}

	// If the latest release matches lastVersion, nothing is new.
	if sections[0].version == lastVersion {
		return ""
	}

	// Determine how many sections to include.
	count := len(sections) // default: all (unknown lastVersion)
	if lastVersion == "" {
		count = 1 // first launch: show only the latest
	} else {
		for i, s := range sections {
			if s.version == lastVersion {
				count = i
				break
			}
		}
	}

	if count == 0 {
		return ""
	}

	var out []string
	for _, s := range sections[:count] {
		// Trim trailing blank lines from each section.
		text := strings.TrimRight(strings.Join(s.lines, "\n"), "\n ")
		out = append(out, text)
	}

	return strings.Join(out, "\n\n")
}

// Render converts raw changelog markdown to styled terminal output.
// width sets the wrap width for the rendered output.
// Falls back to the raw text if rendering fails.
func Render(markdown string, width int) string {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return markdown
	}
	rendered, err := r.Render(markdown)
	if err != nil {
		return markdown
	}
	return strings.TrimRight(rendered, "\n")
}
