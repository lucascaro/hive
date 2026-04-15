package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/escape"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

func (m Model) handlePreviewUpdated(msg components.PreviewUpdatedMsg) (tea.Model, tea.Cmd) {
	if msg.Generation != m.previewPollGen {
		// Stale poll from a previous session or navigation — discard without
		// rescheduling so the old polling goroutine dies off naturally.
		debugLog.Printf("preview msg STALE gen=%d want=%d session=%s — discarded",
			msg.Generation, m.previewPollGen, msg.SessionID)
		return m, nil
	}
	if msg.SessionID == m.appState.ActiveSessionID {
		m.appState.PreviewContent = msg.Content
		m.preview.SetContent(msg.Content)
		debugLog.Printf("preview updated: session=%s contentLen=%d gen=%d", msg.SessionID, len(msg.Content), msg.Generation)
	} else {
		debugLog.Printf("preview msg ignored: msg.session=%s active=%s gen=%d", msg.SessionID, m.appState.ActiveSessionID, msg.Generation)
	}
	return m, m.schedulePollPreview()
}

func (m Model) handleGridPreviewsUpdated(msg components.GridPreviewsUpdatedMsg) (tea.Model, tea.Cmd) {
	if msg.Fast {
		// Fast poll only has the focused session — merge to preserve other cells.
		m.gridView.MergeContents(msg.Contents)
	} else {
		m.gridView.SetContents(msg.Contents)
	}
	if !m.HasView(ViewGrid) {
		return m, nil
	}
	if msg.Fast {
		// Fast-poll loop: reschedule only if still in input mode.
		if m.gridView.InputMode() {
			return m, m.scheduleFocusedSessionPoll()
		}
		return m, nil
	}
	// Background loop: always reschedule while grid is visible.
	return m, m.scheduleGridPoll()
}

func (m Model) handleGridSessionSelected(msg components.GridSessionSelectedMsg) (tea.Model, tea.Cmd) {
	// Do NOT pop ViewGrid here. Keeping it on the stack until detach is what
	// prevents the one-frame sidebar flash between Update returning and the
	// attach Cmd executing: for HideAttachHint=true, TopView() stays ViewGrid
	// through the Exec/Quit; for HideAttachHint=false, the hint is pushed on
	// top of ViewGrid. On detach (AttachDoneMsg path), restoreGrid() is made
	// idempotent so the grid is not pushed again when it is already on the
	// stack (tmux tea.Exec case). Esc on the hint naturally returns to
	// whichever view was under it (grid or main).
	var sessionTitle string
	var agentType state.AgentType
	var projectName string
	var status state.SessionStatus
	var worktreeBranch, worktreePath string
	if s := m.sessionByTmux(msg.TmuxSession, msg.TmuxWindow); s != nil {
		m.focusSession(s.ID)
		sessionTitle = s.Title
		agentType = s.AgentType
		projectName = m.projectNameByID(s.ProjectID)
		status = s.Status
		worktreeBranch = s.WorktreeBranch
		worktreePath = s.WorktreePath
	}
	attach := &SessionAttachMsg{
		TmuxSession:     msg.TmuxSession,
		TmuxWindow:      msg.TmuxWindow,
		RestoreGridMode: m.gridView.Mode,
		SessionTitle:    sessionTitle,
		AgentType:       agentType,
		ProjectName:     projectName,
		Status:          status,
		WorktreeBranch:  worktreeBranch,
		WorktreePath:    worktreePath,
	}
	if !m.cfg.HideAttachHint {
		m.pendingAttach = attach
		m.PushView(ViewAttachHint)
		return m, nil
	}
	cmd := m.doAttach(*attach)
	return m, cmd
}

// scheduleBellBlink returns a tea.Cmd that fires bellBlinkMsg after 600 ms.
// The Model reschedules it on every tick, producing a continuous toggle animation
// independent of terminal ANSI blink support.
func (m *Model) scheduleBellBlink() tea.Cmd {
	return tea.Tick(600*time.Millisecond, func(_ time.Time) tea.Msg {
		return bellBlinkMsg{}
	})
}

// inputModeBackgroundMs is the background poll interval (all sessions) during
// input mode — 2× faster than the default 500 ms.
const inputModeBackgroundMs = 250

// inputModeFocusedMs is the focused-session poll interval during input mode.
const inputModeFocusedMs = 50

func (m *Model) scheduleGridPoll() tea.Cmd {
	sessions := m.gridSessions(m.gridView.Mode)
	if len(sessions) == 0 {
		return nil
	}
	interval := time.Duration(m.cfg.PreviewRefreshMs) * time.Millisecond
	// In input mode use a faster background rate (250 ms, 2× the default) so
	// non-focused sessions are also more responsive. Capped at the configured
	// interval so tests that set PreviewRefreshMs=1 run at their requested speed.
	if m.gridView.InputMode() {
		fast := time.Duration(inputModeBackgroundMs) * time.Millisecond
		if fast < interval {
			interval = fast
		}
	}
	return components.PollGridPreviews(sessions, interval)
}

// scheduleFocusedSessionPoll returns a tea.Cmd that polls just the focused
// session at 50 ms. Used for the fast poll loop in input mode.
func (m *Model) scheduleFocusedSessionPoll() tea.Cmd {
	sel := m.gridView.Selected()
	if sel == nil {
		return nil
	}
	interval := time.Duration(inputModeFocusedMs) * time.Millisecond
	// Respect the configured interval if it's already faster (e.g. tests set 1 ms).
	if cfg := time.Duration(m.cfg.PreviewRefreshMs) * time.Millisecond; cfg < interval {
		interval = cfg
	}
	return components.PollFocusedGridPreview(sel, interval)
}

func (m *Model) schedulePollPreview() tea.Cmd {
	sess := m.appState.ActiveSession()
	if sess == nil {
		debugLog.Printf("schedulePollPreview: no active session (ActiveSessionID=%q)", m.appState.ActiveSessionID)
		return nil
	}
	interval := time.Duration(m.cfg.PreviewRefreshMs) * time.Millisecond
	debugLog.Printf("schedulePollPreview: session=%s tmux=%s:%d interval=%v gen=%d",
		sess.ID, sess.TmuxSession, sess.TmuxWindow, interval, m.previewPollGen)
	return components.PollPreview(sess.ID, sess.TmuxSession, sess.TmuxWindow, interval, m.previewPollGen)
}

func (m *Model) scheduleWatchTitles() tea.Cmd {
	targets := make(map[string]string)
	for _, sess := range state.AllSessions(&m.appState) {
		if sess.Status != state.StatusDead {
			targets[sess.ID] = mux.Target(sess.TmuxSession, sess.TmuxWindow)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	interval := time.Duration(m.cfg.PreviewRefreshMs*2) * time.Millisecond
	return escape.WatchTitles(targets, interval)
}

func (m *Model) scheduleWatchStatuses() tea.Cmd {
	targets := make(map[string]string)
	detection := make(map[string]escape.SessionDetectionCtx)
	for _, sess := range state.AllSessions(&m.appState) {
		if sess.Status != state.StatusDead {
			targets[sess.ID] = mux.Target(sess.TmuxSession, sess.TmuxWindow)
			if ctx, ok := m.detectionCtxs[string(sess.AgentType)]; ok {
				detection[sess.ID] = ctx
			}
		}
	}
	if len(targets) == 0 {
		return nil
	}
	// Batch-read pane titles and bell flags for all windows in the shared hive session.
	titles, bells, err := mux.GetPaneTitles(mux.HiveSession)
	if err != nil {
		debugLog.Printf("scheduleWatchStatuses: GetPaneTitles(%s): %v", mux.HiveSession, err)
		titles = make(map[string]string)
		bells = make(map[string]bool)
	} else {
		if titles == nil {
			titles = make(map[string]string)
		}
		if bells == nil {
			bells = make(map[string]bool)
		}
	}
	// Snapshot maps to avoid concurrent reads in the tick goroutine
	// while handleStatusesDetected writes on the main goroutine.
	prevContents := make(map[string]string, len(m.contentSnapshots))
	for k, v := range m.contentSnapshots {
		prevContents[k] = v
	}
	stableCounts := make(map[string]int, len(m.stableCounts))
	for k, v := range m.stableCounts {
		stableCounts[k] = v
	}
	interval := time.Duration(m.cfg.PreviewRefreshMs*2) * time.Millisecond
	return escape.WatchStatuses(targets, prevContents, stableCounts, detection, titles, bells, interval)
}
