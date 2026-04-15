package tui

import (
	"testing"

	"github.com/lucascaro/hive/internal/tui/components"
)

func TestComputeLayout_Normal(t *testing.T) {
	sw, pw, ch := computeLayout(120, 40, components.PreviewActivityPanelHeight)
	if sw <= 0 {
		t.Errorf("sidebarWidth = %d, want > 0", sw)
	}
	if pw <= 0 {
		t.Errorf("previewWidth = %d, want > 0", pw)
	}
	if sw+pw != 120 {
		t.Errorf("sidebarWidth+previewWidth = %d, want 120", sw+pw)
	}
	if ch != 40-statusBarHeight-components.PreviewActivityPanelHeight {
		t.Errorf("contentHeight = %d, want %d", ch, 40-statusBarHeight-components.PreviewActivityPanelHeight)
	}
}

func TestComputeLayout_SidebarClampedToMin(t *testing.T) {
	sw, _, _ := computeLayout(80, 24, components.PreviewActivityPanelHeight)
	if sw < sidebarMinWidth {
		t.Errorf("sidebarWidth = %d, want >= %d (min)", sw, sidebarMinWidth)
	}
}

func TestComputeLayout_SidebarClampedToMax(t *testing.T) {
	sw, _, _ := computeLayout(400, 40, components.PreviewActivityPanelHeight)
	if sw > sidebarMaxWidth {
		t.Errorf("sidebarWidth = %d, want <= %d (max)", sw, sidebarMaxWidth)
	}
}

func TestComputeLayout_NarrowCollapsesSidebar(t *testing.T) {
	sw, _, _ := computeLayout(minTermWidth-1, 24, components.PreviewActivityPanelHeight)
	if sw != 0 {
		t.Errorf("sidebarWidth = %d for narrow terminal, want 0", sw)
	}
}

func TestComputeLayout_ZeroDimensionsUseSafeDefaults(t *testing.T) {
	sw, pw, ch := computeLayout(0, 0, components.PreviewActivityPanelHeight)
	if sw < 0 {
		t.Errorf("sidebarWidth = %d for zero dims, want >= 0", sw)
	}
	if pw < 0 {
		t.Errorf("previewWidth = %d for zero dims, want >= 0", pw)
	}
	if ch < 1 {
		t.Errorf("contentHeight = %d for zero dims, want >= 1", ch)
	}
}

func TestComputeLayout_ContentHeightAtLeastOne(t *testing.T) {
	_, _, ch := computeLayout(80, 1, components.PreviewActivityPanelHeight)
	if ch < 1 {
		t.Errorf("contentHeight = %d for 1-row terminal, want >= 1", ch)
	}
}

func TestComputeLayout_PreviewWidthNonNegative(t *testing.T) {
	sw, pw, _ := computeLayout(minTermWidth-1, 24, components.PreviewActivityPanelHeight)
	if sw != 0 {
		t.Skip("sidebar not collapsed for this width")
	}
	if pw != minTermWidth-1 {
		t.Errorf("previewWidth = %d for collapsed sidebar, want %d", pw, minTermWidth-1)
	}
}

func TestComputeLayout_ExactMinTermWidth(t *testing.T) {
	sw, _, _ := computeLayout(minTermWidth, 24, components.PreviewActivityPanelHeight)
	if sw == 0 {
		t.Errorf("sidebarWidth = 0 at exactly minTermWidth=%d, sidebar should not collapse", minTermWidth)
	}
}
