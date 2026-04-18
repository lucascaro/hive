package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

func (m Model) handlePreviewUpdated(msg components.PreviewUpdatedMsg) (tea.Model, tea.Cmd) {
	if m.polling.IsStale(msg.Generation) {
		// Stale poll from a previous session or navigation — discard without
		// rescheduling so the old polling goroutine dies off naturally.
		debugLog.Printf("preview msg STALE gen=%d want=%d session=%s — discarded",
			msg.Generation, m.polling.Generation(), msg.SessionID)
		return m, nil
	}
	// Skip the activity-pip stamp while grid is on top: the grid poll already
	// captures every session (including the active one) on its own cadence, so
	// stamping here would double-stamp the active session's pip relative to
	// the others, making it look like it polls twice as fast.
	var pipCmd tea.Cmd
	if !m.HasView(ViewGrid) {
		pipCmd = m.stampPreviewPoll(msg.SessionID)
	}
	if msg.SessionID == m.appState.ActiveSessionID {
		m.appState.PreviewContent = msg.Content
		m.preview.SetContent(msg.Content)
		debugLog.Printf("preview updated: session=%s contentLen=%d gen=%d", msg.SessionID, len(msg.Content), msg.Generation)
	} else {
		debugLog.Printf("preview msg ignored: msg.session=%s active=%s gen=%d", msg.SessionID, m.appState.ActiveSessionID, msg.Generation)
	}
	return m, tea.Batch(pipCmd, m.schedulePollPreview())
}

func (m Model) handleGridPreviewsUpdated(msg components.GridPreviewsUpdatedMsg) (tea.Model, tea.Cmd) {
	// Discard stale background ticks from a previous chain (e.g. spawned by a
	// prior g/G mode toggle that started a parallel loop). Stamping or
	// rescheduling stale messages would multiply the effective polling rate.
	if !msg.Fast && m.polling.IsStale(msg.Generation) {
		debugLog.Printf("grid poll msg STALE gen=%d want=%d — discarded", msg.Generation, m.polling.Generation())
		return m, nil
	}
	if !msg.Fast {
		// Background poll: stamp activity pips, set content, reschedule.
		var pipCmd tea.Cmd
		for sessID := range msg.Contents {
			if cmd := m.stampPreviewPoll(sessID); cmd != nil && pipCmd == nil {
				pipCmd = cmd
			}
		}
		if msg.Partial {
			// Partial batch (e.g. input mode excluded the focused session).
			// MergeContents preserves excluded sessions' content — SetContents
			// would blank them, causing a visible flash.
			m.gridView.MergeContents(msg.Contents)
		} else {
			m.gridView.SetContents(msg.Contents)
		}
		if !m.HasView(ViewGrid) {
			return m, pipCmd
		}
		return m, tea.Batch(pipCmd, m.scheduleGridPoll())
	}
	// Fast poll: merge focused session content and stamp its pip.
	// The pip stays continuously lit (50ms < 150ms flash window), which is
	// the correct signal: "this session is actively monitored in input mode."
	var pipCmd tea.Cmd
	for sessID := range msg.Contents {
		if cmd := m.stampPreviewPoll(sessID); cmd != nil && pipCmd == nil {
			pipCmd = cmd
		}
	}
	m.gridView.MergeContents(msg.Contents)
	if !m.HasView(ViewGrid) {
		return m, pipCmd
	}
	if m.gridView.InputMode() {
		return m, tea.Batch(pipCmd, m.scheduleFocusedSessionPoll())
	}
	return m, pipCmd
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

// stampPreviewPoll records the time a session's preview was most recently
// polled. Drives the activity pip flash — fires on every poll regardless of
// whether the captured content changed.
func (m *Model) stampPreviewPoll(sessID string) tea.Cmd {
	if sessID == "" {
		return nil
	}
	if m.lastPreviewChange == nil {
		m.lastPreviewChange = make(map[string]time.Time)
	}
	m.lastPreviewChange[sessID] = time.Now()
	return m.ensureActivityPipRunning()
}

// Activity pip timing. The flash duration must be < the tick interval so the
// "off" frame renders between stamps. In grid input mode we run faster (25ms)
// to drive the rotating progress-pie animation; otherwise 150ms is enough.
const (
	ActivityPipFlashNormal = 150 * time.Millisecond
	ActivityPipFlashInput  = 25 * time.Millisecond
	activityPipIdleThresh  = 300 * time.Millisecond
	activityPipInputThresh = 100 * time.Millisecond
)

// activityPipTickMsg drives redraws so the activity pip flash fades out
// cleanly even when no other messages are flowing.
type activityPipTickMsg struct{}

// gridInputActive reports whether the grid view is on top and input mode is on.
// Used to gate input-mode-specific behavior across pip ticks, polling, and rendering.
func (m *Model) gridInputActive() bool {
	return m.HasView(ViewGrid) && m.gridView.InputMode()
}

// scheduleActivityPipTick returns a tea.Cmd that fires activityPipTickMsg
// at ActivityPipFlashNormal normally, or ActivityPipFlashInput while grid
// input mode is active so the focused-session pip can drive the rotating
// progress-pie animation. Lightweight — no IO, just a redraw trigger.
func (m *Model) scheduleActivityPipTick() tea.Cmd {
	interval := ActivityPipFlashNormal
	if m.gridInputActive() {
		interval = ActivityPipFlashInput
	}
	return tea.Tick(interval, func(_ time.Time) tea.Msg {
		return activityPipTickMsg{}
	})
}

func (m Model) handleActivityPipTick() (tea.Model, tea.Cmd) {
	if !m.hasRecentActivity() {
		m.activityPipRunning = false
		return m, nil
	}
	m.pipFrame++
	return m, m.scheduleActivityPipTick()
}

func (m *Model) hasRecentActivity() bool {
	if len(m.lastPreviewChange) == 0 {
		return false
	}
	// Keep the ticker alive slightly longer than the flash duration so the
	// "off" frame renders after the pip expires.
	threshold := activityPipIdleThresh
	if m.gridInputActive() {
		threshold = activityPipInputThresh
	}
	now := time.Now()
	for _, t := range m.lastPreviewChange {
		if now.Sub(t) < threshold {
			return true
		}
	}
	return false
}

func (m *Model) ensureActivityPipRunning() tea.Cmd {
	if m.activityPipRunning {
		return nil
	}
	m.activityPipRunning = true
	return m.scheduleActivityPipTick()
}

// scheduleBellBlink returns a tea.Cmd that fires bellBlinkMsg after 600 ms.
// The Model reschedules it on every tick, producing a continuous toggle animation
// independent of terminal ANSI blink support.
func (m *Model) scheduleBellBlink() tea.Cmd {
	return tea.Tick(600*time.Millisecond, func(_ time.Time) tea.Msg {
		return bellBlinkMsg{}
	})
}

func (m *Model) ensureBellBlinkRunning() tea.Cmd {
	if m.bellBlinkRunning {
		return nil
	}
	m.bellBlinkRunning = true
	return m.scheduleBellBlink()
}

// inputModeBackgroundMs is the background poll interval (non-focused sessions)
// during input mode — 2× slower than the default 500ms to free CPU for the
// focused session's fast 50ms poll.
const inputModeBackgroundMs = 1000

// inputModeFocusedMs is the focused-session poll interval during input mode.
const inputModeFocusedMs = 50

// scheduleGridPoll delegates to the PollingManager with current grid state.
func (m *Model) scheduleGridPoll() tea.Cmd {
	sessions := m.gridSessions(m.gridView.Mode)
	focusedID := ""
	if sel := m.gridView.Selected(); sel != nil {
		focusedID = sel.ID
	}
	return m.polling.ScheduleGridPoll(sessions, m.gridView.InputMode(), focusedID)
}

// scheduleFocusedSessionPoll delegates to the PollingManager.
func (m *Model) scheduleFocusedSessionPoll() tea.Cmd {
	return m.polling.ScheduleFocusedPoll(m.gridView.Selected())
}

// schedulePollPreview delegates to the PollingManager.
func (m *Model) schedulePollPreview() tea.Cmd {
	if m.HasView(ViewGrid) {
		debugLog.Printf("schedulePollPreview: grid visible, skipping sidebar preview poll")
		return nil
	}
	sess := m.appState.ActiveSession()
	if sess == nil {
		debugLog.Printf("schedulePollPreview: no active session (ActiveSessionID=%q)", m.appState.ActiveSessionID)
		return nil
	}
	debugLog.Printf("schedulePollPreview: session=%s tmux=%s:%d gen=%d",
		sess.ID, sess.TmuxSession, sess.TmuxWindow, m.polling.Generation())
	return m.polling.SchedulePreview(sess)
}

// scheduleWatchStatuses delegates to the PollingManager.
func (m *Model) scheduleWatchStatuses() tea.Cmd {
	return m.polling.ScheduleStatuses(state.AllSessions(&m.appState))
}

// scheduleWatchTitles delegates to the PollingManager.
func (m *Model) scheduleWatchTitles() tea.Cmd {
	return m.polling.ScheduleTitles(state.AllSessions(&m.appState))
}
