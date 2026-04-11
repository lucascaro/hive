package changelog

import (
	"strings"
	"testing"
)

const testChangelog = `# Changelog

## [Unreleased]

### Added
- Something in progress

## [0.6.0] — 2026-04-11

### Added
- Reorder items via keyboard

### Fixed
- Terminal bell forwarding

## [0.5.1] — 2026-04-10

### Fixed
- Faster attach/detach transitions

## [0.5.0] — 2026-04-09

### Added
- Single-key detach shortcut

## [0.3.0] — 2026-04-05

### Changed
- View stack replaces flag-based view dispatch
`

func TestParseSince_ReturnsEntriesBetweenVersions(t *testing.T) {
	result := ParseSince(testChangelog, "0.5.0")
	if !strings.Contains(result, "0.6.0") {
		t.Error("expected 0.6.0 section")
	}
	if !strings.Contains(result, "0.5.1") {
		t.Error("expected 0.5.1 section")
	}
	if strings.Contains(result, "0.5.0") {
		t.Error("should not contain 0.5.0 section")
	}
	if strings.Contains(result, "0.3.0") {
		t.Error("should not contain 0.3.0 section")
	}
}

func TestParseSince_EmptyLastVersion_ReturnsLatestOnly(t *testing.T) {
	result := ParseSince(testChangelog, "")
	if !strings.Contains(result, "0.6.0") {
		t.Error("expected 0.6.0 section")
	}
	if strings.Contains(result, "0.5.1") {
		t.Error("should not contain older versions")
	}
}

func TestParseSince_SameVersion_ReturnsEmpty(t *testing.T) {
	result := ParseSince(testChangelog, "0.6.0")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestParseSince_UnknownLastVersion_ReturnsAll(t *testing.T) {
	result := ParseSince(testChangelog, "0.1.0")
	if !strings.Contains(result, "0.6.0") {
		t.Error("expected 0.6.0")
	}
	if !strings.Contains(result, "0.3.0") {
		t.Error("expected 0.3.0")
	}
}

func TestParseSince_SkipsUnreleased(t *testing.T) {
	result := ParseSince(testChangelog, "0.5.0")
	if strings.Contains(result, "Unreleased") {
		t.Error("should not contain Unreleased section")
	}
	if strings.Contains(result, "Something in progress") {
		t.Error("should not contain unreleased content")
	}
}

func TestParseSince_EmptyChangelog(t *testing.T) {
	result := ParseSince("", "0.5.0")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}
