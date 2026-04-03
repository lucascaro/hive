package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDispatchKey_FirstFocusedHandlerWins(t *testing.T) {
	captured := false
	leaked := false

	handlers := []KeyHandler{
		componentHandler{
			focused: func() bool { return true },
			handle:  func(tea.KeyMsg) tea.Cmd { captured = true; return nil },
		},
		componentHandler{
			focused: func() bool { return true },
			handle:  func(tea.KeyMsg) tea.Cmd { leaked = true; return nil },
		},
	}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	_, handled := dispatchKey(handlers, msg)

	if !handled {
		t.Fatal("dispatchKey should report handled=true when a handler is focused")
	}
	if !captured {
		t.Fatal("first focused handler should have received the key")
	}
	if leaked {
		t.Fatal("second handler should NOT have received the key")
	}
}

func TestDispatchKey_NoFocusedHandler(t *testing.T) {
	called := false
	handlers := []KeyHandler{
		componentHandler{
			focused: func() bool { return false },
			handle:  func(tea.KeyMsg) tea.Cmd { called = true; return nil },
		},
		componentHandler{
			focused: func() bool { return false },
			handle:  func(tea.KeyMsg) tea.Cmd { called = true; return nil },
		},
	}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	cmd, handled := dispatchKey(handlers, msg)

	if handled {
		t.Fatal("dispatchKey should report handled=false when no handler is focused")
	}
	if cmd != nil {
		t.Fatal("cmd should be nil when no handler is focused")
	}
	if called {
		t.Fatal("no handler should have been called")
	}
}

func TestDispatchKey_SkipsUnfocusedHandler(t *testing.T) {
	order := []int{}

	handlers := []KeyHandler{
		componentHandler{
			focused: func() bool { return false },
			handle:  func(tea.KeyMsg) tea.Cmd { order = append(order, 0); return nil },
		},
		componentHandler{
			focused: func() bool { return true },
			handle:  func(tea.KeyMsg) tea.Cmd { order = append(order, 1); return nil },
		},
		componentHandler{
			focused: func() bool { return true },
			handle:  func(tea.KeyMsg) tea.Cmd { order = append(order, 2); return nil },
		},
	}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}}
	_, handled := dispatchKey(handlers, msg)

	if !handled {
		t.Fatal("should be handled")
	}
	if len(order) != 1 || order[0] != 1 {
		t.Fatalf("expected only handler[1] to fire, got %v", order)
	}
}

func TestDispatchKey_ReturnsHandlerCmd(t *testing.T) {
	type testMsg struct{}
	handlers := []KeyHandler{
		componentHandler{
			focused: func() bool { return true },
			handle: func(tea.KeyMsg) tea.Cmd {
				return func() tea.Msg { return testMsg{} }
			},
		},
	}

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	cmd, handled := dispatchKey(handlers, msg)

	if !handled {
		t.Fatal("should be handled")
	}
	if cmd == nil {
		t.Fatal("cmd should not be nil")
	}
	result := cmd()
	if _, ok := result.(testMsg); !ok {
		t.Fatalf("cmd returned %T, want testMsg", result)
	}
}

func TestDispatchKey_EmptyHandlers(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	cmd, handled := dispatchKey(nil, msg)

	if handled {
		t.Fatal("should not be handled with nil handlers")
	}
	if cmd != nil {
		t.Fatal("cmd should be nil")
	}
}
