package tui

import (
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/escape"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

func (m Model) handleSessionCreated(msg SessionCreatedMsg) (tea.Model, tea.Cmd) {
	m.appState = *state.UpdateSessionStatus(&m.appState, msg.Session.ID, state.StatusRunning)
	m.sidebar.Rebuild(&m.appState)
	m.refreshGrid()
	m.focusSession(msg.Session.ID)
	m.persist()
	m.previewPollGen++ // new session, start fresh poll chain
	return m, m.schedulePollPreview()
}

func (m Model) handleSessionKilled(msg SessionKilledMsg) (tea.Model, tea.Cmd) {
	fallback := state.NextSessionAfterRemoval(&m.appState, msg.SessionID)
	m.appState = *state.RemoveSession(&m.appState, msg.SessionID)
	if msg.TmuxSession != "" {
		killTmuxSessionIfEmpty(&m.appState, msg.TmuxSession)
	}
	delete(m.stableCounts, msg.SessionID)
	delete(m.contentSnapshots, msg.SessionID)
	// Rebuild sidebar before focusSession so its SyncActiveSession can
	// locate the fallback in the new items list. Use persist() instead of
	// commitState() to avoid a second redundant rebuild.
	m.sidebar.Rebuild(&m.appState)
	m.refreshGrid()
	m.focusSession(fallback)
	m.persist()
	m.previewPollGen++
	return m, m.schedulePollPreview()
}

func (m Model) handleSessionTitleChanged(msg SessionTitleChangedMsg) (tea.Model, tea.Cmd) {
	m.appState = *state.UpdateSessionTitle(&m.appState, msg.SessionID, msg.Title, msg.Source)
	// Rename tmux window keeping the structured {proj}-{agent}-{title} format.
	sess := m.appState.ActiveSession()
	if sess != nil {
		projName := ""
		if proj := m.appState.ActiveProject(); proj != nil {
			projName = proj.Name
		}
		target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
		_ = mux.RenameWindow(target, mux.WindowName(projName, string(sess.AgentType), msg.Title))
		m.fireHook(state.HookEvent{
			Name:         state.EventSessionTitleChange,
			SessionID:    sess.ID,
			SessionTitle: msg.Title,
			AgentType:    sess.AgentType,
			AgentCmd:     sess.AgentCmd,
			TmuxSession:  sess.TmuxSession,
			TmuxWindow:   sess.TmuxWindow,
			WorkDir:      sess.WorkDir,
		})
	}
	m.refreshGrid()
	m.commitState()
	return m, nil
}

func (m Model) handleSessionAttach(msg SessionAttachMsg) (tea.Model, tea.Cmd) {
	// Clear visual bell indicator — the user is now looking at this session.
	delete(m.bellPending, m.appState.ActiveSessionID)
	cmd := m.doAttach(msg)
	return m, cmd
}

func (m Model) handleAttachDone(msg AttachDoneMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		return m, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
	}
	// Clear bell indicator for the session the user just visited.
	delete(m.bellPending, m.appState.ActiveSessionID)
	m.appState.RestoreGridMode = msg.RestoreGridMode
	m.restoreGrid()
	m.sidebar.Rebuild(&m.appState)
	m.sidebar.SetBellPending(m.bellPending)
	m.sidebar.SyncActiveSession(m.appState.ActiveSessionID)
	m.previewPollGen++
	m.appState.PreviewContent = ""
	m.preview.SetContent("")
	cmds := []tea.Cmd{tea.EnableMouseCellMotion, m.schedulePollPreview()}
	if m.HasView(ViewGrid) {
		cmds = append(cmds, m.scheduleGridPoll())
	}
	return m, tea.Batch(cmds...)
}

func (m Model) handleSessionDetached() (tea.Model, tea.Cmd) {
	m.previewPollGen++ // returning from native attach; start fresh poll chain
	m.appState.PreviewContent = ""
	m.preview.SetContent("")
	return m, m.schedulePollPreview()
}

func (m Model) handleSessionWindowGone(msg components.SessionWindowGoneMsg) (tea.Model, tea.Cmd) {
	debugLog.Printf("session window gone: %s", msg.SessionID)
	m.appState = *state.RemoveSession(&m.appState, msg.SessionID)
	delete(m.stableCounts, msg.SessionID)
	delete(m.contentSnapshots, msg.SessionID)
	m.commitState()
	// Switch to whichever session the sidebar now has selected.
	m.syncActiveFromSidebar()
	m.previewPollGen++ // invalidate any in-flight polls for the removed session
	return m, m.schedulePollPreview()
}

func (m Model) handleTitleDetected(msg escape.TitleDetectedMsg) (tea.Model, tea.Cmd) {
	sess := m.appState.ActiveSession()
	if sess != nil && msg.SessionID == sess.ID {
		if sess.TitleSource != state.TitleSourceUser || m.cfg.AgentTitleOverridesUserTitle {
			m.appState = *state.UpdateSessionTitle(&m.appState, msg.SessionID, msg.Title, state.TitleSourceAgent)
			m.commitState()
		}
	}
	return m, m.scheduleWatchTitles()
}

func (m Model) handleStatusesDetected(msg escape.StatusesDetectedMsg) (tea.Model, tea.Cmd) {
	// Update content snapshots and stable counts so the next diff is accurate.
	// Skip sessions that no longer exist in appState — a late tick from a
	// previously scheduled WatchStatuses can deliver data for killed sessions.
	for sessionID, content := range msg.Contents {
		if state.FindSession(&m.appState, sessionID) == nil {
			continue
		}
		prev := m.contentSnapshots[sessionID]
		m.contentSnapshots[sessionID] = content
		if content != prev {
			m.stableCounts[sessionID] = 0 // content changed, reset debounce
		} else {
			m.stableCounts[sessionID]++ // content unchanged, increment
		}
	}
	// Wholesale-replace the pane titles map.  Dead-session entries naturally
	// fall out without explicit cleanup, and the grid renders from this map
	// directly when its subtitle row is enabled.
	if msg.Titles != nil {
		m.paneTitles = msg.Titles
		if m.HasView(ViewGrid) {
			m.gridView.SetPaneTitles(m.paneTitles)
		}
	}
	// If the status watcher captured new content for the active session, update
	// the preview immediately rather than waiting for the next PollPreview tick.
	if content, ok := msg.Contents[m.appState.ActiveSessionID]; ok {
		m.appState.PreviewContent = content
		m.preview.SetContent(content)
	}
	// Forward terminal bell and mark sessions with pending bell indicator.
	// The tmux bell flag stays set until the window is selected, so we use
	// bellPending as edge tracking: only emit \a for sessions that aren't
	// already marked as pending.  bellPending is cleared on attach.
	changed := false
	if len(msg.Bells) > 0 {
		// Build reverse map: target → sessionID for visual indicator.
		targetToSession := make(map[string]string, len(m.contentSnapshots))
		for _, sess := range state.AllSessions(&m.appState) {
			targetToSession[mux.Target(sess.TmuxSession, sess.TmuxWindow)] = sess.ID
		}
		newBell := false
		for target := range msg.Bells {
			if sid, ok := targetToSession[target]; ok {
				if !m.bellPending[sid] {
					newBell = true
					m.bellPending[sid] = true
				}
			}
		}
		if newBell && time.Since(m.lastBellTime) > 500*time.Millisecond {
			os.Stdout.Write([]byte("\a"))
			m.lastBellTime = time.Now()
		}
		if newBell {
			changed = true // sidebar needs to show bell badges
		}
	}

	for sessionID, status := range msg.Statuses {
		sess := state.FindSession(&m.appState, sessionID)
		if sess != nil && sess.Status != status {
			m.appState = *state.UpdateSessionStatus(&m.appState, sessionID, status)
			changed = true
		}
	}
	if changed {
		m.sidebar.Rebuild(&m.appState)
		m.sidebar.SetBellPending(m.bellPending)
	}
	return m, m.scheduleWatchStatuses()
}
