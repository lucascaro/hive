package styles

import (
	"strings"
	"testing"
)

func TestNextProjectColor_Cycles(t *testing.T) {
	n := len(ProjectPalette)
	for i := 0; i < n*2; i++ {
		got := NextProjectColor(i)
		want := ProjectPalette[i%n]
		if got != want {
			t.Errorf("NextProjectColor(%d) = %q, want %q", i, got, want)
		}
	}
}

func TestNextFreeColor_SkipsUsed(t *testing.T) {
	used := []string{ProjectPalette[0], ProjectPalette[1]}
	got := NextFreeColor(used)
	if got != ProjectPalette[2] {
		t.Errorf("NextFreeColor with first 2 used = %q, want %q", got, ProjectPalette[2])
	}
}

func TestNextFreeColor_AllUsedFallback(t *testing.T) {
	got := NextFreeColor(ProjectPalette)
	if got == "" {
		t.Error("NextFreeColor with all used returned empty")
	}
}

func TestNextFreeSessionColor_SkipsProjectAndUsed(t *testing.T) {
	projColor := ProjectPalette[0]
	usedColors := []string{ProjectPalette[1]}
	got := NextFreeSessionColor(projColor, usedColors)
	if got == projColor {
		t.Errorf("NextFreeSessionColor should skip project color %q, got %q", projColor, got)
	}
	if got == ProjectPalette[1] {
		t.Errorf("NextFreeSessionColor should skip used color %q, got %q", ProjectPalette[1], got)
	}
	if got != ProjectPalette[2] {
		t.Errorf("NextFreeSessionColor = %q, want %q", got, ProjectPalette[2])
	}
}

func TestNextFreeSessionColor_AllUsedFallback(t *testing.T) {
	got := NextFreeSessionColor(ProjectPalette[0], ProjectPalette[1:])
	if got == "" {
		t.Error("NextFreeSessionColor with all used should still return a color")
	}
}

func TestNextFreeSessionColor_EmptyUsed(t *testing.T) {
	projColor := ProjectPalette[0]
	got := NextFreeSessionColor(projColor, nil)
	// Should return first palette color that isn't the project color.
	if got == projColor {
		t.Errorf("NextFreeSessionColor should skip project color, got %q", got)
	}
	if got != ProjectPalette[1] {
		t.Errorf("NextFreeSessionColor = %q, want %q", got, ProjectPalette[1])
	}
}

func TestCycleColor_Forward(t *testing.T) {
	got := CycleColor(ProjectPalette[0], +1, nil)
	if got != ProjectPalette[1] {
		t.Errorf("CycleColor forward = %q, want %q", got, ProjectPalette[1])
	}
}

func TestCycleColor_Backward(t *testing.T) {
	got := CycleColor(ProjectPalette[1], -1, nil)
	if got != ProjectPalette[0] {
		t.Errorf("CycleColor backward = %q, want %q", got, ProjectPalette[0])
	}
}

func TestCycleColor_SkipsUsed(t *testing.T) {
	// Current is [0], [1] is used by another, should skip to [2]
	got := CycleColor(ProjectPalette[0], +1, []string{ProjectPalette[1]})
	if got != ProjectPalette[2] {
		t.Errorf("CycleColor skipping used = %q, want %q", got, ProjectPalette[2])
	}
}

func TestCycleColor_WrapsAround(t *testing.T) {
	last := len(ProjectPalette) - 1
	got := CycleColor(ProjectPalette[last], +1, nil)
	if got != ProjectPalette[0] {
		t.Errorf("CycleColor wrap = %q, want %q", got, ProjectPalette[0])
	}
}

func TestContrastForeground_DarkBg(t *testing.T) {
	got := ContrastForeground("#000000")
	if string(got) != "#F9FAFB" {
		t.Errorf("ContrastForeground(black) = %q, want light", got)
	}
}

func TestContrastForeground_LightBg(t *testing.T) {
	got := ContrastForeground("#FFFFFF")
	if string(got) != "#1F2937" {
		t.Errorf("ContrastForeground(white) = %q, want dark", got)
	}
}

func TestContrastForeground_MidTone_PicksBestContrast(t *testing.T) {
	// Orange #F97316 has lum ~0.325; dark text gives better contrast than light.
	got := ContrastForeground("#F97316")
	if string(got) != "#1F2937" {
		t.Errorf("ContrastForeground(orange) = %q, want dark for better contrast", got)
	}
}

func TestContrastForeground_InvalidHex(t *testing.T) {
	// Should fallback to light (dark assumed)
	got := ContrastForeground("invalid")
	if string(got) != "#F9FAFB" {
		t.Errorf("ContrastForeground(invalid) = %q, want light fallback", got)
	}
}

func TestContrastForeground_AllPaletteColors_MinContrast(t *testing.T) {
	// Every palette color should produce a foreground with at least 3.5:1 contrast.
	for _, hex := range ProjectPalette {
		fg := ContrastForeground(hex)
		bgLum := relativeLuminance(hex)
		fgLum := relativeLuminance(string(fg))
		cr := contrastRatio(fgLum, bgLum)
		if cr < 3.5 {
			t.Errorf("palette color %s: contrast ratio %.1f:1 is below 3.5:1 minimum (fg=%s)", hex, cr, fg)
		}
	}
}

func TestNextFreeColor_EmptyUsed(t *testing.T) {
	got := NextFreeColor(nil)
	if got != ProjectPalette[0] {
		t.Errorf("NextFreeColor(nil) = %q, want %q", got, ProjectPalette[0])
	}
}

func TestNextFreeColor_CaseInsensitive(t *testing.T) {
	used := []string{strings.ToLower(ProjectPalette[0])}
	got := NextFreeColor(used)
	if got == ProjectPalette[0] {
		t.Errorf("NextFreeColor should treat colors case-insensitively, got %q again", got)
	}
}

func TestCycleColor_BackwardWrapsAround(t *testing.T) {
	got := CycleColor(ProjectPalette[0], -1, nil)
	want := ProjectPalette[len(ProjectPalette)-1]
	if got != want {
		t.Errorf("CycleColor backward wrap = %q, want %q", got, want)
	}
}

func TestCycleColor_AllUsedByOthers(t *testing.T) {
	// All palette colors used by others — should still return next (no skip possible).
	got := CycleColor(ProjectPalette[0], +1, ProjectPalette[1:])
	if got == "" {
		t.Error("CycleColor with all used should still return a color")
	}
}

func TestCycleColor_UnknownCurrent(t *testing.T) {
	// Unknown color defaults to startIdx=0, then cycles from there.
	got := CycleColor("#999999", +1, nil)
	if got != ProjectPalette[1] {
		t.Errorf("CycleColor unknown current = %q, want %q", got, ProjectPalette[1])
	}
}

func TestProjectColorBar_NonEmpty(t *testing.T) {
	got := ProjectColorBar("#7C3AED")
	if got == "" {
		t.Error("ProjectColorBar returned empty")
	}
}

func TestProjectColorOrDefault_Empty(t *testing.T) {
	got := ProjectColorOrDefault("")
	if got == "" {
		t.Error("ProjectColorOrDefault(\"\") returned empty")
	}
}

func TestProjectColorOrDefault_NonEmpty(t *testing.T) {
	got := ProjectColorOrDefault("#FF0000")
	if got != "#FF0000" {
		t.Errorf("ProjectColorOrDefault(#FF0000) = %q, want #FF0000", got)
	}
}

func TestAgentBadgeOnBg_ContainsAgentName(t *testing.T) {
	got := AgentBadgeOnBg("claude", "#000000")
	if !strings.Contains(got, "claude") {
		t.Errorf("AgentBadgeOnBg = %q, expected to contain 'claude'", got)
	}
}

func TestStatusDotOnBg_AllStatuses(t *testing.T) {
	for _, status := range []string{"running", "idle", "waiting", "dead", "unknown"} {
		got := StatusDotOnBg(status, "#000000")
		if got == "" {
			t.Errorf("StatusDotOnBg(%q) returned empty", status)
		}
	}
}

func TestAgentBadge_KnownAgents(t *testing.T) {
	known := []string{"claude", "codex", "gemini", "copilot", "aider", "opencode"}
	for _, agent := range known {
		t.Run(agent, func(t *testing.T) {
			got := AgentBadge(agent)
			if got == "" {
				t.Errorf("AgentBadge(%q) returned empty string", agent)
			}
			if !strings.Contains(got, agent) {
				t.Errorf("AgentBadge(%q) = %q, expected to contain agent name", agent, got)
			}
		})
	}
}

func TestAgentBadge_UnknownAgentFallback(t *testing.T) {
	got := AgentBadge("unknown-agent-xyz")
	if got == "" {
		t.Error("AgentBadge for unknown agent returned empty string")
	}
}

func TestStatusDot_AllStatuses(t *testing.T) {
	statuses := []string{"running", "idle", "waiting", "dead"}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			got := StatusDot(status)
			if got == "" {
				t.Errorf("StatusDot(%q) returned empty string", status)
			}
		})
	}
}

func TestStatusDot_UnknownFallback(t *testing.T) {
	got := StatusDot("unknown-status")
	if got == "" {
		t.Error("StatusDot for unknown status returned empty string")
	}
}

func TestStatusLegend_NonEmpty(t *testing.T) {
	got := StatusLegend()
	if got == "" {
		t.Error("StatusLegend() returned empty string")
	}
}

func TestStatusLegend_ContainsAllStatusNames(t *testing.T) {
	got := StatusLegend()
	for _, label := range []string{"idle", "working", "waiting", "dead"} {
		if !strings.Contains(got, label) {
			t.Errorf("StatusLegend() missing %q", label)
		}
	}
}
