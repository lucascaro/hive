package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lucascaro/hive/internal/tui/components"
)

// TestPalette_OpenAndClose verifies that ctrl+p opens the palette and esc closes it.
func TestPalette_OpenAndClose(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Open palette.
	f.Send(tea.KeyMsg{Type: tea.KeyCtrlP})
	if f.model.TopView() != ViewPalette {
		t.Fatalf("expected ViewPalette, got %s", f.model.TopView())
	}
	if !f.model.palette.Active {
		t.Fatal("palette should be active after ctrl+p")
	}

	// Close with esc.
	cmd := f.SendSpecialKey(tea.KeyEscape)
	f.ExecCmdChain(cmd)
	if f.model.TopView() == ViewPalette {
		t.Fatal("palette should be closed after esc")
	}
}

// TestPalette_SelectAction verifies that pressing enter on a palette item
// dispatches the action and dismisses the palette.
func TestPalette_SelectAction(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.Send(tea.KeyMsg{Type: tea.KeyCtrlP})
	if f.model.TopView() != ViewPalette {
		t.Fatalf("expected ViewPalette, got %s", f.model.TopView())
	}

	// Press enter to select the first item.
	cmd := f.SendSpecialKey(tea.KeyEnter)
	f.ExecCmdChain(cmd)

	// Palette should be dismissed.
	if f.model.palette.Active {
		t.Fatal("palette should be dismissed after selection")
	}
}

// TestPalette_FilterNarrowsItems verifies that typing narrows the visible items.
func TestPalette_FilterNarrowsItems(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.Send(tea.KeyMsg{Type: tea.KeyCtrlP})

	// Type "new" to filter.
	f.SendKey("n")
	f.SendKey("e")
	f.SendKey("w")

	if f.model.palette.FilterQuery() != "new" {
		t.Errorf("FilterQuery = %q, want %q", f.model.palette.FilterQuery(), "new")
	}

	// View should contain "New" items.
	view := f.model.palette.View()
	if !strings.Contains(view, "New") {
		t.Error("palette view should contain 'New' items after filtering")
	}
}

// TestPalette_ShortcutsShown verifies that keyboard shortcuts are displayed
// alongside action names by checking the palette items directly.
func TestPalette_ShortcutsShown(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.Send(tea.KeyMsg{Type: tea.KeyCtrlP})

	// Check that palette items have non-empty shortcuts.
	items := f.model.paletteItems()
	for _, item := range items {
		pi := item.(components.PaletteItem)
		if pi.Shortcut() == "" {
			t.Errorf("palette item %q has empty shortcut", pi.FilterValue())
		}
	}
}

// TestPalette_EscClearsFilterFirst verifies that esc clears the filter before
// closing the palette.
func TestPalette_EscClearsFilterFirst(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.Send(tea.KeyMsg{Type: tea.KeyCtrlP})

	// Type a filter.
	f.SendKey("q")
	if f.model.palette.FilterQuery() != "q" {
		t.Fatal("filter should be 'q'")
	}

	// First esc clears filter (no cmd needed — filter clear is synchronous).
	f.SendSpecialKey(tea.KeyEscape)
	if f.model.palette.FilterQuery() != "" {
		t.Error("first esc should clear the filter")
	}
	if f.model.TopView() != ViewPalette {
		t.Error("palette should still be open after clearing filter")
	}

	// Second esc closes palette.
	cmd := f.SendSpecialKey(tea.KeyEscape)
	f.ExecCmdChain(cmd)
	if f.model.TopView() == ViewPalette {
		t.Error("second esc should close the palette")
	}
}

// TestPalette_OpenFromGrid verifies that ctrl+p works from grid view.
func TestPalette_OpenFromGrid(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	f.SendKey("g")
	f.AssertGridActive(true)

	f.Send(tea.KeyMsg{Type: tea.KeyCtrlP})
	if f.model.TopView() != ViewPalette {
		t.Fatalf("expected ViewPalette from grid, got %s", f.model.TopView())
	}
}
