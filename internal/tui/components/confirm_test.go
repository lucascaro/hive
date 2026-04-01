package components

import (
	"strings"
	"testing"
)

func TestConfirmView_EmptyWhenNoMessage(t *testing.T) {
	c := &Confirm{}
	got := c.View()
	if got != "" {
		t.Errorf("Confirm.View() with empty Message = %q, want empty string", got)
	}
}

func TestConfirmView_NonEmptyWithMessage(t *testing.T) {
	c := &Confirm{Message: "Are you sure?", Action: "kill-session"}
	got := c.View()
	if got == "" {
		t.Error("Confirm.View() with message returned empty string")
	}
}

func TestConfirmView_ContainsMessage(t *testing.T) {
	c := &Confirm{Message: "Delete session forever?"}
	got := c.View()
	if !strings.Contains(got, "Delete session forever?") {
		t.Errorf("Confirm.View() = %q, expected to contain the message", got)
	}
}

func TestConfirmView_ContainsConfirmHint(t *testing.T) {
	c := &Confirm{Message: "Really?"}
	got := c.View()
	if !strings.Contains(got, "y/enter") {
		t.Errorf("Confirm.View() missing y/enter hint: %q", got)
	}
	if !strings.Contains(got, "n/esc") {
		t.Errorf("Confirm.View() missing n/esc hint: %q", got)
	}
}
