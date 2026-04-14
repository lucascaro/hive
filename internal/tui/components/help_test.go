package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// stubKeyMap is a minimal help.KeyMap for tests — just enough for
// HelpPanel.renderTabContent to not crash on the Keys tab.
type stubKeyMap struct{}

func (stubKeyMap) ShortHelp() []key.Binding { return nil }
func (stubKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "attach"))},
	}
}

func newTestPanel() HelpPanel {
	hp := NewHelpPanel(help.New())
	hp.Width = 120
	hp.Height = 40
	return hp
}

func TestHelpPanel_Open_ClampsToValidRange(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{-1, 2}, // out of range — leaves ActiveTab unchanged (still 2 from setup)
		{0, 0},
		{3, 3},
		{4, 3},   // out of range — leaves unchanged
		{99, 3},  // out of range — leaves unchanged
		{-10, 3}, // out of range — leaves unchanged
	}
	for _, c := range cases {
		hp := newTestPanel()
		hp.ActiveTab = 2 // baseline
		hp.ScrollOffset = 5
		hp.Open(c.in)
		if c.in >= 0 && c.in < helpNumTabs {
			if hp.ActiveTab != c.want {
				t.Errorf("Open(%d) ActiveTab = %d, want %d", c.in, hp.ActiveTab, c.want)
			}
		} else {
			if hp.ActiveTab != 2 {
				t.Errorf("Open(%d) mutated ActiveTab to %d, want unchanged (2)", c.in, hp.ActiveTab)
			}
		}
		if hp.ScrollOffset != 0 {
			t.Errorf("Open(%d) ScrollOffset = %d, want 0 (always reset)", c.in, hp.ScrollOffset)
		}
	}
}

func TestHelpPanel_Update_TabNav(t *testing.T) {
	hp := newTestPanel()
	km := stubKeyMap{}

	// Right from 0 → 1
	hp.ActiveTab = 0
	hp.Update(tea.KeyMsg{Type: tea.KeyRight}, km)
	if hp.ActiveTab != 1 {
		t.Errorf("right from 0: ActiveTab = %d, want 1", hp.ActiveTab)
	}

	// Right clamps at last tab
	hp.ActiveTab = helpNumTabs - 1
	hp.Update(tea.KeyMsg{Type: tea.KeyRight}, km)
	if hp.ActiveTab != helpNumTabs-1 {
		t.Errorf("right at last: ActiveTab = %d, want %d", hp.ActiveTab, helpNumTabs-1)
	}

	// Left clamps at 0
	hp.ActiveTab = 0
	hp.Update(tea.KeyMsg{Type: tea.KeyLeft}, km)
	if hp.ActiveTab != 0 {
		t.Errorf("left at 0: ActiveTab = %d, want 0", hp.ActiveTab)
	}

	// Tab switch resets ScrollOffset
	hp.ActiveTab = 1
	hp.ScrollOffset = 5
	hp.Update(tea.KeyMsg{Type: tea.KeyRight}, km)
	if hp.ScrollOffset != 0 {
		t.Errorf("tab switch did not reset ScrollOffset, got %d", hp.ScrollOffset)
	}
}

func TestHelpPanel_Update_Scroll_ClampsAtMax(t *testing.T) {
	hp := newTestPanel()
	hp.ActiveTab = 3 // Features tab has long content
	km := stubKeyMap{}

	// Find the max by pressing j many times
	for i := 0; i < 1000; i++ {
		hp.Update(tea.KeyMsg{Type: tea.KeyDown}, km)
	}
	max := hp.ScrollOffset
	if max <= 0 {
		t.Fatalf("expected positive max scroll after 1000 down presses, got %d", max)
	}

	// Pressing j again should not increase
	hp.Update(tea.KeyMsg{Type: tea.KeyDown}, km)
	if hp.ScrollOffset != max {
		t.Errorf("scroll past end: offset jumped from %d to %d (phantom scroll regression)", max, hp.ScrollOffset)
	}

	// Pressing k decrements by one — no "catch-up" of phantom offset
	hp.Update(tea.KeyMsg{Type: tea.KeyUp}, km)
	if hp.ScrollOffset != max-1 {
		t.Errorf("k after max: offset = %d, want %d", hp.ScrollOffset, max-1)
	}
}

func TestHelpPanel_Update_Scroll_ClampsAtZero(t *testing.T) {
	hp := newTestPanel()
	km := stubKeyMap{}
	hp.ScrollOffset = 0
	hp.Update(tea.KeyMsg{Type: tea.KeyUp}, km)
	if hp.ScrollOffset != 0 {
		t.Errorf("k at 0: offset = %d, want 0", hp.ScrollOffset)
	}
}

func TestHelpPanel_Update_JK_Aliases(t *testing.T) {
	hp := newTestPanel()
	hp.ActiveTab = 2
	km := stubKeyMap{}

	hp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, km)
	after := hp.ScrollOffset
	if after == 0 {
		t.Errorf("j key did not scroll down (offset still 0)")
	}
	hp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}, km)
	if hp.ScrollOffset != after-1 {
		t.Errorf("k key did not scroll up: %d → %d", after, hp.ScrollOffset)
	}
}

func TestFixBgResets(t *testing.T) {
	// Plain text without ANSI — gets prefix only
	got := fixBgResets("hello")
	if !strings.HasPrefix(got, helpPanelBgANSI) {
		t.Errorf("fixBgResets should prepend colorBgANSI, got %q", got)
	}
	if !strings.HasSuffix(got, "hello") {
		t.Errorf("fixBgResets dropped content, got %q", got)
	}

	// Content with SGR reset — every reset is followed by re-applied bg
	in := "a\x1b[0mb\x1b[0mc"
	out := fixBgResets(in)
	// Count \x1b[0m followed by colorBgANSI
	expected := strings.ReplaceAll(in, "\x1b[0m", "\x1b[0m"+helpPanelBgANSI)
	expected = helpPanelBgANSI + expected
	if out != expected {
		t.Errorf("fixBgResets mismatch\nwant: %q\ngot:  %q", expected, out)
	}

	// Short reset form \x1b[m is also rewritten
	inShort := "x\x1b[my"
	outShort := fixBgResets(inShort)
	if !strings.Contains(outShort, "\x1b[m"+helpPanelBgANSI) {
		t.Errorf("fixBgResets did not handle \\x1b[m form, got %q", outShort)
	}

	// Empty string still gets the prefix (safe no-op)
	if got := fixBgResets(""); got != helpPanelBgANSI {
		t.Errorf("fixBgResets(empty) = %q, want just the bg prefix", got)
	}
}
