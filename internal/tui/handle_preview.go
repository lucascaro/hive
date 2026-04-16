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
	if !msg.Fast && msg.Generation != m.gridPollGen {
		debugLog.Printf("grid poll msg STALE gen=%d want=%d — discarded", msg.Generation, m.gridPollGen)
		return m, nil
	}
	if !msg.Fast {
		// Background poll: stamp activity pips, set full content, reschedule.
		var pipCmd tea.Cmd
		for sessID := range msg.Contents {
			if cmd := m.stampPreviewPoll(sessID); cmd != nil && pipCmd == nil {
				pipCmd = cmd
			}
		}
		m.gridView.SetContents(msg.Contents)
		if !m.HasView(ViewGrid) {
			return m, pipCmd
		}
		return m, tea.Batch(pipCmd, m.scheduleGridPoll())
	}
	// Fast poll: merge focused session content only. Do NOT stamp the
	// activity pip — the focused session is already stamped every background
	// tick, and stamping on the 50ms fast loop would make its pip flash ~5x
	// faster than other sessions.
	m.gridView.MergeContents(msg.Contents)
	if !m.HasView(ViewGrid) {
		return m, nil
	}
	if m.gridView.InputMode() {
		return m, m.scheduleFocusedSessionPoll()
	}
	return m, nil
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

// activityPipTickMsg drives redraws so the activity pip flash fades out
// cleanly even when no other messages are flowing.
type activityPipTickMsg struct{}

// scheduleActivityPipTick returns a tea.Cmd that fires activityPipTickMsg
// every 150 ms. Lightweight — no IO, just a redraw trigger.
func (m *Model) scheduleActivityPipTick() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(_ time.Time) tea.Msg {
		return activityPipTickMsg{}
	})
}

func (m Model) handleActivityPipTick() (tea.Model, tea.Cmd) {
	if !m.hasRecentActivity() {
		m.activityPipRunning = false
		return m, nil
	}
	return m, m.scheduleActivityPipTick()
}

func (m *Model) hasRecentActivity() bool {
	if len(m.lastPreviewChange) == 0 {
		return false
	}
	now := time.Now()
	for _, t := range m.lastPreviewChange {
		if now.Sub(t) < 300*time.Millisecond {
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

func (m *Model) scheduleGridPoll() tea.Cmd {
	sessions := m.gridSessions(m.gridView.Mode)
	if len(sessions) == 0 {
		return nil
	}
	interval := time.Duration(m.cfg.PreviewRefreshMs) * time.Millisecond
	if m.gridView.InputMode() {
		// In input mode, slow down the background poll to free CPU for the
		// focused session's fast 50ms loop. The focused session is excluded
		// from the batch (it's already captured by the fast poll).
		slow := time.Duration(inputModeBackgroundMs) * time.Millisecond
		if slow > interval {
			interval = slow
		}
		// Exclude the focused session — already captured by the fast poll.
		sel := m.gridView.Selected()
		if sel != nil {
			filtered := make([]*state.Session, 0, len(sessions))
			for _, s := range sessions {
				if s.ID != sel.ID {
					filtered = append(filtered, s)
				}
			}
			sessions = filtered
		}
		if len(sessions) == 0 {
			return nil
		}
	}
	return components.PollGridPreviews(sessions, interval, m.gridPollGen)
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
	if m.HasView(ViewGrid) {
		debugLog.Printf("schedulePollPreview: grid visible, skipping sidebar preview poll")
		return nil
	}
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
