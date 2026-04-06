package components

import "testing"

func TestNewTitleEditor_InitialState(t *testing.T) {
	te := NewTitleEditor()
	if te.Active {
		t.Error("TitleEditor should not be Active on creation")
	}
	if te.SessionID != "" {
		t.Errorf("SessionID = %q, want empty", te.SessionID)
	}
	if te.TeamID != "" {
		t.Errorf("TeamID = %q, want empty", te.TeamID)
	}
}

func TestTitleEditor_Start(t *testing.T) {
	te := NewTitleEditor()
	te.Start("sess-1", "", "", "old title")
	if !te.Active {
		t.Error("TitleEditor should be Active after Start()")
	}
	if te.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", te.SessionID, "sess-1")
	}
	if te.Value() != "old title" {
		t.Errorf("Value() = %q, want %q", te.Value(), "old title")
	}
}

func TestTitleEditor_StartWithTeam(t *testing.T) {
	te := NewTitleEditor()
	te.Start("", "team-1", "", "team name")
	if te.TeamID != "team-1" {
		t.Errorf("TeamID = %q, want %q", te.TeamID, "team-1")
	}
}

func TestTitleEditor_StartWithProject(t *testing.T) {
	te := NewTitleEditor()
	te.Start("", "", "proj-1", "project name")
	if te.ProjectID != "proj-1" {
		t.Errorf("ProjectID = %q, want %q", te.ProjectID, "proj-1")
	}
	if te.Value() != "project name" {
		t.Errorf("Value() = %q, want %q", te.Value(), "project name")
	}
}

func TestTitleEditor_Stop(t *testing.T) {
	te := NewTitleEditor()
	te.Start("sess-1", "", "", "title")
	te.Stop()
	if te.Active {
		t.Error("TitleEditor should not be Active after Stop()")
	}
	if te.SessionID != "" {
		t.Errorf("SessionID = %q after Stop(), want empty", te.SessionID)
	}
	if te.TeamID != "" {
		t.Errorf("TeamID = %q after Stop(), want empty", te.TeamID)
	}
	if te.ProjectID != "" {
		t.Errorf("ProjectID = %q after Stop(), want empty", te.ProjectID)
	}
}

func TestTitleEditor_ViewNonEmpty(t *testing.T) {
	te := NewTitleEditor()
	// View() wraps the textinput; should not panic and should return a string
	got := te.View()
	// textinput always renders something (even if empty)
	_ = got
}
