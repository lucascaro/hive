package tui

import "testing"

func TestComputeLayout_Normal(t *testing.T) {
	sw, pw, ch := computeLayout(120, 40)
	if sw <= 0 {
		t.Errorf("sidebarWidth = %d, want > 0", sw)
	}
	if pw <= 0 {
		t.Errorf("previewWidth = %d, want > 0", pw)
	}
	if sw+pw != 120 {
		t.Errorf("sidebarWidth+previewWidth = %d, want 120", sw+pw)
	}
	if ch != 40-statusBarHeight {
		t.Errorf("contentHeight = %d, want %d", ch, 40-statusBarHeight)
	}
}

func TestComputeLayout_SidebarClampedToMin(t *testing.T) {
	// Very wide terminal where ratio gives less than min
	sw, _, _ := computeLayout(80, 24)
	if sw < sidebarMinWidth {
		t.Errorf("sidebarWidth = %d, want >= %d (min)", sw, sidebarMinWidth)
	}
}

func TestComputeLayout_SidebarClampedToMax(t *testing.T) {
	// Wide terminal where ratio would exceed max
	sw, _, _ := computeLayout(400, 40)
	if sw > sidebarMaxWidth {
		t.Errorf("sidebarWidth = %d, want <= %d (max)", sw, sidebarMaxWidth)
	}
}

func TestComputeLayout_NarrowCollapsesSidebar(t *testing.T) {
	// Terminal narrower than minTermWidth should collapse sidebar to 0
	sw, _, _ := computeLayout(minTermWidth-1, 24)
	if sw != 0 {
		t.Errorf("sidebarWidth = %d for narrow terminal, want 0", sw)
	}
}

func TestComputeLayout_ZeroDimensionsUseSafeDefaults(t *testing.T) {
	sw, pw, ch := computeLayout(0, 0)
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
	// Very short terminal
	_, _, ch := computeLayout(80, 1)
	if ch < 1 {
		t.Errorf("contentHeight = %d for 1-row terminal, want >= 1", ch)
	}
}

func TestComputeLayout_PreviewWidthNonNegative(t *testing.T) {
	// When sidebar is 0, preview should equal termWidth
	sw, pw, _ := computeLayout(minTermWidth-1, 24)
	if sw != 0 {
		t.Skip("sidebar not collapsed for this width")
	}
	if pw != minTermWidth-1 {
		t.Errorf("previewWidth = %d for collapsed sidebar, want %d", pw, minTermWidth-1)
	}
}

func TestComputeLayout_ExactMinTermWidth(t *testing.T) {
	// Exactly minTermWidth should NOT collapse the sidebar
	sw, _, _ := computeLayout(minTermWidth, 24)
	if sw == 0 {
		t.Errorf("sidebarWidth = 0 at exactly minTermWidth=%d, sidebar should not collapse", minTermWidth)
	}
}
