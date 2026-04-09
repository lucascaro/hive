package tui

import (
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/list"

	"github.com/lucascaro/hive/internal/hooks"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

func (m *Model) activeSessionByID(id string) *state.Session {
	return state.FindSession(&m.appState, id)
}

func (m *Model) agentCmd(agentType string) []string {
	if profile, ok := m.cfg.Agents[agentType]; ok && len(profile.Cmd) > 0 {
		return profile.Cmd
	}
	return []string{agentType}
}

// sortedAgentItems returns DefaultAgentItems sorted by usage (most used / recent first).
func (m *Model) sortedAgentItems() []list.Item {
	items := make([]list.Item, len(components.DefaultAgentItems))
	copy(items, components.DefaultAgentItems)
	sort.SliceStable(items, func(i, j int) bool {
		ai := items[i].(interface{ FilterValue() string }).FilterValue()
		aj := items[j].(interface{ FilterValue() string }).FilterValue()
		ri := m.appState.AgentUsage[ai]
		rj := m.appState.AgentUsage[aj]
		return ri.Score() > rj.Score()
	})
	return items
}

// pendingAttachDetails computes the SessionAttachMsg for the currently selected session, or nil.
func (m *Model) pendingAttachDetails() *SessionAttachMsg {
	sel := m.sidebar.Selected()
	if sel == nil || sel.SessionID == "" {
		return nil
	}
	sess := m.activeSessionByID(sel.SessionID)
	if sess == nil {
		return nil
	}
	return &SessionAttachMsg{
		TmuxSession:     sess.TmuxSession,
		TmuxWindow:      sess.TmuxWindow,
		RestoreGridMode: state.GridRestoreNone,
		SessionTitle:    sess.Title,
		AgentType:       sess.AgentType,
		ProjectName:     m.projectNameByID(sess.ProjectID),
		Status:          sess.Status,
		WorktreeBranch:  sess.WorktreeBranch,
		WorktreePath:    sess.WorktreePath,
	}
}

// focusSession is the single path for session focus changes (except for
// startup bootstrap in New() where components are not yet initialized).
// It updates ActiveSessionID, syncs the owning project/team, syncs the
// sidebar and grid cursors, and refreshes the preview pane.
func (m *Model) focusSession(sessionID string) {
	m.appState.ActiveSessionID = sessionID
	if sessionID != "" {
		if sess := state.FindSession(&m.appState, sessionID); sess != nil {
			m.appState.ActiveProjectID = sess.ProjectID
			m.appState.ActiveTeamID = sess.TeamID
		}
	} else {
		m.appState.ActiveProjectID = ""
		m.appState.ActiveTeamID = ""
	}
	m.sidebar.SyncActiveSession(sessionID)
	m.gridView.SyncCursor(sessionID)
	cached := m.contentSnapshots[sessionID] // "" for missing or empty sessionID
	m.appState.PreviewContent = cached
	m.preview.SetContent(cached)
}

func (m *Model) syncActiveFromSidebar() {
	sel := m.sidebar.Selected()
	if sel == nil {
		debugLog.Printf("syncActiveFromSidebar: no selection (cursor=%d items=%d)", m.sidebar.Cursor, len(m.sidebar.Items))
		return
	}
	prevSession := m.appState.ActiveSessionID
	prevProject := m.appState.ActiveProjectID
	if sel.SessionID != "" {
		// Session row: delegate to the unified focus path (handles project/team
		// sync, grid cursor, preview).
		if sel.SessionID != m.appState.ActiveSessionID {
			m.focusSession(sel.SessionID)
		}
	} else {
		// Project or team row: sync those fields only, session stays put.
		if sel.ProjectID != "" {
			m.appState.ActiveProjectID = sel.ProjectID
		}
		if sel.TeamID != "" {
			m.appState.ActiveTeamID = sel.TeamID
		}
	}
	debugLog.Printf("syncActiveFromSidebar: cursor=%d kind=%d sess=%s->%s proj=%s->%s",
		m.sidebar.Cursor, sel.Kind,
		prevSession, m.appState.ActiveSessionID,
		prevProject, m.appState.ActiveProjectID)
}

func (m *Model) liveSessions() []*state.Session {
	var out []*state.Session
	for _, sess := range state.AllSessions(&m.appState) {
		if sess.Status != state.StatusDead {
			out = append(out, sess)
		}
	}
	return out
}

func (m *Model) gridSessions(mode state.GridRestoreMode) []*state.Session {
	switch mode {
	case state.GridRestoreAll:
		return m.liveSessions()
	case state.GridRestoreProject:
		var sessions []*state.Session
		for _, sess := range m.liveSessions() {
			if m.appState.ActiveProjectID == "" || sess.ProjectID == m.appState.ActiveProjectID {
				sessions = append(sessions, sess)
			}
		}
		if len(sessions) == 0 {
			return m.liveSessions()
		}
		return sessions
	default:
		return nil
	}
}

func (m *Model) gridContentsFromSnapshots(sessions []*state.Session) map[string]string {
	if len(sessions) == 0 {
		return nil
	}
	contents := make(map[string]string, len(sessions))
	for _, sess := range sessions {
		if content := m.contentSnapshots[sess.ID]; content != "" {
			contents[sess.ID] = content
		}
	}
	return contents
}

// gridProjectNames builds a projectID→name map from the current app state.
func (m *Model) gridProjectNames() map[string]string {
	names := make(map[string]string, len(m.appState.Projects))
	for _, p := range m.appState.Projects {
		names[p.ID] = p.Name
	}
	return names
}

// gridProjectColors builds a projectID→hex color map from the current app state.
func (m *Model) gridProjectColors() map[string]string {
	colors := make(map[string]string, len(m.appState.Projects))
	for _, p := range m.appState.Projects {
		colors[p.ID] = p.Color
	}
	return colors
}

// projectNameByID returns the display name for a project ID, or "" if not found.
func (m *Model) projectNameByID(id string) string {
	if p := state.FindProject(&m.appState, id); p != nil {
		return p.Name
	}
	return ""
}

func (m *Model) teamNameByID(id string) string {
	if t := state.FindTeam(&m.appState, id); t != nil {
		return t.Name
	}
	return id
}

// sessionByTmux returns the session matching the given tmux session + window, or nil.
func (m *Model) sessionByTmux(tmuxSession string, tmuxWindow int) *state.Session {
	return state.FindSessionByTmux(&m.appState, tmuxSession, tmuxWindow)
}

// uniqueTmuxSessionNames returns the set of distinct tmux session names used
// by all hive sessions currently in appState.
func uniqueTmuxSessionNames(appState *state.AppState) []string {
	seen := make(map[string]struct{})
	for _, sess := range state.AllSessions(appState) {
		seen[sess.TmuxSession] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	return names
}

// killTmuxSessionIfEmpty kills the tmux session container named tmuxSess if no
// remaining hive sessions in appState still reference it.
func killTmuxSessionIfEmpty(appState *state.AppState, tmuxSess string) {
	for _, sess := range state.AllSessions(appState) {
		if sess.TmuxSession == tmuxSess {
			return // still in use
		}
	}
	_ = mux.KillSession(tmuxSess)
}

func (m *Model) fireHook(event state.HookEvent) {
	if !m.cfg.Hooks.Enabled {
		return
	}
	dir := m.cfg.Hooks.Dir
	if strings.HasPrefix(dir, "~") {
		home, _ := os.UserHomeDir()
		dir = home + dir[1:]
	}
	go func() {
		_ = hooks.Run(dir, event)
	}()
}
