package components

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestOrphanPicker_NewEmptySessions(t *testing.T) {
	op := NewOrphanPicker(nil)
	if op.Active {
		t.Error("expected Active=false for empty sessions")
	}
}

func TestOrphanPicker_NewWithSessions(t *testing.T) {
	op := NewOrphanPicker([]string{"hive-aaa", "hive-bbb"})
	if !op.Active {
		t.Error("expected Active=true")
	}
	if op.cursor != 0 {
		t.Errorf("expected cursor=0, got %d", op.cursor)
	}
}

func TestOrphanPicker_CursorNavigation(t *testing.T) {
	op := NewOrphanPicker([]string{"a", "b", "c"})

	// Down
	op, _ = op.Update(keyType(tea.KeyDown))
	if op.cursor != 1 {
		t.Errorf("after j: cursor=%d, want 1", op.cursor)
	}
	op, _ = op.Update(keyType(tea.KeyDown))
	if op.cursor != 2 {
		t.Errorf("after down: cursor=%d, want 2", op.cursor)
	}
	// Clamp at bottom
	op, _ = op.Update(keyType(tea.KeyDown))
	if op.cursor != 2 {
		t.Errorf("should clamp at bottom: cursor=%d, want 2", op.cursor)
	}

	// Up
	op, _ = op.Update(keyType(tea.KeyUp))
	if op.cursor != 1 {
		t.Errorf("after k: cursor=%d, want 1", op.cursor)
	}
	op, _ = op.Update(keyType(tea.KeyUp))
	if op.cursor != 0 {
		t.Errorf("after up: cursor=%d, want 0", op.cursor)
	}
	// Clamp at top
	op, _ = op.Update(keyType(tea.KeyUp))
	if op.cursor != 0 {
		t.Errorf("should clamp at top: cursor=%d, want 0", op.cursor)
	}
}

func TestOrphanPicker_SpaceToggle(t *testing.T) {
	op := NewOrphanPicker([]string{"a", "b"})

	// Toggle first on
	op, _ = op.Update(keyPress(" "))
	if !op.selected[0] {
		t.Error("expected selected[0]=true after space")
	}
	// Toggle first off
	op, _ = op.Update(keyPress(" "))
	if op.selected[0] {
		t.Error("expected selected[0]=false after second space")
	}
}

func TestOrphanPicker_ToggleAll(t *testing.T) {
	op := NewOrphanPicker([]string{"a", "b", "c"})

	// None selected → all selected
	op, _ = op.Update(keyPress("a"))
	for i, s := range op.selected {
		if !s {
			t.Errorf("after toggle-all (none→all): selected[%d]=false", i)
		}
	}

	// All selected → none selected
	op, _ = op.Update(keyPress("a"))
	for i, s := range op.selected {
		if s {
			t.Errorf("after toggle-all (all→none): selected[%d]=true", i)
		}
	}

	// Partial → all selected
	op.selected[0] = true
	op, _ = op.Update(keyPress("a"))
	for i, s := range op.selected {
		if !s {
			t.Errorf("after toggle-all (partial→all): selected[%d]=false", i)
		}
	}
}

func TestOrphanPicker_EnterReturnsSelected(t *testing.T) {
	op := NewOrphanPicker([]string{"a", "b", "c"})
	op.selected[0] = true
	op.selected[2] = true

	op, cmd := op.Update(keyType(tea.KeyEnter))
	if op.Active {
		t.Error("expected Active=false after enter")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	done, ok := msg.(OrphanPickerDoneMsg)
	if !ok {
		t.Fatalf("expected OrphanPickerDoneMsg, got %T", msg)
	}
	if len(done.Selected) != 2 || done.Selected[0] != "a" || done.Selected[1] != "c" {
		t.Errorf("Selected=%v, want [a c]", done.Selected)
	}
}

func TestOrphanPicker_EnterNoneSelected(t *testing.T) {
	op := NewOrphanPicker([]string{"a", "b"})
	_, cmd := op.Update(keyType(tea.KeyEnter))
	msg := cmd()
	done := msg.(OrphanPickerDoneMsg)
	if done.Selected != nil {
		t.Errorf("expected nil Selected, got %v", done.Selected)
	}
}

func TestOrphanPicker_EscReturnsNil(t *testing.T) {
	op := NewOrphanPicker([]string{"a", "b"})
	op.selected[0] = true

	op, cmd := op.Update(keyType(tea.KeyEscape))
	if op.Active {
		t.Error("expected Active=false after esc")
	}
	msg := cmd()
	done := msg.(OrphanPickerDoneMsg)
	if done.Selected != nil {
		t.Errorf("expected nil Selected on esc, got %v", done.Selected)
	}
}

func TestOrphanPicker_QuitReturnsNil(t *testing.T) {
	op := NewOrphanPicker([]string{"a"})
	op, cmd := op.Update(keyPress("q"))
	if op.Active {
		t.Error("expected Active=false after q")
	}
	msg := cmd()
	done := msg.(OrphanPickerDoneMsg)
	if done.Selected != nil {
		t.Errorf("expected nil Selected on q, got %v", done.Selected)
	}
}
