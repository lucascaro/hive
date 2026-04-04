package tui

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/escape"
	"github.com/lucascaro/hive/internal/git"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
)

var (
	debugLog     = log.New(os.Stderr, "[hive] ", log.Ltime)
	debugLogOnce sync.Once
)

func initDebugLog() {
	debugLogOnce.Do(func() {
		f, err := os.OpenFile(config.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		debugLog = log.New(f, "[hive] ", log.Ltime|log.Lmicroseconds)
	})
}

// Model is the root Bubble Tea model.
type Model struct {
	cfg          config.Config
	appState     state.AppState
	keys         KeyMap
	sidebar      components.Sidebar
	preview      components.Preview
	statusBar    components.StatusBar
	titleEditor  components.TitleEditor
	agentPicker  components.AgentPicker
	teamBuilder  components.TeamBuilder
	confirm      components.Confirm
	gridView     components.GridView
	orphanPicker    components.OrphanPicker
	recoveryPicker  components.RecoveryPicker
	settings     components.SettingsView
	dirPicker    components.DirPicker
	nameInput    textinput.Model // for project name / directory input
	// UI sub-states
	inputMode          string // "project-name", "project-dir-confirm", "new-session", "worktree-branch", ""
	pendingProjectName string // name entered in step 1 of project creation
	pendingProjectID   string
	pendingAgentType   string // agent type awaiting install confirmation
	// Worktree session creation
	pendingWorktree          bool   // true when the next session should use a worktree
	pendingWorktreeAgentType string // agent type selected for worktree session
	pendingWorktreeAgentCmd  []string
	// Attach hint overlay
	showAttachHint bool
	pendingAttach  *SessionAttachMsg
	// attachPending is set before tea.Quit so cmd/start.go can handle the
	// attach when the native backend is active (which cannot use tea.ExecProcess).
	attachPending *SessionAttachMsg
	// previewPollGen is incremented each time we intentionally start a fresh
	// polling cycle (session switch, new session, detach).  PreviewUpdatedMsg
	// events whose Generation doesn't match are discarded without rescheduling,
	// which lets stale concurrent poll goroutines die off naturally instead of
	// accumulating and causing rapid-fire re-renders.
	previewPollGen uint64
	// contentSnapshots holds the last captured pane content for each session,
	// used by the status watcher to detect activity via content diffing.
	contentSnapshots map[string]string
	// stateLastKnownMtime is the modification time of state.json as of our most
	// recent write or reload.  The background watcher compares against this to
	// detect writes made by other hive instances.
	stateLastKnownMtime time.Time
}

// LastAttach returns the pending attach request after the TUI exits, or nil.
func (m Model) LastAttach() *SessionAttachMsg { return m.attachPending }

// New creates the root model.
func New(cfg config.Config, appState state.AppState) Model {
	initDebugLog()
	components.InitSidebarLog()
	components.InitStatusLog()
	components.InitPreviewLog()
	km := NewKeyMap(cfg.Keybindings)
	ni := textinput.New()
	ni.CharLimit = 256
	ni.Width = 48
	ni.Placeholder = "Project name"

	// Snapshot the current mtime of state.json so the watcher can detect
	// writes made by other hive instances without treating our own writes
	// as external changes.
	var initialStateMtime time.Time
	if info, err := os.Stat(config.StatePath()); err == nil {
		initialStateMtime = info.ModTime()
	}

	m := Model{
		cfg:                 cfg,
		appState:            appState,
		keys:                km,
		stateLastKnownMtime: initialStateMtime,
		titleEditor:         components.NewTitleEditor(),
		agentPicker:         components.NewAgentPicker(),
		teamBuilder:         components.NewTeamBuilder(),
		settings:            components.NewSettingsView(),
		orphanPicker:        components.NewOrphanPicker(appState.OrphanSessions),
		dirPicker:           components.NewDirPicker(),
		recoveryPicker:      components.NewRecoveryPicker(appState.RecoverableSessions),
		nameInput:           ni,
		contentSnapshots:    make(map[string]string),
	}
	// Clear the transient fields now that the pickers own their lists.
	m.appState.OrphanSessions = nil
	m.appState.RecoverableSessions = nil
	m.sidebar.Rebuild(&m.appState)
	// Sync sidebar cursor to the active session (set by caller on re-entry after detach),
	// or auto-select the first available session on fresh start.
	if m.appState.ActiveSessionID == "" {
		for i, item := range m.sidebar.Items {
			if item.Kind == components.KindSession {
				m.appState.ActiveSessionID = item.SessionID
				m.appState.ActiveProjectID = item.ProjectID
				m.sidebar.Cursor = i
				debugLog.Printf("auto-selected session %s at sidebar index %d", item.SessionID, i)
				break
			}
		}
	} else {
		// Ensure the active session's parent project/team is expanded so SyncActiveSession
		// can locate it in the sidebar item list (collapsed parents hide their children).
		expandForActiveSession(&m.appState, m.appState.ActiveSessionID)
		m.sidebar.Rebuild(&m.appState)
		m.sidebar.SyncActiveSession(m.appState.ActiveSessionID)
		debugLog.Printf("synced cursor to existing active session %s", m.appState.ActiveSessionID)
	}
	// Restore the grid view if the user detached from a grid-initiated session.
	m.restoreGrid()
	debugLog.Printf("New() done: ActiveSessionID=%q, %d projects, %d sidebar items",
		m.appState.ActiveSessionID, len(m.appState.Projects), len(m.sidebar.Items))
	return m
}

// restoreGrid re-opens the grid view if RestoreGridMode is set, then clears the flag.
func (m *Model) restoreGrid() {
	if m.appState.RestoreGridMode == state.GridRestoreNone {
		return
	}
	mode := m.appState.RestoreGridMode
	m.appState.RestoreGridMode = state.GridRestoreNone
	sessions := m.gridSessions(mode)
	m.gridView.Show(sessions, mode)
	m.gridView.SetProjectNames(m.gridProjectNames())
	m.gridView.SetContents(m.gridContentsFromSnapshots(sessions))
	m.gridView.SyncCursor(m.appState.ActiveSessionID)
}

// expandForActiveSession un-collapses any parent project or team that contains
// the given session, so the sidebar can include it in its item list.
func expandForActiveSession(appState *state.AppState, sessionID string) {
	if sessionID == "" {
		return
	}
	for _, p := range appState.Projects {
		for _, sess := range p.Sessions {
			if sess.ID == sessionID {
				p.Collapsed = false
				return
			}
		}
		for _, t := range p.Teams {
			for _, sess := range t.Sessions {
				if sess.ID == sessionID {
					p.Collapsed = false
					t.Collapsed = false
					return
				}
			}
		}
	}
}

// Init returns the initial commands.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tea.SetWindowTitle("hive"),
		m.schedulePollPreview(),
		m.scheduleWatchTitles(),
		m.scheduleWatchStatuses(),
		scheduleWatchState(m.stateLastKnownMtime),
	}
	if m.gridView.Active {
		cmds = append(cmds, m.scheduleGridPoll())
	}
	return tea.Batch(cmds...)
}

// Update handles all messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)
	case components.PreviewUpdatedMsg:
		return m.handlePreviewUpdated(msg)
	case components.SessionWindowGoneMsg:
		return m.handleSessionWindowGone(msg)
	case escape.TitleDetectedMsg:
		return m.handleTitleDetected(msg)
	case escape.StatusesDetectedMsg:
		return m.handleStatusesDetected(msg)
	case SessionCreatedMsg:
		return m.handleSessionCreated(msg)
	case SessionKilledMsg:
		return m.handleSessionKilled(msg)
	case SessionTitleChangedMsg:
		return m.handleSessionTitleChanged(msg)
	case SessionAttachMsg:
		return m.handleSessionAttach(msg)
	case AttachDoneMsg:
		return m.handleAttachDone(msg)
	case SessionDetachedMsg:
		return m.handleSessionDetached()
	case TeamCreatedMsg:
		return m.handleTeamCreated(msg)
	case TeamKilledMsg:
		return m.handleTeamKilled(msg)
	case ProjectCreatedMsg:
		return m.handleProjectCreated(msg)
	case ProjectKilledMsg:
		return m.handleProjectKilled(msg)
	case components.DirPickedMsg:
		return m.handleDirPicked(msg)
	case components.DirPickerCancelMsg:
		return m.handleDirPickerCancel()
	case ConfirmActionMsg:
		return m.handleConfirmAction(msg)
	case ConfirmedMsg:
		return m.handleConfirmed(msg)
	case components.CancelledMsg:
		return m.handleCancelled()
	case components.GridPreviewsUpdatedMsg:
		return m.handleGridPreviewsUpdated(msg)
	case components.GridSessionSelectedMsg:
		return m.handleGridSessionSelected(msg)
	case components.AgentPickedMsg:
		return m.handleAgentPicked(msg)
	case AgentInstalledMsg:
		return m.handleAgentInstalled(msg)
	case components.RecoveryPickerDoneMsg:
		return m.handleRecoveryPickerDone(msg)
	case components.OrphanPickerDoneMsg:
		return m.handleOrphanPickerDone(msg)
	case components.TeamBuiltMsg:
		return m.handleTeamBuilt(msg)
	case ErrorMsg:
		return m.handleError(msg)
	case PersistMsg:
		return m.handlePersist()
	case stateWatchMsg:
		return m.handleStateWatch(msg)
	case QuitAndKillMsg:
		return m.handleQuitAndKill()
	case components.SettingsSaveRequestMsg:
		return m.handleSettingsSaveRequest(msg)
	case components.SettingsClosedMsg:
		return m.handleSettingsClosed()
	case ConfigSavedMsg:
		return m.handleConfigSaved()
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}

	// Forward non-key, non-handled messages to active modals that need
	// internal ticks (e.g. cursor blink, filter debounce in charmbracelet/list).
	if m.dirPicker.Active {
		m.dirPicker.Update(msg)
	}

	return m, nil
}

// View renders the full UI.
func (m Model) View() string {
	// Settings screen fills the full terminal.
	if m.settings.Active {
		m.settings.Width = m.appState.TermWidth
		m.settings.Height = m.appState.TermHeight
		return m.settings.View()
	}
	// Grid overview fills the full terminal.
	if m.gridView.Active {
		m.gridView.Width = m.appState.TermWidth
		m.gridView.Height = m.appState.TermHeight
		return m.gridView.View()
	}
	// Show overlays first.
	if m.appState.ShowHelp {
		return m.helpView()
	}
	if m.appState.ShowTmuxHelp {
		return m.tmuxHelpView()
	}
	if m.showAttachHint {
		return m.overlayView(m.attachHintView())
	}
	if m.appState.ShowConfirm {
		return m.overlayView(m.confirm.View())
	}
	if m.recoveryPicker.Active {
		m.recoveryPicker.Width = m.appState.TermWidth
		m.recoveryPicker.Height = m.appState.TermHeight
		return m.overlayView(m.recoveryPicker.View())
	}
	if m.orphanPicker.Active {
		m.orphanPicker.Width = m.appState.TermWidth
		m.orphanPicker.Height = m.appState.TermHeight
		return m.overlayView(m.orphanPicker.View())
	}
	if m.agentPicker.Active {
		return m.overlayView(m.agentPicker.View())
	}
	if m.teamBuilder.Active {
		return m.overlayView(m.teamBuilder.View())
	}
	if m.inputMode == "project-name" {
		return m.overlayView(m.nameInputView("New Project (1/2)", "Project name:", "enter: next  esc: cancel"))
	}
	if m.dirPicker.Active {
		return m.overlayView(m.dirPicker.View())
	}
	if m.inputMode == "project-dir-confirm" {
		return m.overlayView(m.dirConfirmView())
	}
	if m.inputMode == "custom-command" {
		return m.overlayView(m.nameInputView("Custom Command", "Command to run:", "enter: create  esc: cancel"))
	}
	if m.inputMode == "worktree-branch" {
		return m.overlayView(m.nameInputView("New Worktree Session", "Branch name:", "enter: create  esc: cancel"))
	}
	if m.appState.EditingTitle {
		return m.overlayView(m.renameDialogView())
	}

	sw, pw, ch := computeLayout(m.appState.TermWidth, m.appState.TermHeight)
	m.sidebar.Width = sw
	m.sidebar.Height = ch
	m.preview.Resize(pw, ch)
	m.statusBar.Width = m.appState.TermWidth

	sidebarView := m.sidebar.View(m.appState.ActiveSessionID, m.appState.FocusedPane == state.PaneSidebar)
	previewView := m.preview.View(m.appState.ActiveSessionID)
	statusView := m.statusBar.View(&m.appState, m.appState.FocusedPane, m.appState.FilterActive, m.appState.FilterQuery)

	sidebarLines := strings.Count(sidebarView, "\n") + 1
	previewLines := strings.Count(previewView, "\n") + 1
	statusLines := strings.Count(statusView, "\n") + 1

	main := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, previewView)
	mainLines := strings.Count(main, "\n") + 1
	out := lipgloss.JoinVertical(lipgloss.Left, main, statusView)
	outLines := strings.Count(out, "\n") + 1

	sidebarW := strings.IndexByte(sidebarView, '\n')
	if sidebarW < 0 {
		sidebarW = len(sidebarView)
	}
	previewW := strings.IndexByte(previewView, '\n')
	if previewW < 0 {
		previewW = len(previewView)
	}
	debugLog.Printf("View: term=%dx%d sw=%d pw=%d ch=%d | sidebar=%dx%d preview=%dx%d status=%dx%d | main=%d total=%d expected=%d%s",
		m.appState.TermWidth, m.appState.TermHeight, sw, pw, ch,
		sidebarW, sidebarLines,
		previewW, previewLines,
		m.appState.TermWidth, statusLines,
		mainLines, outLines, m.appState.TermHeight,
		func() string {
			if outLines != m.appState.TermHeight {
				return fmt.Sprintf(" FRAME_HEIGHT_MISMATCH(off_by=%d)", outLines-m.appState.TermHeight)
			}
			return ""
		}(),
	)
	if sidebarLines != ch {
		debugLog.Printf("View WARNING: sidebar height mismatch: got=%d want=%d (sw=%d innerW=%d items=%d)",
			sidebarLines, ch, sw, sw-2, len(m.sidebar.Items))
	}
	if previewLines != ch {
		debugLog.Printf("View WARNING: preview height mismatch: got=%d want=%d (pw=%d contentLen=%d contentLines=%d)",
			previewLines, ch, pw, len(m.appState.PreviewContent),
			strings.Count(m.appState.PreviewContent, "\n")+1)
	}
	if statusLines != statusBarHeight {
		debugLog.Printf("View WARNING: statusbar height mismatch: got=%d want=%d (termW=%d)",
			statusLines, statusBarHeight, m.appState.TermWidth)
	}
	if mainLines != ch {
		debugLog.Printf("View WARNING: JoinHorizontal height mismatch: got=%d want=%d (sidebar=%d preview=%d)",
			mainLines, ch, sidebarLines, previewLines)
	}

	out = strings.TrimRight(out, "\n")
	outLines = strings.Count(out, "\n") + 1
	switch {
	case outLines < m.appState.TermHeight:
		out += strings.Repeat("\n", m.appState.TermHeight-outLines)
	case outLines > m.appState.TermHeight:
		nl := 0
		for i := 0; i < len(out); i++ {
			if out[i] == '\n' {
				nl++
				if nl == m.appState.TermHeight {
					out = out[:i]
					break
				}
			}
		}
	}
	return out
}

func (m *Model) recomputeLayout() {
	sw, pw, ch := computeLayout(m.appState.TermWidth, m.appState.TermHeight)
	m.sidebar.Width = sw
	m.sidebar.Height = ch
	m.preview.Resize(pw, ch)
	m.statusBar.Width = m.appState.TermWidth
}

// commitState rebuilds the sidebar from the current app state and persists to disk.
func (m *Model) commitState() {
	m.sidebar.Rebuild(&m.appState)
	m.persist()
}

func (m *Model) persist() {
	if mtime, err := saveState(&m.appState); err != nil {
		log.Printf("hive: failed to save state: %v", err)
	} else {
		m.stateLastKnownMtime = mtime
	}
	if err := saveUsage(m.appState.AgentUsage); err != nil {
		log.Printf("hive: failed to save usage: %v", err)
	}
}

// reloadStateFromDisk reads the current state.json written by another hive
// instance, reconciles it against the live tmux backend (removing sessions
// whose windows are gone), and merges the result into the running TUI.
func (m *Model) reloadStateFromDisk() {
	projects, err := LoadState()
	if err != nil {
		debugLog.Printf("reloadStateFromDisk: LoadState error: %v", err)
		return
	}
	if projects == nil {
		projects = []*state.Project{}
	}

	tmp := &state.AppState{Projects: projects}

	type windowSet = map[int]struct{}
	windowCache := make(map[string]windowSet)
	windowsFor := func(tmuxSess string) (windowSet, bool) {
		if ws, ok := windowCache[tmuxSess]; ok {
			return ws, true
		}
		windows, err := mux.ListWindows(tmuxSess)
		if err != nil {
			windowCache[tmuxSess] = nil
			return nil, false
		}
		ws := make(windowSet, len(windows))
		for _, w := range windows {
			ws[w.Index] = struct{}{}
		}
		windowCache[tmuxSess] = ws
		return ws, true
	}

	type worktreeRef struct{ workDir, worktreePath string }
	var deadWorktrees []worktreeRef
	var deadIDs []string
	for _, sess := range state.AllSessions(tmp) {
		ws, ok := windowsFor(sess.TmuxSession)
		if !ok {
			deadIDs = append(deadIDs, sess.ID)
			if sess.WorktreePath != "" {
				deadWorktrees = append(deadWorktrees, worktreeRef{sess.WorkDir, sess.WorktreePath})
			}
			continue
		}
		if _, found := ws[sess.TmuxWindow]; !found {
			deadIDs = append(deadIDs, sess.ID)
			if sess.WorktreePath != "" {
				deadWorktrees = append(deadWorktrees, worktreeRef{sess.WorkDir, sess.WorktreePath})
			}
		}
	}

	for _, wt := range deadWorktrees {
		if gitRoot, gerr := git.Root(wt.workDir); gerr == nil {
			if rmErr := git.RemoveWorktree(gitRoot, wt.worktreePath); rmErr != nil {
				debugLog.Printf("reloadStateFromDisk: remove worktree %s: %v", wt.worktreePath, rmErr)
			}
		} else {
			debugLog.Printf("reloadStateFromDisk: git root for %s: %v; removing worktree directory directly", wt.workDir, gerr)
			if rmErr := os.RemoveAll(wt.worktreePath); rmErr != nil {
				debugLog.Printf("reloadStateFromDisk: remove worktree directory %s: %v", wt.worktreePath, rmErr)
			}
		}
	}
	for _, id := range deadIDs {
		tmp = state.RemoveSession(tmp, id)
	}

	m.appState.Projects = tmp.Projects
	if m.appState.ActiveSessionID != "" {
		if state.FindSession(&m.appState, m.appState.ActiveSessionID) == nil {
			debugLog.Printf("reloadStateFromDisk: active session %s no longer exists, clearing", m.appState.ActiveSessionID)
			m.appState.ActiveSessionID = ""
			m.appState.ActiveProjectID = ""
			m.appState.ActiveTeamID = ""
		}
	}

	if len(deadIDs) > 0 {
		m.persist()
	}

	m.sidebar.Rebuild(&m.appState)
	if m.appState.ActiveSessionID == "" {
		m.syncActiveFromSidebar()
	} else {
		m.sidebar.SyncActiveSession(m.appState.ActiveSessionID)
	}
	m.previewPollGen++
	debugLog.Printf("reloadStateFromDisk: done — %d projects, %d dead sessions removed, activeSession=%s",
		len(m.appState.Projects), len(deadIDs), m.appState.ActiveSessionID)
}
