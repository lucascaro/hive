package tui

import (
	"testing"

	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// TestFlow_ColorCycle_ChangesProjectColor tests pressing "c" to cycle a project's color.
func TestFlow_ColorCycle_ChangesProjectColor(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	initialColor := f.model.appState.Projects[0].Color

	// Press "c" to cycle to next color.
	f.SendKey("c")

	newColor := f.model.appState.Projects[0].Color
	if newColor == initialColor {
		t.Error("pressing 'c' should change the project color")
	}

	f.Snapshot("01-after-color-cycle")
}

// TestFlow_ColorCyclePrev_ChangesProjectColor tests pressing "C" to cycle backward.
func TestFlow_ColorCyclePrev_ChangesProjectColor(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	initialColor := f.model.appState.Projects[0].Color

	// Press "C" to cycle backward.
	f.SendKey("C")

	newColor := f.model.appState.Projects[0].Color
	if newColor == initialColor {
		t.Error("pressing 'C' should change the project color")
	}
}

// TestFlow_ColorCycle_SkipsOtherProjectColors tests that cycling skips colors used by other projects.
func TestFlow_ColorCycle_SkipsOtherProjectColors(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	proj2Color := f.model.appState.Projects[1].Color

	// Cycle project 1's color several times; it should never match project 2's color.
	for i := 0; i < len(styles.ProjectPalette); i++ {
		f.SendKey("c")
		if f.model.appState.Projects[0].Color == proj2Color {
			t.Errorf("after %d cycles, proj-1 color %q matches proj-2 color %q",
				i+1, f.model.appState.Projects[0].Color, proj2Color)
		}
	}
}

// TestFlow_ColorCycle_GridView tests color cycling in grid view.
func TestFlow_ColorCycle_GridView(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Open grid view.
	f.SendKey("g")
	f.AssertGridActive(true)

	// Get the selected session's project ID.
	gv := f.model.gridView
	sess := gv.Selected()
	if sess == nil {
		t.Fatal("no session selected in grid")
	}
	proj := state.FindProject(&f.model.appState, sess.ProjectID)
	if proj == nil {
		t.Fatal("project not found for selected session")
	}
	initialColor := proj.Color

	// Press "c" in grid view.
	f.SendKey("c")

	proj = state.FindProject(&f.model.appState, sess.ProjectID)
	if proj.Color == initialColor {
		t.Error("pressing 'c' in grid view should change the project color")
	}

	f.Snapshot("01-grid-after-color-cycle")
}
