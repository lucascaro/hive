package components

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func testDirPicker(t *testing.T) DirPicker {
	t.Helper()
	dp := NewDirPicker()
	dir := t.TempDir()
	// Create a couple of sub-directories for navigation tests.
	if err := os.MkdirAll(filepath.Join(dir, "aaa"), 0755); err != nil {
		t.Fatalf("create test dir aaa: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "bbb"), 0755); err != nil {
		t.Fatalf("create test dir bbb: %v", err)
	}
	dp.Show(dir)
	return dp
}

func TestDirPicker_ActiveConsumesAllKeyMsg(t *testing.T) {
	dp := testDirPicker(t)

	keys := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'a'}},
		{Type: tea.KeyRunes, Runes: []rune{'/'}},
		{Type: tea.KeyRunes, Runes: []rune{'x'}},
		{Type: tea.KeyUp},
		{Type: tea.KeyDown},
		{Type: tea.KeyTab},
	}

	for _, k := range keys {
		_, consumed := dp.Update(k)
		if !consumed {
			t.Errorf("key %q: consumed=false, want true", k.String())
		}
	}
}

func TestDirPicker_FilteringKeysConsumed(t *testing.T) {
	dp := testDirPicker(t)

	// Press "/" to start filtering.
	dp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})

	// Type characters while filtering.
	chars := []rune{'a', 'b', 'c'}
	for _, r := range chars {
		_, consumed := dp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		if !consumed {
			t.Errorf("filter char %q: consumed=false, want true", string(r))
		}
	}

	// Esc to clear filter should also be consumed.
	_, consumed := dp.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if !consumed {
		t.Error("esc during filter: consumed=false, want true")
	}
}

func TestDirPicker_NonKeyMsgNotConsumed(t *testing.T) {
	dp := testDirPicker(t)

	// Window size messages should not be consumed.
	_, consumed := dp.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if consumed {
		t.Error("WindowSizeMsg: consumed=true, want false")
	}
}

func TestDirPicker_InactiveDoesNotConsume(t *testing.T) {
	dp := NewDirPicker()
	// Not shown / not active.

	_, consumed := dp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if consumed {
		t.Error("inactive picker should not consume keys")
	}
}

func TestDirPicker_EscCancels(t *testing.T) {
	dp := testDirPicker(t)

	cmd, consumed := dp.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if !consumed {
		t.Fatal("esc should be consumed")
	}
	if dp.Active {
		t.Fatal("esc should deactivate the picker")
	}
	if cmd == nil {
		t.Fatal("esc should return a cmd")
	}
	msg := cmd()
	if _, ok := msg.(DirPickerCancelMsg); !ok {
		t.Fatalf("esc cmd returned %T, want DirPickerCancelMsg", msg)
	}
}

func TestDirPicker_DotConfirmsCurrentDir(t *testing.T) {
	dp := testDirPicker(t)
	origDir := dp.currentDir

	cmd, consumed := dp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
	if !consumed {
		t.Fatal("dot should be consumed")
	}
	if cmd == nil {
		t.Fatal("dot should return a cmd")
	}
	msg := cmd()
	picked, ok := msg.(DirPickedMsg)
	if !ok {
		t.Fatalf("dot cmd returned %T, want DirPickedMsg", msg)
	}
	if picked.Dir != origDir {
		t.Errorf("picked dir=%q, want %q", picked.Dir, origDir)
	}
}

func TestDirPicker_EnterDescends(t *testing.T) {
	dp := testDirPicker(t)
	origDir := dp.currentDir

	// The first item should be "aaa" (alphabetical).
	cmd, consumed := dp.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !consumed {
		t.Fatal("enter should be consumed")
	}
	// No cmd returned for navigation.
	_ = cmd

	expected := filepath.Join(origDir, "aaa")
	if dp.currentDir != expected {
		t.Errorf("after enter: currentDir=%q, want %q", dp.currentDir, expected)
	}
}

func TestDirPicker_BackspaceGoesUp(t *testing.T) {
	dp := testDirPicker(t)
	origDir := dp.currentDir

	// Descend first.
	dp.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Now go up with backspace.
	_, consumed := dp.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if !consumed {
		t.Fatal("backspace should be consumed")
	}
	if dp.currentDir != origDir {
		t.Errorf("after backspace: currentDir=%q, want %q", dp.currentDir, origDir)
	}
}

func TestDirPicker_LeftArrowGoesUp(t *testing.T) {
	dp := testDirPicker(t)
	origDir := dp.currentDir

	dp.Update(tea.KeyMsg{Type: tea.KeyEnter}) // descend
	_, consumed := dp.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if !consumed {
		t.Fatal("left arrow should be consumed")
	}
	if dp.currentDir != origDir {
		t.Errorf("after left: currentDir=%q, want %q", dp.currentDir, origDir)
	}
}

func TestDirPicker_HKeyGoesUp(t *testing.T) {
	dp := testDirPicker(t)
	origDir := dp.currentDir

	dp.Update(tea.KeyMsg{Type: tea.KeyEnter}) // descend
	_, consumed := dp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if !consumed {
		t.Fatal("h should be consumed")
	}
	if dp.currentDir != origDir {
		t.Errorf("after h: currentDir=%q, want %q", dp.currentDir, origDir)
	}
}
