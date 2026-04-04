package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lucascaro/hive/internal/tui/components"
)

// TestFlow_NewProject_NameDirAgent tests the full new project creation flow:
// press "n" → type name → enter → dir picker → pick dir → project created.
func TestFlow_NewProject_NameDirAgent(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Step 1: Press "n" to start new project flow.
	f.SendKey("n")
	f.AssertInputMode("project-name")
	f.Snapshot("01-name-input")

	// Step 2: Type project name and press enter.
	f.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("my-new-project")})
	f.SendSpecialKey(tea.KeyEnter)

	// Dir picker should be active, input mode cleared.
	f.AssertInputMode("")
	if !f.Model().dirPicker.Active {
		t.Fatal("dirPicker should be active after entering project name")
	}
	if f.Model().pendingProjectName != "my-new-project" {
		t.Fatalf("pendingProjectName = %q, want %q",
			f.Model().pendingProjectName, "my-new-project")
	}
	f.Snapshot("02-dir-picker")

	// Step 3: Pick an existing directory.
	dir := t.TempDir()
	cmd := f.Send(components.DirPickedMsg{Dir: dir})

	// Dir picker should be dismissed.
	if f.Model().dirPicker.Active {
		t.Fatal("dirPicker should be inactive after pick")
	}

	// Execute the createProject command.
	if cmd == nil {
		t.Fatal("expected createProject command")
	}
	msg := cmd()
	if msg == nil {
		t.Fatal("createProject command returned nil")
	}
	f.Send(msg)

	// The new project should appear in the sidebar.
	f.ViewContains("my-new-project")
	f.Snapshot("03-project-created")
}

// TestFlow_NewProject_DirPickerCancel_BackToName tests that canceling the dir picker
// returns to the project name input with the name preserved.
func TestFlow_NewProject_DirPickerCancel_BackToName(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Start new project flow.
	f.SendKey("n")
	f.AssertInputMode("project-name")

	// Type name and confirm.
	f.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("my-project")})
	f.SendSpecialKey(tea.KeyEnter)

	// Dir picker should be active.
	if !f.Model().dirPicker.Active {
		t.Fatal("dirPicker should be active")
	}
	f.Snapshot("01-dir-picker-open")

	// Cancel the dir picker.
	f.Send(components.DirPickerCancelMsg{})

	// Should return to name input with name preserved.
	f.AssertInputMode("project-name")
	if f.Model().dirPicker.Active {
		t.Fatal("dirPicker should be inactive after cancel")
	}
	if f.Model().pendingProjectName != "my-project" {
		t.Fatalf("pendingProjectName = %q, want preserved %q",
			f.Model().pendingProjectName, "my-project")
	}
	f.Snapshot("02-back-to-name")
}

// TestFlow_NewProject_NonExistentDir_AsksConfirmation tests that picking a
// non-existent directory shows a confirmation dialog.
func TestFlow_NewProject_NonExistentDir_AsksConfirmation(t *testing.T) {
	m, mock := testFlowModel(t)
	f := newFlowRunner(t, m, mock)

	// Start new project flow.
	f.SendKey("n")
	f.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("proj")})
	f.SendSpecialKey(tea.KeyEnter)

	// Pick a non-existent directory.
	nonexistent := t.TempDir() + "/does-not-exist"
	f.Send(components.DirPickedMsg{Dir: nonexistent})

	// Should show directory confirmation dialog.
	f.AssertInputMode("project-dir-confirm")
	f.ViewContains("does not exist")
	f.ViewContains("Create it?")
}
