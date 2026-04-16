package tui

import (
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/escape"
	"github.com/lucascaro/hive/internal/git"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
	"github.com/lucascaro/hive/internal/tui/styles"
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
	pendingSaveConfig  config.Config // config staged for save while confirm dialog is open
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
	// gridPollGen is incremented whenever we intentionally start a fresh grid
	// polling cycle (grid open, mode switch, attach return).  Background
	// GridPreviewsUpdatedMsg events whose Generation doesn't match are
	// discarded without rescheduling, so concurrent poll chains created by
	// rapid g/G mode toggles die off naturally instead of accumulating and
	// multiplying the polling rate.
	gridPollGen uint64
	// contentSnapshots holds the last captured pane content for each session,
	// used by the status watcher to detect activity via content diffing.
	contentSnapshots map[string]string
	// stableCounts tracks consecutive polls where content was unchanged per session,
	// used for debounce before transitioning running→idle/waiting.
	stableCounts map[string]int
	// paneTitles holds the most recent pane title (set by agents via OSC 0/2)
	// for each tmux target ("hive:N"), refreshed every status poll.  Keyed
	// by target rather than sessionID to match GetPaneTitles' shape and the
	// grid lookup.  Strictly transient — never persisted to disk.
	paneTitles map[string]string
	// detectionCtxs holds compiled status detection regexes per agent type,
	// built once at startup from config.StatusDetection.
	detectionCtxs map[string]escape.SessionDetectionCtx
	// lastBellTime is the time the last audible bell was forwarded to the
	// user's terminal. Used for 500ms debounce to prevent rapid-fire bells.
	lastBellTime time.Time
	// bellPending tracks sessions that have fired a bell since the user last
	// attached to them. Used for the visual bell indicator in the sidebar.
	// Keyed by sessionID, cleared on attach.
	bellPending map[string]bool
	// attachOut is the writer used by doAttach to emit pre-attach escape
	// sequences (e.g. the primary-buffer clear). Defaults to os.Stdout.
	// Tests may inject a bytes.Buffer to capture and assert the output.
	attachOut io.Writer
	// bellBlinkOn is toggled every ~600 ms by the bell-blink ticker so the
	// bell badge animates on/off in software (ANSI terminal blink is unreliable
	// in modern terminals like iTerm2 which disable it by default).
	bellBlinkOn      bool
	bellBlinkRunning bool
	// lastPreviewChange records the time at which each session's preview was
	// most recently polled. Drives the activity-pip flash. Not persisted.
	lastPreviewChange  map[string]time.Time
	activityPipRunning bool
	// pipFrame advances on every activityPipTickMsg. Used by the input-mode
	// focused-session pip to render a rotating "progress pie" animation
	// (○ → ◔ → ◑ → ◕ → ● → ...) instead of a binary on/off blink.
	pipFrame int
	// stateLastKnownMtime is the modification time of state.json as of our most
	// recent write or reload.  The background watcher compares against this to
	// detect writes made by other hive instances.
	stateLastKnownMtime time.Time
	// whatsNewContent holds the parsed changelog text for the "What's New" overlay.
	whatsNewContent  string
	whatsNewViewport viewport.Model
	// viewStack tracks the active view layers. ViewMain is always at the bottom.
	// Push to open a view, pop to close it. TopView() drives View() and key dispatch.
	viewStack []ViewID
	// helpModel renders the one-line key hints in the statusbar.
	// helpPanel renders the full tabbed help overlay (? / H keys).
	helpModel     help.Model
	gridHelpModel help.Model
	helpPanel     components.HelpPanel
}

// LastAttach returns the pending attach request after the TUI exits, or nil.
func (m Model) LastAttach() *SessionAttachMsg { return m.attachPending }

// newStyledHelp returns a help.Model pre-configured with Hive's color palette.
func newStyledHelp() help.Model {
	h := help.New()
	h.Styles.ShortKey = styles.HelpKeyStyle
	h.Styles.ShortDesc = styles.HelpDescStyle
	h.Styles.ShortSeparator = styles.MutedStyle
	h.Styles.FullKey = styles.HelpKeyStyle
	h.Styles.FullDesc = styles.HelpDescStyle
	h.Styles.FullSeparator = styles.MutedStyle
	h.Styles.Ellipsis = styles.MutedStyle
	return h
}

// New creates the root model. whatsNewContent, if non-empty, triggers the
// "What's New" overlay on first render.
func New(cfg config.Config, appState state.AppState, whatsNewContent string) Model {
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
		attachOut:           os.Stdout,
		titleEditor:         components.NewTitleEditor(),
		agentPicker:         components.NewAgentPicker(),
		teamBuilder:         components.NewTeamBuilder(),
		settings:            components.NewSettingsView(),
		orphanPicker:        components.NewOrphanPicker(appState.OrphanSessions),
		dirPicker:           components.NewDirPicker(),
		recoveryPicker:      components.NewRecoveryPicker(appState.RecoverableSessions),
		nameInput:           ni,
		contentSnapshots:  make(map[string]string),
		lastPreviewChange: make(map[string]time.Time),
		stableCounts:        make(map[string]int),
		paneTitles:          make(map[string]string),
		bellPending:         make(map[string]bool),
		detectionCtxs:       buildDetectionCtxs(cfg.Agents),
		viewStack:           []ViewID{ViewMain},
		helpModel:           newStyledHelp(),
		gridHelpModel:       newStyledHelp(),
		helpPanel:           components.NewHelpPanel(newStyledHelp()),
	}
	m.gridView.InputEnabled = !cfg.DisableGridInput
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
	// Push startup overlays onto the view stack if they were activated by constructors.
	if m.recoveryPicker.Active {
		m.PushView(ViewRecovery)
	}
	if m.orphanPicker.Active {
		m.PushView(ViewOrphan)
	}
	// Restore the grid view if the user detached from a grid-initiated session.
	m.restoreGrid()
	// Open the preferred startup view if no attach-restore already pushed a grid.
	// Use HasView instead of snapshotting RestoreGridMode: restoreGrid() clears
	// that field, so checking it after the call always reads None.
	if !m.HasView(ViewGrid) {
		switch m.cfg.StartupView {
		case "grid":
			m.openGrid(state.GridRestoreProject)
		case "grid-all":
			m.openGrid(state.GridRestoreAll)
		case "sidebar", "":
			// no-op: normal sidebar startup
		default:
			debugLog.Printf("unknown StartupView %q, defaulting to sidebar", m.cfg.StartupView)
		}
	}
	// Show "What's New" overlay if there's changelog content.
	// Pushed last so it appears on top of any restored grid or recovery overlays.
	if whatsNewContent != "" {
		m.whatsNewContent = whatsNewContent
		vp := viewport.New(60, 20)
		vp.SetContent(whatsNewContent)
		m.whatsNewViewport = vp
		m.PushView(ViewWhatsNew)
	}
	debugLog.Printf("New() done: ActiveSessionID=%q, %d projects, %d sidebar items",
		m.appState.ActiveSessionID, len(m.appState.Projects), len(m.sidebar.Items))
	return m
}

// restoreGrid re-opens the grid view if RestoreGridMode is set, then clears the flag.
//
// SyncState/SetPaneTitles/SetContents run unconditionally — even when ViewGrid
// is already on the stack (the #111 tmux-detach path). This is intentional:
// after an attach the active session, pane titles, and preview snapshots may
// have changed, so the grid must be refreshed to reflect post-attach state.
// The side effect is that any mid-grid cursor position the user had before
// attaching is reset to the active-session cell — acceptable trade-off because
// the user's focus on return is the session they just attached to.
//
// Only the PushView is guarded by !HasView(ViewGrid) so the grid is not pushed
// twice on detach-restore (would break the stack invariant).
func (m *Model) restoreGrid() {
	if m.appState.RestoreGridMode == state.GridRestoreNone {
		return
	}
	mode := m.appState.RestoreGridMode
	m.appState.RestoreGridMode = state.GridRestoreNone
	sessions := m.gridSessions(mode)
	m.gridView.SyncState(sessions, mode, m.gridProjectNames(), m.gridProjectColors(), m.gridSessionColors(), m.appState.ActiveSessionID)
	m.gridView.SetPaneTitles(m.paneTitles)
	m.gridView.SetContents(m.gridContentsFromSnapshots(sessions))
	if !m.HasView(ViewGrid) {
		m.PushView(ViewGrid)
	}
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
	if len(m.bellPending) > 0 {
		cmds = append(cmds, m.ensureBellBlinkRunning())
	}
	if m.HasView(ViewGrid) {
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
	case escape.TitlesDetectedMsg:
		return m.handleTitlesDetected(msg)
	case escape.StatusesDetectedMsg:
		return m.handleStatusesDetected(msg)
	case SessionCreatedMsg:
		return m.handleSessionCreated(msg)
	case SessionKilledMsg:
		return m.handleSessionKilled(msg)
	case SessionTitleChangedMsg:
		return m.handleSessionTitleChanged(msg)
	case ProjectNameChangedMsg:
		m.appState = *state.UpdateProjectName(&m.appState, msg.ProjectID, msg.Name)
		m.commitState()
		return m, nil
	case TeamNameChangedMsg:
		m.appState = *state.UpdateTeamName(&m.appState, msg.TeamID, msg.Name)
		m.commitState()
		return m, nil
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
	case components.SettingsSaveConfirmMsg:
		return m.handleSettingsSaveConfirm(msg)
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
	case activityPipTickMsg:
		return m.handleActivityPipTick()
	case bellBlinkMsg:
		m.bellBlinkOn = !m.bellBlinkOn
		m.sidebar.SetBellBlink(m.bellBlinkOn)
		m.gridView.SetBellBlink(m.bellBlinkOn)
		if len(m.bellPending) == 0 {
			m.bellBlinkRunning = false
			return m, nil
		}
		return m, m.scheduleBellBlink()
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}

	// Forward non-key, non-handled messages to active modals that need
	// internal ticks (e.g. cursor blink, filter debounce in charmbracelet/list).
	if m.HasView(ViewDirPicker) {
		m.dirPicker.Update(msg)
	}

	return m, nil
}

// View renders the full UI based on the top of the view stack.
func (m Model) View() string {
	switch m.TopView() {
	case ViewSettings:
		m.settings.Width = m.appState.TermWidth
		m.settings.Height = m.appState.TermHeight
		return m.settings.View()
	case ViewGrid:
		m.gridView.Width = m.appState.TermWidth
		gridH := m.appState.TermHeight - components.PreviewActivityPanelHeight
		if gridH < 1 {
			gridH = 1
		}
		m.gridView.Height = gridH
		m.gridHelpModel.Width = m.appState.TermWidth
		gridHints := m.gridHelpModel.View(NewGridKeyMap(m.keys))
		gridBody := m.gridView.View(gridHints)
		flashDur := ActivityPipFlashNormal
		var focusedID string
		if m.gridView.InputMode() {
			flashDur = ActivityPipFlashInput
			if sel := m.gridView.Selected(); sel != nil {
				focusedID = sel.ID
			}
		}
		gridActivity := components.PreviewActivityPanel{
			Width:         m.appState.TermWidth,
			Sessions:      m.gridSessions(m.gridView.Mode),
			LastChange:    m.lastPreviewChange,
			FlashDuration: flashDur,
			FocusedID:     focusedID,
			PipFrame:      m.pipFrame,
		}.View()
		return lipgloss.JoinVertical(lipgloss.Left, gridBody, gridActivity)
	case ViewHelp:
		return m.helpView()
	case ViewGridInputHint:
		return m.overlayView(m.gridInputHintView())
	case ViewAttachHint:
		return m.overlayView(m.attachHintView())
	case ViewConfirm:
		return m.overlayView(m.confirm.View())
	case ViewRecovery:
		m.recoveryPicker.Width = m.appState.TermWidth
		m.recoveryPicker.Height = m.appState.TermHeight
		return m.overlayView(m.recoveryPicker.View())
	case ViewOrphan:
		m.orphanPicker.Width = m.appState.TermWidth
		m.orphanPicker.Height = m.appState.TermHeight
		return m.overlayView(m.orphanPicker.View())
	case ViewAgentPicker:
		return m.overlayView(m.agentPicker.View())
	case ViewTeamBuilder:
		return m.overlayView(m.teamBuilder.View())
	case ViewProjectName:
		return m.overlayView(m.nameInputView("New Project (1/2)", "Project name:", "enter: next  esc: cancel"))
	case ViewDirPicker:
		return m.overlayView(m.dirPicker.View())
	case ViewDirConfirm:
		return m.overlayView(m.dirConfirmView())
	case ViewCustomCmd:
		return m.overlayView(m.nameInputView("Custom Command", "Command to run:", "enter: create  esc: cancel"))
	case ViewWorktreeBranch:
		return m.overlayView(m.nameInputView("New Worktree Session", "Branch name:", "enter: create  esc: cancel"))
	case ViewRename:
		return m.overlayView(m.renameDialogView())
	case ViewWhatsNew:
		return m.whatsNewView()
	case ViewFilter:
		// Filter is an inline mode — fall through to main layout rendering.
	}

	sw, pw, ch := computeLayout(m.appState.TermWidth, m.appState.TermHeight, components.PreviewActivityPanelHeight)
	m.sidebar.Width = sw
	m.sidebar.Height = ch
	m.preview.Resize(pw, ch)
	m.statusBar.Width = m.appState.TermWidth

	sidebarView := m.sidebar.View(m.appState.ActiveSessionID, m.appState.FocusedPane == state.PaneSidebar)
	previewView := m.preview.View(m.appState.ActiveSessionID)
	statusView := m.statusBar.View(&m.appState, m.buildStatusHints())
	activityView := m.activityPanelView()

	sidebarLines := strings.Count(sidebarView, "\n") + 1
	previewLines := strings.Count(previewView, "\n") + 1
	statusLines := strings.Count(statusView, "\n") + 1

	main := lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, previewView)
	mainLines := strings.Count(main, "\n") + 1
	out := lipgloss.JoinVertical(lipgloss.Left, main, activityView, statusView)
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
	sw, pw, ch := computeLayout(m.appState.TermWidth, m.appState.TermHeight, components.PreviewActivityPanelHeight)
	m.sidebar.Width = sw
	m.sidebar.Height = ch
	m.preview.Resize(pw, ch)
	m.statusBar.Width = m.appState.TermWidth
}

// activityPanelView renders the preview-activity panel (one row of pip+title
// per session, packed horizontally). Rendered in both main and grid views.
func (m *Model) activityPanelView() string {
	return components.PreviewActivityPanel{
		Width:         m.appState.TermWidth,
		Sessions:      state.AllSessions(&m.appState),
		LastChange:    m.lastPreviewChange,
		FlashDuration: 150 * time.Millisecond,
	}.View()
}

// buildStatusHints returns the pre-computed hint line for the statusbar.
// Special-case states (filter) return custom strings; otherwise the
// hints are rendered via bubbles/help from the key map, with the status
// legend appended at the end.
func (m *Model) buildStatusHints() string {
	legend := "  " + styles.MutedStyle.Render(styles.StatusLegend())
	if m.appState.FilterActive {
		return fmt.Sprintf("Filter: %s  [esc: clear]", m.appState.FilterQuery+"_") + legend
	}
	if m.appState.FocusedPane == state.PanePreview {
		return "?:help  tab:sidebar  a:attach  ctrl+r:refresh  q:quit" + legend
	}
	m.helpModel.Width = m.statusBar.Width - 3
	return m.helpModel.View(m.keys) + legend
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
			m.focusSession("")
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

// buildDetectionCtxs compiles status detection regexes from config once at startup.
// Returns a map keyed by agent type name (e.g. "claude").
func buildDetectionCtxs(agents map[string]config.AgentProfile) map[string]escape.SessionDetectionCtx {
	ctxs := make(map[string]escape.SessionDetectionCtx, len(agents))
	for name, profile := range agents {
		ctx := escape.SessionDetectionCtx{
			StableTicks: profile.Status.StableTicks,
		}
		if profile.Status.WaitTitle != "" {
			if re, err := regexp.Compile(profile.Status.WaitTitle); err == nil {
				ctx.WaitTitleRe = re
			} else {
				debugLog.Printf("bad wait_title regex for %s: %v", name, err)
			}
		}
		if profile.Status.RunTitle != "" {
			if re, err := regexp.Compile(profile.Status.RunTitle); err == nil {
				ctx.RunTitleRe = re
			} else {
				debugLog.Printf("bad run_title regex for %s: %v", name, err)
			}
		}
		if profile.Status.WaitPrompt != "" {
			if re, err := regexp.Compile(profile.Status.WaitPrompt); err == nil {
				ctx.WaitPromptRe = re
			} else {
				debugLog.Printf("bad wait_prompt regex for %s: %v", name, err)
			}
		}
		if profile.Status.IdlePrompt != "" {
			if re, err := regexp.Compile(profile.Status.IdlePrompt); err == nil {
				ctx.IdlePromptRe = re
			} else {
				debugLog.Printf("bad idle_prompt regex for %s: %v", name, err)
			}
		}
		ctxs[name] = ctx
	}
	return ctxs
}
