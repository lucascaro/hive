package components

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// TitleEditor wraps a textinput for inline session/team/project title editing.
type TitleEditor struct {
	input     textinput.Model
	SessionID string
	TeamID    string
	ProjectID string
	Active    bool
}

// NewTitleEditor creates a TitleEditor.
func NewTitleEditor() TitleEditor {
	ti := textinput.New()
	ti.CharLimit = 80
	ti.Width = 44
	return TitleEditor{input: ti}
}

// Start begins editing with the given initial value.
func (te *TitleEditor) Start(sessionID, teamID, projectID, current string) {
	te.SessionID = sessionID
	te.TeamID = teamID
	te.ProjectID = projectID
	te.input.SetValue(current)
	te.input.CursorEnd()
	te.input.Focus()
	te.Active = true
}

// Stop clears the editor.
func (te *TitleEditor) Stop() {
	te.input.Blur()
	te.Active = false
	te.SessionID = ""
	te.TeamID = ""
	te.ProjectID = ""
}

// Value returns the current text.
func (te *TitleEditor) Value() string { return te.input.Value() }

// Update handles key messages while the editor is active.
func (te *TitleEditor) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	te.input, cmd = te.input.Update(msg)
	return cmd
}

// View renders the text input.
func (te *TitleEditor) View() string {
	return te.input.View()
}
