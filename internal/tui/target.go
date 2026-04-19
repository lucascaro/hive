package tui

import (
	"github.com/lucascaro/hive/internal/tui/components"
)

// TargetKind identifies what kind of thing the user currently has selected.
// Commands use this to decide whether they are applicable (e.g. kill-session
// only applies when Kind == TargetSession).
type TargetKind int

const (
	TargetNone TargetKind = iota
	TargetProject
	TargetTeam
	TargetSession
)

// Target is a view-agnostic snapshot of "what is currently selected." It
// collapses the two historical selection sources (sidebar vs. grid) into
// one shape so action executors don't branch on context.
type Target struct {
	Kind      TargetKind
	ProjectID string
	TeamID    string
	SessionID string
	Label     string
}

// activeTarget returns the selection for the currently active view. When the
// grid is on top of the view stack, the grid's selection wins; otherwise the
// sidebar's selection is used. Executors read from this instead of reaching
// into m.sidebar or m.gridView directly.
func (m *Model) activeTarget() Target {
	if m.TopView() == ViewGrid {
		if s := m.gridView.Selected(); s != nil {
			return Target{
				Kind:      TargetSession,
				ProjectID: s.ProjectID,
				SessionID: s.ID,
				Label:     s.Title,
			}
		}
		return Target{}
	}
	sel := m.sidebar.Selected()
	if sel == nil {
		return Target{}
	}
	t := Target{
		ProjectID: sel.ProjectID,
		TeamID:    sel.TeamID,
		SessionID: sel.SessionID,
		Label:     sel.Label,
	}
	switch sel.Kind {
	case components.KindProject:
		t.Kind = TargetProject
	case components.KindTeam:
		t.Kind = TargetTeam
	case components.KindSession:
		t.Kind = TargetSession
	}
	return t
}
