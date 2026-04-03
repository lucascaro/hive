package tui

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/escape"
	"github.com/lucascaro/hive/internal/git"
	"github.com/lucascaro/hive/internal/hooks"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/components"
	"github.com/lucascaro/hive/internal/tui/styles"
)

var debugLog *log.Logger

func init() {
	f, err := os.OpenFile(config.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		debugLog = log.New(os.Stderr, "[hive] ", log.Ltime)
		return
	}
	debugLog = log.New(f, "[hive] ", log.Ltime|log.Lmicroseconds)
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
}

// LastAttach returns the pending attach request after the TUI exits, or nil.
func (m Model) LastAttach() *SessionAttachMsg { return m.attachPending }

// New creates the root model.
func New(cfg config.Config, appState state.AppState) Model {
	km := NewKeyMap(cfg.Keybindings)
	ni := textinput.New()
	ni.CharLimit = 256
	ni.Width = 48
	ni.Placeholder = "Project name"

	m := Model{
		cfg:              cfg,
		appState:         appState,
		keys:             km,
		titleEditor:      components.NewTitleEditor(),
		agentPicker:      components.NewAgentPicker(),
		teamBuilder:      components.NewTeamBuilder(),
		settings:         components.NewSettingsView(),
		orphanPicker:     components.NewOrphanPicker(appState.OrphanSessions),
		dirPicker:        components.NewDirPicker(),
		recoveryPicker:   components.NewRecoveryPicker(appState.RecoverableSessions),
		nameInput:        ni,
		contentSnapshots: make(map[string]string),
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
	if m.appState.RestoreGridMode != state.GridRestoreNone {
		mode := m.appState.RestoreGridMode
		m.appState.RestoreGridMode = state.GridRestoreNone
		sessions := m.gridSessions(mode)
		m.gridView.Show(sessions, mode)
		m.gridView.SetProjectNames(m.gridProjectNames())
		m.gridView.SetContents(m.gridContentsFromSnapshots(sessions))
		m.gridView.SyncCursor(m.appState.ActiveSessionID)
	}
	debugLog.Printf("New() done: ActiveSessionID=%q, %d projects, %d sidebar items",
		m.appState.ActiveSessionID, len(m.appState.Projects), len(m.sidebar.Items))
	return m
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
	}
	if m.gridView.Active {
		cmds = append(cmds, m.scheduleGridPoll())
	}
	return tea.Batch(cmds...)
}

// Update handles all messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// While the directory picker is active, let it handle non-resize messages
	// before the main update switch. Window size messages still need to be
	// processed here so the app's dimensions and layout stay in sync.
	// Only short-circuit when the picker actually consumes the message so that
	// background messages (preview polls, title/status watchers) continue to run.
	if m.dirPicker.Active {
		if _, ok := msg.(tea.WindowSizeMsg); !ok {
			cmd, consumed := m.dirPicker.Update(msg)
			if consumed {
				return m, cmd
			}
		}
	}

	switch msg := msg.(type) {
	// --- Window resize ---
	case tea.WindowSizeMsg:
		m.appState.TermWidth = msg.Width
		m.appState.TermHeight = msg.Height
		m.dirPicker.SetHeight(msg.Height)
		m.recomputeLayout()
		return m, nil

	// --- Preview refresh ---
	case components.PreviewUpdatedMsg:
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

	// --- Window gone (agent exited, tmux window auto-closed) ---
	case components.SessionWindowGoneMsg:
		debugLog.Printf("session window gone: %s", msg.SessionID)
		m.appState = *state.RemoveSession(&m.appState, msg.SessionID)
		m.sidebar.Rebuild(&m.appState)
		m.persist()
		// Switch to whichever session the sidebar now has selected.
		m.syncActiveFromSidebar()
		m.previewPollGen++ // invalidate any in-flight polls for the removed session
		return m, m.schedulePollPreview()

	// --- Title watcher ---
	case escape.TitleDetectedMsg:
		sess := m.appState.ActiveSession()
		if sess != nil && msg.SessionID == sess.ID {
			if sess.TitleSource != state.TitleSourceUser || m.cfg.AgentTitleOverridesUserTitle {
				m.appState = *state.UpdateSessionTitle(&m.appState, msg.SessionID, msg.Title, state.TitleSourceAgent)
				m.sidebar.Rebuild(&m.appState)
				m.persist()
			}
		}
		return m, m.scheduleWatchTitles()

	// --- Status watcher ---
	case escape.StatusesDetectedMsg:
		// Always update content snapshots so the next diff is accurate.
		for sessionID, content := range msg.Contents {
			m.contentSnapshots[sessionID] = content
		}
		// If the status watcher captured new content for the active session, update
		// the preview immediately rather than waiting for the next PollPreview tick.
		if content, ok := msg.Contents[m.appState.ActiveSessionID]; ok {
			m.appState.PreviewContent = content
			m.preview.SetContent(content)
		}
		changed := false
		for sessionID, status := range msg.Statuses {
			sess := state.FindSession(&m.appState, sessionID)
			if sess != nil && sess.Status != status {
				m.appState = *state.UpdateSessionStatus(&m.appState, sessionID, status)
				changed = true
			}
		}
		if changed {
			m.sidebar.Rebuild(&m.appState)
		}
		return m, m.scheduleWatchStatuses()

	// --- Session lifecycle ---
	case SessionCreatedMsg:
		m.appState = *state.UpdateSessionStatus(&m.appState, msg.Session.ID, state.StatusRunning)
		m.appState.ActiveSessionID = msg.Session.ID
		m.appState.PreviewContent = ""
		m.preview.SetContent("")
		m.sidebar.Rebuild(&m.appState)
		m.sidebar.SyncActiveSession(msg.Session.ID)
		m.persist()
		m.previewPollGen++ // new session, start fresh poll chain
		return m, m.schedulePollPreview()

	case SessionKilledMsg:
		m.appState = *state.RemoveSession(&m.appState, msg.SessionID)
		m.sidebar.Rebuild(&m.appState)
		m.persist()
		return m, nil

	case SessionTitleChangedMsg:
		m.appState = *state.UpdateSessionTitle(&m.appState, msg.SessionID, msg.Title, msg.Source)
		// Rename tmux window too
		sess := m.appState.ActiveSession()
		if sess != nil {
			target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
			_ = mux.RenameWindow(target, msg.Title)
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
		m.sidebar.Rebuild(&m.appState)
		m.persist()
		return m, nil

	case SessionAttachMsg:
		cmd := m.doAttach(msg)
		return m, cmd

	case AttachDoneMsg:
		if msg.Err != nil {
			return m, func() tea.Msg { return ErrorMsg{Err: msg.Err} }
		}
		m.appState.RestoreGridMode = msg.RestoreGridMode
		m.previewPollGen++
		return m, m.schedulePollPreview()

	case SessionDetachedMsg:
		m.previewPollGen++ // returning from native attach; start fresh poll chain
		return m, m.schedulePollPreview()

	// --- Team lifecycle ---
	case TeamCreatedMsg:
		m.appState.ActiveTeamID = msg.Team.ID
		m.sidebar.Rebuild(&m.appState)
		m.persist()
		return m, nil

	case TeamKilledMsg:
		m.appState = *state.RemoveTeam(&m.appState, msg.TeamID)
		m.sidebar.Rebuild(&m.appState)
		m.persist()
		return m, nil

	// --- Project lifecycle ---
	case ProjectCreatedMsg:
		m.appState.ActiveProjectID = msg.Project.ID
		m.sidebar.Rebuild(&m.appState)
		m.persist()
		return m, nil

	case ProjectKilledMsg:
		m.appState = *state.RemoveProject(&m.appState, msg.ProjectID)
		m.sidebar.Rebuild(&m.appState)
		m.persist()
		return m, nil

	// --- Directory picker result ---
	case components.DirPickedMsg:
		m.dirPicker.Active = false
		dir := msg.Dir
		if _, err := os.Stat(dir); err != nil {
			if os.IsNotExist(err) {
				// Directory doesn't exist — ask for confirmation before creating.
				m.nameInput.SetValue(dir)
				m.inputMode = "project-dir-confirm"
				return m, nil
			}
			// Unexpected error (e.g. permission denied) — surface it and abort.
			m.inputMode = ""
			return m, func() tea.Msg {
				return ErrorMsg{Err: fmt.Errorf("check directory: %w", err)}
			}
		}
		name := m.pendingProjectName
		m.pendingProjectName = ""
		m.inputMode = ""
		return m, m.createProject(name, dir)

	case components.DirPickerCancelMsg:
		m.dirPicker.Active = false
		// Return to the project name step.
		m.inputMode = "project-name"
		m.nameInput.Reset()
		m.nameInput.SetValue(m.pendingProjectName)
		return m, m.nameInput.Focus()

	// --- Confirmation ---
	case ConfirmActionMsg:
		m.appState.ShowConfirm = true
		m.appState.ConfirmMsg = msg.Message
		m.appState.ConfirmAction = msg.Action
		m.confirm.Message = msg.Message
		m.confirm.Action = msg.Action
		return m, nil

	case ConfirmedMsg:
		m.appState.ShowConfirm = false
		m.confirm.Message = ""
		return m, m.handleConfirmedAction(msg.Action)

	case components.CancelledMsg:
		m.appState.ShowConfirm = false
		m.appState.EditingTitle = false
		m.titleEditor.Stop()
		m.agentPicker.Hide()
		m.teamBuilder.Hide()
		m.nameInput.Blur()
		m.inputMode = ""
		m.pendingWorktree = false
		m.pendingWorktreeAgentType = ""
		m.pendingWorktreeAgentCmd = nil
		return m, nil

	// --- Grid view ---
	case components.GridPreviewsUpdatedMsg:
		m.gridView.SetContents(msg.Contents)
		if m.gridView.Active {
			return m, m.scheduleGridPoll()
		}
		return m, nil

	case components.GridSessionSelectedMsg:
		var sessionTitle string
		var agentType state.AgentType
		var projectName string
		if s := m.sessionByTmux(msg.TmuxSession, msg.TmuxWindow); s != nil {
			sessionTitle = s.Title
			agentType = s.AgentType
			projectName = m.projectNameByID(s.ProjectID)
		}
		attach := &SessionAttachMsg{
			TmuxSession:     msg.TmuxSession,
			TmuxWindow:      msg.TmuxWindow,
			RestoreGridMode: m.gridView.Mode,
			SessionTitle:    sessionTitle,
			AgentType:       agentType,
			ProjectName:     projectName,
		}
		if !m.cfg.HideAttachHint {
			m.pendingAttach = attach
			m.showAttachHint = true
			return m, nil
		}
		cmd := m.doAttach(*attach)
		return m, cmd

	// --- Agent picker result ---
	case components.AgentPickedMsg:
		// Team builder owns agent selection while it is active.
		if m.teamBuilder.Active {
			cmd := m.teamBuilder.Update(msg)
			return m, cmd
		}
		if m.inputMode == "new-session" {
			agentTypeStr := string(msg.AgentType)
			profile := m.cfg.Agents[agentTypeStr]
			agentBin := agentTypeStr
			if len(profile.Cmd) > 0 {
				agentBin = profile.Cmd[0]
			}
			if _, err := exec.LookPath(agentBin); err != nil {
				// Binary not found — prompt to install.
				m.pendingAgentType = agentTypeStr
				installInfo := ""
				if len(profile.InstallCmd) > 0 {
					installInfo = "\n\nInstall with: " + strings.Join(profile.InstallCmd, " ")
				}
				return m, func() tea.Msg {
					return ConfirmActionMsg{
						Message: fmt.Sprintf("%q not found in PATH.%s\n\nInstall now?", agentBin, installInfo),
						Action:  "install-agent:" + agentTypeStr,
					}
				}
			}
			if m.pendingWorktree {
				// Worktree flow: collect branch name next.
				m.pendingWorktreeAgentType = agentTypeStr
				m.pendingWorktreeAgentCmd = profile.Cmd
				m.inputMode = "worktree-branch"
				m.nameInput.Placeholder = "branch-name"
				m.nameInput.Reset()
				m.nameInput.SetValue(git.RandomBranchName())
				blinkCmd := m.nameInput.Focus()
				return m, blinkCmd
			}
			cmd := m.createSession(m.pendingProjectID, agentTypeStr, profile.Cmd)
			return m, cmd
		}
		return m, nil

	// --- Agent installation result ---
	case AgentInstalledMsg:
		m.appState.LastError = ""
		m.appState.InstallingAgent = ""
		cmd := m.createSession(m.pendingProjectID, msg.AgentType, m.cfg.Agents[msg.AgentType].Cmd)
		m.pendingAgentType = ""
		return m, cmd

	// --- Orphan recovery result ---
	case components.RecoveryPickerDoneMsg:
		if len(msg.Selected) > 0 {
			m.recoverSessions(msg.Selected)
		}
		return m, nil

	// --- Orphan cleanup result ---
	case components.OrphanPickerDoneMsg:
		for _, name := range msg.Selected {
			_ = mux.KillSession(name)
		}
		return m, nil

	// --- Team builder result ---
	case components.TeamBuiltMsg:
		cmd := m.createTeam(msg.Spec)
		return m, cmd

	// --- Errors ---
	case ErrorMsg:
		m.appState.LastError = msg.Err.Error()
		return m, nil

	// --- Persistence ---
	case PersistMsg:
		m.persist()
		return m, nil

	// --- Quit and kill ---
	case QuitAndKillMsg:
		m.killAllSessions()
		return m, tea.Quit

	// --- Settings ---
	case components.SettingsSaveRequestMsg:
		newCfg := msg.Config
		if err := config.Save(newCfg); err != nil {
			return m, func() tea.Msg {
				return ErrorMsg{Err: fmt.Errorf("save settings: %w", err)}
			}
		}
		m.cfg = newCfg
		m.keys = NewKeyMap(newCfg.Keybindings)
		return m, func() tea.Msg { return ConfigSavedMsg{Config: newCfg} }

	case components.SettingsClosedMsg:
		m.settings.Close()

	case ConfigSavedMsg:
		m.appState.LastError = "" // clear any previous error

	// --- Key events ---
	case tea.KeyMsg:
		return m.handleKey(msg)

	// --- Mouse events ---
	case tea.MouseMsg:
		return m.handleMouse(msg)
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

	// Log a WARNING when the total frame height doesn't match the terminal height,
	// since this causes terminal scroll and visual corruption.
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

	// Normalize the frame to exactly TermHeight lines so Bubble Tea's cursor
	// bookkeeping stays accurate and ghost lines cannot accumulate.
	// JoinVertical can append a trailing newline; trim it before counting.
	out = strings.TrimRight(out, "\n")
	outLines = strings.Count(out, "\n") + 1
	switch {
	case outLines < m.appState.TermHeight:
		out += strings.Repeat("\n", m.appState.TermHeight-outLines)
	case outLines > m.appState.TermHeight:
		// Truncate at the Nth newline.
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

// --- Key handler ---

// handleKey handles keyboard events.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Settings screen consumes all keys while active.
	if m.settings.Active {
		cmd, _ := m.settings.Update(msg)
		return m, cmd
	}
	// Grid overview consumes all keys while active.
	if m.gridView.Active {
		m.gridView.Width = m.appState.TermWidth
		m.gridView.Height = m.appState.TermHeight
		switch msg.String() {
		case "G":
			// Switch to all-projects view without closing.
			prevID := ""
			if s := m.gridView.Selected(); s != nil {
				prevID = s.ID
			}
			m.gridView.Show(m.gridSessions(state.GridRestoreAll), state.GridRestoreAll)
			m.gridView.SetProjectNames(m.gridProjectNames())
			m.gridView.SyncCursor(prevID)
			return m, m.scheduleGridPoll()
		case "x":
			if sess := m.gridView.Selected(); sess != nil {
				m.gridView.Hide()
				s := sess
				return m, func() tea.Msg {
					return ConfirmActionMsg{
						Message: fmt.Sprintf("Kill session %q?", s.Title),
						Action:  "kill-session:" + s.ID,
					}
				}
			}
		case "r":
			if sess := m.gridView.Selected(); sess != nil {
				m.gridView.Hide()
				m.sidebar.SyncActiveSession(sess.ID)
				m.appState.ActiveSessionID = sess.ID
				return m, m.startRename()
			}
		}
		cmd, _ := m.gridView.Update(msg)
		return m, cmd
	}
	// Route to active sub-component first.
	if m.appState.EditingTitle {
		return m.handleTitleEdit(msg)
	}
	if m.recoveryPicker.Active {
		updated, cmd := m.recoveryPicker.Update(msg)
		m.recoveryPicker = updated
		return m, cmd
	}
	if m.orphanPicker.Active {
		updated, cmd := m.orphanPicker.Update(msg)
		m.orphanPicker = updated
		return m, cmd
	}
	if m.agentPicker.Active {
		cmd, _ := m.agentPicker.Update(msg)
		return m, cmd
	}
	if m.teamBuilder.Active {
		cmd := m.teamBuilder.Update(msg)
		return m, cmd
	}
	if m.inputMode == "project-name" {
		return m.handleNameInput(msg)
	}
	if m.inputMode == "project-dir-confirm" {
		return m.handleDirConfirm(msg)
	}
	if m.inputMode == "worktree-branch" {
		return m.handleWorktreeBranchInput(msg)
	}
	if m.appState.FilterActive {
		return m.handleFilter(msg)
	}
	if m.showAttachHint {
		return m.handleAttachHint(msg)
	}
	if m.appState.ShowConfirm {
		return m.handleConfirm(msg)
	}

	// Global keys
	switch {
	case msg.String() == "ctrl+c":
		return m, tea.Quit

	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.QuitKill):
		return m, func() tea.Msg {
			return ConfirmActionMsg{
				Message: "Quit and kill ALL sessions?",
				Action:  "quit-kill",
			}
		}

	case key.Matches(msg, m.keys.Help):
		m.appState.ShowHelp = !m.appState.ShowHelp
		m.appState.ShowTmuxHelp = false
		return m, nil

	case key.Matches(msg, m.keys.Settings):
		m.settings.Width = m.appState.TermWidth
		m.settings.Height = m.appState.TermHeight
		m.settings.Open(m.cfg)
		return m, nil

	case key.Matches(msg, m.keys.TmuxHelp):
		m.appState.ShowTmuxHelp = !m.appState.ShowTmuxHelp
		m.appState.ShowHelp = false
		return m, nil

	case msg.String() == "esc" && (m.appState.ShowHelp || m.appState.ShowTmuxHelp):
		m.appState.ShowHelp = false
		m.appState.ShowTmuxHelp = false
		return m, nil

	case key.Matches(msg, m.keys.FocusToggle):
		if m.appState.FocusedPane == state.PaneSidebar {
			m.appState.FocusedPane = state.PanePreview
		} else {
			m.appState.FocusedPane = state.PaneSidebar
		}
		return m, nil

	case key.Matches(msg, m.keys.Filter):
		m.appState.FilterActive = true
		m.appState.FilterQuery = ""
		return m, nil

	case key.Matches(msg, m.keys.GridOverview):
		// g shows only current project; G (handled above in the active-grid block,
		// or via msg.String()=="G" below) shows all.
		sessions := m.gridSessions(state.GridRestoreProject)
		m.gridView.Show(sessions, state.GridRestoreProject)
		m.gridView.SetProjectNames(m.gridProjectNames())
		m.gridView.SyncCursor(m.appState.ActiveSessionID)
		return m, m.scheduleGridPoll()

	case msg.String() == "G":
		m.gridView.Show(m.gridSessions(state.GridRestoreAll), state.GridRestoreAll)
		m.gridView.SetProjectNames(m.gridProjectNames())
		m.gridView.SyncCursor(m.appState.ActiveSessionID)
		return m, m.scheduleGridPoll()

	case key.Matches(msg, m.keys.NewProject):
		m.inputMode = "project-name"
		m.nameInput.Placeholder = "my-project"
		m.nameInput.Reset()
		blinkCmd := m.nameInput.Focus()
		return m, blinkCmd

	case key.Matches(msg, m.keys.NewSession):
		sel := m.sidebar.Selected()
		if sel == nil {
			return m, nil
		}
		pid := sel.ProjectID
		if pid == "" {
			return m, nil
		}
		m.pendingProjectID = pid
		m.pendingWorktree = false
		m.inputMode = "new-session"
		m.agentPicker.Show(m.sortedAgentItems())
		return m, nil

	case key.Matches(msg, m.keys.NewWorktreeSession):
		sel := m.sidebar.Selected()
		if sel == nil {
			return m, nil
		}
		pid := sel.ProjectID
		if pid == "" {
			return m, nil
		}
		// Verify this project is a git repo before proceeding.
		projDir := ""
		for _, p := range m.appState.Projects {
			if p.ID == pid {
				projDir = p.Directory
				break
			}
		}
		if projDir == "" {
			projDir, _ = os.Getwd()
		}
		if !git.IsGitRepo(projDir) {
			return m, func() tea.Msg {
				return ErrorMsg{Err: fmt.Errorf("project directory is not a git repository")}
			}
		}
		m.pendingProjectID = pid
		m.pendingWorktree = true
		m.inputMode = "new-session"
		m.agentPicker.Show(m.sortedAgentItems())
		return m, nil

	case key.Matches(msg, m.keys.NewTeam):
		sel := m.sidebar.Selected()
		if sel == nil {
			return m, nil
		}
		workDir := ""
		for _, p := range m.appState.Projects {
			if p.ID == sel.ProjectID {
				workDir = p.Directory
				break
			}
		}
		if workDir == "" {
			workDir, _ = os.Getwd()
		}
		m.teamBuilder.Start(workDir)
		m.pendingProjectID = sel.ProjectID
		return m, nil

	case key.Matches(msg, m.keys.Attach):
		if !m.cfg.HideAttachHint {
			attach := m.pendingAttachDetails()
			if attach != nil {
				m.pendingAttach = attach
				m.showAttachHint = true
				return m, nil
			}
		}
		return m, m.attachActiveSession()

	case key.Matches(msg, m.keys.Rename):
		return m, m.startRename()

	case key.Matches(msg, m.keys.KillSession):
		sel := m.sidebar.Selected()
		if sel != nil && sel.Kind == components.KindProject {
			return m, func() tea.Msg {
				return ConfirmActionMsg{
					Message: fmt.Sprintf("Kill project %q and all its sessions?", sel.Label),
					Action:  "kill-project:" + sel.ProjectID,
				}
			}
		}
		if sel != nil && sel.SessionID != "" {
			return m, func() tea.Msg {
				return ConfirmActionMsg{
					Message: fmt.Sprintf("Kill session %q?", sel.Label),
					Action:  "kill-session:" + sel.SessionID,
				}
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.KillTeam):
		sel := m.sidebar.Selected()
		if sel != nil && sel.TeamID != "" {
			teamName := m.teamNameByID(sel.TeamID)
			return m, func() tea.Msg {
				return ConfirmActionMsg{
					Message: fmt.Sprintf("Kill team %q and all its sessions?", teamName),
					Action:  "kill-team:" + sel.TeamID,
				}
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.ToggleCollapse):
		sel := m.sidebar.Selected()
		if sel != nil {
			if sel.Kind == components.KindProject {
				m.appState = *state.ToggleProjectCollapsed(&m.appState, sel.ProjectID)
			} else if sel.Kind == components.KindTeam {
				m.appState = *state.ToggleTeamCollapsed(&m.appState, sel.TeamID)
			}
			m.sidebar.Rebuild(&m.appState)
		}
		return m, nil

	case key.Matches(msg, m.keys.CollapseItem):
		sel := m.sidebar.Selected()
		if sel == nil {
			return m, nil
		}
		if sel.Kind == components.KindSession {
			// Left on a session collapses the immediate parent (team or project).
			if sel.TeamID != "" {
				m.appState = *state.ToggleTeamCollapsed(&m.appState, sel.TeamID)
			} else {
				m.appState = *state.ToggleProjectCollapsed(&m.appState, sel.ProjectID)
			}
			m.sidebar.Rebuild(&m.appState)
			// Move cursor to the parent item.
			for i, item := range m.sidebar.Items {
				if sel.TeamID != "" && item.Kind == components.KindTeam && item.TeamID == sel.TeamID {
					m.sidebar.Cursor = i
					break
				} else if sel.TeamID == "" && item.Kind == components.KindProject && item.ProjectID == sel.ProjectID {
					m.sidebar.Cursor = i
					break
				}
			}
		} else if !sel.Collapsed {
			if sel.Kind == components.KindProject {
				m.appState = *state.ToggleProjectCollapsed(&m.appState, sel.ProjectID)
				m.sidebar.Rebuild(&m.appState)
			} else if sel.Kind == components.KindTeam {
				m.appState = *state.ToggleTeamCollapsed(&m.appState, sel.TeamID)
				m.sidebar.Rebuild(&m.appState)
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.ExpandItem):
		sel := m.sidebar.Selected()
		if sel != nil && sel.Collapsed {
			if sel.Kind == components.KindProject {
				m.appState = *state.ToggleProjectCollapsed(&m.appState, sel.ProjectID)
				m.sidebar.Rebuild(&m.appState)
			} else if sel.Kind == components.KindTeam {
				m.appState = *state.ToggleTeamCollapsed(&m.appState, sel.TeamID)
				m.sidebar.Rebuild(&m.appState)
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.NavUp):
		prev := m.sidebar.Cursor
		prevSession := m.appState.ActiveSessionID
		m.sidebar.MoveUp()
		debugLog.Printf("NavUp: cursor %d->%d activeSession=%s", prev, m.sidebar.Cursor, m.appState.ActiveSessionID)
		if m.sidebar.Cursor != prev {
			m.syncActiveFromSidebar()
			if m.appState.ActiveSessionID != prevSession {
				m.previewPollGen++ // switched to a different session, start fresh poll
				return m, m.schedulePollPreview()
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.NavDown):
		prev := m.sidebar.Cursor
		prevSession := m.appState.ActiveSessionID
		m.sidebar.MoveDown()
		debugLog.Printf("NavDown: cursor %d->%d activeSession=%s", prev, m.sidebar.Cursor, m.appState.ActiveSessionID)
		if m.sidebar.Cursor != prev {
			m.syncActiveFromSidebar()
			if m.appState.ActiveSessionID != prevSession {
				m.previewPollGen++ // switched to a different session, start fresh poll
				return m, m.schedulePollPreview()
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.NavProjectUp):
		prev := m.sidebar.Cursor
		prevSession := m.appState.ActiveSessionID
		m.sidebar.JumpPrevProject()
		debugLog.Printf("NavProjectUp: cursor %d->%d", prev, m.sidebar.Cursor)
		if m.sidebar.Cursor != prev {
			m.syncActiveFromSidebar()
			if m.appState.ActiveSessionID != prevSession {
				m.previewPollGen++
				return m, m.schedulePollPreview()
			}
		}
		return m, nil

	case key.Matches(msg, m.keys.NavProjectDown):
		prev := m.sidebar.Cursor
		prevSession := m.appState.ActiveSessionID
		m.sidebar.JumpNextProject()
		debugLog.Printf("NavProjectDown: cursor %d->%d", prev, m.sidebar.Cursor)
		if m.sidebar.Cursor != prev {
			m.syncActiveFromSidebar()
			if m.appState.ActiveSessionID != prevSession {
				m.previewPollGen++
				return m, m.schedulePollPreview()
			}
		}
		return m, nil

	// Jump to project by number
	case msg.String() >= "1" && msg.String() <= "9":
		idx := int(msg.String()[0]-'0') - 1
		count := 0
		for i, item := range m.sidebar.Items {
			if item.Kind == components.KindProject {
				if count == idx {
					m.sidebar.Cursor = i
					m.syncActiveFromSidebar()
					break
				}
				count++
			}
		}
		return m, nil
	}
	return m, nil
}

// --- Title editing ---

func (m *Model) startRename() tea.Cmd {
	sel := m.sidebar.Selected()
	if sel == nil {
		return nil
	}
	current := sel.Label
	m.appState.EditingTitle = true
	m.titleEditor.Start(sel.SessionID, sel.TeamID, current)
	return m.titleEditor.Update(nil)
}

func (m Model) handleTitleEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		newTitle := m.titleEditor.Value()
		sessionID := m.titleEditor.SessionID
		m.titleEditor.Stop()
		m.appState.EditingTitle = false
		if newTitle != "" && sessionID != "" {
			return m, func() tea.Msg {
				return SessionTitleChangedMsg{
					SessionID: sessionID,
					Title:     newTitle,
					Source:    state.TitleSourceUser,
				}
			}
		}
		return m, nil
	case "esc":
		m.titleEditor.Stop()
		m.appState.EditingTitle = false
		return m, nil
	}
	cmd := m.titleEditor.Update(msg)
	return m, cmd
}

// --- Filter ---

// --- Mouse handler ---

// handleMouse routes mouse press and scroll-wheel events to the appropriate
// component. Motion and release events are silently ignored.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Only act on press events and wheel scrolls.
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}

	// Settings screen: ignore mouse (keyboard-only).
	if m.settings.Active {
		return m, nil
	}

	// Grid view: cell selection and attach.
	if m.gridView.Active {
		m.gridView.Width = m.appState.TermWidth
		m.gridView.Height = m.appState.TermHeight
		switch msg.Button {
		case tea.MouseButtonLeft:
			if idx, ok := m.gridView.CellAt(msg.X, msg.Y); ok {
				m.gridView.Cursor = idx
				// Clicking a grid cell activates (attaches) that session.
				if sess := m.gridView.Selected(); sess != nil {
					m.gridView.Hide()
					s := sess
					return m, func() tea.Msg {
						return components.GridSessionSelectedMsg{
							TmuxSession: s.TmuxSession,
							TmuxWindow:  s.TmuxWindow,
						}
					}
				}
			}
		case tea.MouseButtonWheelUp:
			m.gridView.MoveUp()
		case tea.MouseButtonWheelDown:
			m.gridView.MoveDown()
		}
		return m, nil
	}

	// Ignore mouse when any modal overlay is active.
	if m.appState.ShowHelp || m.appState.ShowTmuxHelp || m.appState.ShowConfirm ||
		m.showAttachHint || m.recoveryPicker.Active || m.orphanPicker.Active || m.agentPicker.Active ||
		m.teamBuilder.Active || m.appState.EditingTitle ||
		m.inputMode != "" || m.dirPicker.Active {
		return m, nil
	}

	sw, _, _ := computeLayout(m.appState.TermWidth, m.appState.TermHeight)
	inSidebar := msg.X < sw

	switch msg.Button {
	case tea.MouseButtonLeft:
		if inSidebar {
			return m.handleSidebarClick(msg.Y)
		}
		// Click in preview area: attach the active session (same as pressing 'a').
		if !m.cfg.HideAttachHint {
			attach := m.pendingAttachDetails()
			if attach != nil {
				m.pendingAttach = attach
				m.showAttachHint = true
				return m, nil
			}
		}
		return m, m.attachActiveSession()

	case tea.MouseButtonWheelUp:
		if inSidebar {
			prev := m.sidebar.Cursor
			prevSession := m.appState.ActiveSessionID
			m.sidebar.MoveUp()
			if m.sidebar.Cursor != prev {
				m.syncActiveFromSidebar()
				if m.appState.ActiveSessionID != prevSession {
					m.previewPollGen++
					return m, m.schedulePollPreview()
				}
			}
		} else {
			m.preview.ScrollUp(3)
		}

	case tea.MouseButtonWheelDown:
		if inSidebar {
			prev := m.sidebar.Cursor
			prevSession := m.appState.ActiveSessionID
			m.sidebar.MoveDown()
			if m.sidebar.Cursor != prev {
				m.syncActiveFromSidebar()
				if m.appState.ActiveSessionID != prevSession {
					m.previewPollGen++
					return m, m.schedulePollPreview()
				}
			}
		} else {
			m.preview.ScrollDown(3)
		}
	}
	return m, nil
}

// handleSidebarClick processes a left-click at sidebar row y.
func (m Model) handleSidebarClick(y int) (tea.Model, tea.Cmd) {
	idx := m.sidebar.ItemAtRow(y)
	if idx < 0 {
		return m, nil
	}
	prev := m.sidebar.Cursor
	prevSession := m.appState.ActiveSessionID
	m.sidebar.Cursor = idx
	m.sidebar.EnsureCursorVisible(m.sidebar.Height)
	sel := m.sidebar.Selected()
	if sel == nil {
		return m, nil
	}
	switch sel.Kind {
	case components.KindProject:
		m.appState = *state.ToggleProjectCollapsed(&m.appState, sel.ProjectID)
		m.sidebar.Rebuild(&m.appState)
		m.persist()
		return m, nil
	case components.KindTeam:
		m.appState = *state.ToggleTeamCollapsed(&m.appState, sel.TeamID)
		m.sidebar.Rebuild(&m.appState)
		m.persist()
		return m, nil
	case components.KindSession:
		if m.sidebar.Cursor != prev {
			m.syncActiveFromSidebar()
			if m.appState.ActiveSessionID != prevSession {
				m.previewPollGen++
				return m, m.schedulePollPreview()
			}
		}
	}
	return m, nil
}

func (m Model) handleFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter":
		m.appState.FilterActive = false
		if msg.String() == "esc" {
			m.appState.FilterQuery = ""
			m.sidebar.FilterQuery = ""
			m.sidebar.Rebuild(&m.appState)
		}
		return m, nil
	case "backspace":
		if len(m.appState.FilterQuery) > 0 {
			m.appState.FilterQuery = m.appState.FilterQuery[:len(m.appState.FilterQuery)-1]
			m.sidebar.FilterQuery = m.appState.FilterQuery
			m.sidebar.Rebuild(&m.appState)
		}
		return m, nil
	default:
		if len(msg.String()) == 1 {
			m.appState.FilterQuery += msg.String()
			m.sidebar.FilterQuery = m.appState.FilterQuery
			m.sidebar.Rebuild(&m.appState)
		}
	}
	return m, nil
}

// --- Name input ---

func (m Model) handleNameInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		val := strings.TrimSpace(m.nameInput.Value())
		if val == "" {
			return m, nil
		}
		if m.inputMode == "project-name" {
			// Step 1 done: open the interactive directory picker.
			m.pendingProjectName = val
			m.inputMode = ""
			m.nameInput.Blur()
			cwd, _ := os.Getwd()
			m.dirPicker.Show(cwd)
			return m, nil
		}
	case "esc":
		m.nameInput.Blur()
		m.inputMode = ""
		m.pendingProjectName = ""
		return m, nil
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
}

// --- Directory creation confirmation ---

func (m Model) handleDirConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		dir := strings.TrimSpace(m.nameInput.Value())
		if err := os.MkdirAll(dir, 0755); err != nil {
			m.inputMode = ""
			m.pendingProjectName = ""
			return m, func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("create directory: %w", err)} }
		}
		m.inputMode = ""
		cmd := m.createProject(m.pendingProjectName, dir)
		m.pendingProjectName = ""
		return m, cmd
	case "n", "N", "esc":
		// Return to directory picker so user can choose a different path.
		m.inputMode = ""
		dir := strings.TrimSpace(m.nameInput.Value())
		m.dirPicker.Show(dir)
		return m, nil
	}
	return m, nil
}

// --- Worktree branch input ---

func (m Model) handleWorktreeBranchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		branch := strings.TrimSpace(m.nameInput.Value())
		if branch == "" {
			return m, nil
		}
		m.nameInput.Blur()
		m.inputMode = ""
		agentType := m.pendingWorktreeAgentType
		agentCmd := m.pendingWorktreeAgentCmd
		m.pendingWorktreeAgentType = ""
		m.pendingWorktreeAgentCmd = nil
		m.pendingWorktree = false
		return m, m.createSessionWithWorktree(m.pendingProjectID, agentType, agentCmd, branch)
	case "esc":
		m.nameInput.Blur()
		m.inputMode = ""
		m.pendingWorktree = false
		m.pendingWorktreeAgentType = ""
		m.pendingWorktreeAgentCmd = nil
		return m, nil
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
}

// --- Confirmation ---

func (m Model) handleConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Confirm) {
		action := m.appState.ConfirmAction
		m.appState.ShowConfirm = false
		m.confirm.Message = ""
		return m, func() tea.Msg { return ConfirmedMsg{Action: action} }
	}
	if key.Matches(msg, m.keys.Cancel) {
		m.appState.ShowConfirm = false
		m.confirm.Message = ""
		return m, nil
	}
	return m, nil
}

func (m Model) handleConfirmedAction(action string) tea.Cmd {
	if strings.HasPrefix(action, "kill-session:") {
		sessionID := strings.TrimPrefix(action, "kill-session:")
		return m.killSession(sessionID)
	}
	if strings.HasPrefix(action, "kill-team:") {
		teamID := strings.TrimPrefix(action, "kill-team:")
		return m.killTeam(teamID)
	}
	if strings.HasPrefix(action, "kill-project:") {
		projectID := strings.TrimPrefix(action, "kill-project:")
		return m.killProject(projectID)
	}
	if strings.HasPrefix(action, "install-agent:") {
		agentType := strings.TrimPrefix(action, "install-agent:")
		installCmd := m.cfg.Agents[agentType].InstallCmd
		if len(installCmd) == 0 {
			return func() tea.Msg {
				return ErrorMsg{Err: fmt.Errorf("no install command configured for %q", agentType)}
			}
		}
		m.appState.LastError = ""
		m.appState.InstallingAgent = agentType
		return func() tea.Msg {
			if err := exec.Command(installCmd[0], installCmd[1:]...).Run(); err != nil {
				return ErrorMsg{Err: fmt.Errorf("install %s failed: %w", agentType, err)}
			}
			return AgentInstalledMsg{AgentType: agentType}
		}
	}
	if strings.HasPrefix(action, "gitignore-worktrees:") {
		// action format: "gitignore-worktrees:<projectID>:<branch>"
		rest := strings.TrimPrefix(action, "gitignore-worktrees:")
		colonIdx := strings.Index(rest, ":")
		if colonIdx < 0 {
			return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("malformed gitignore action")} }
		}
		projectID := rest[:colonIdx]
		branch := rest[colonIdx+1:]

		var proj *state.Project
		for _, p := range m.appState.Projects {
			if p.ID == projectID {
				proj = p
				break
			}
		}
		if proj == nil {
			return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("project not found")} }
		}
		projDir := proj.Directory
		if projDir == "" {
			projDir, _ = os.Getwd()
		}
		gitRoot, err := git.Root(projDir)
		if err != nil {
			return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("git root not found: %w", err)} }
		}
		_ = git.AddToGitignore(gitRoot, ".worktrees")
		worktreePath := git.WorktreePath(gitRoot, branch)
		agentTypeStr := m.pendingWorktreeAgentType
		agentCmd := m.pendingWorktreeAgentCmd
		m.pendingWorktreeAgentType = ""
		m.pendingWorktreeAgentCmd = nil
		return m.spawnWorktreeSession(proj, agentTypeStr, agentCmd, branch, gitRoot, worktreePath)
	}
	if action == "quit-kill" {
		m.killAllSessions()
		return tea.Quit
	}
	return nil
}

// --- Session/project/team operations ---

func (m *Model) createProject(name, directory string) tea.Cmd {
	_, proj := state.CreateProject(&m.appState, name, "", "#7C3AED", directory)
	m.sidebar.Rebuild(&m.appState)
	m.persist()
	m.fireHook(state.HookEvent{
		Name:        state.EventProjectCreate,
		ProjectID:   proj.ID,
		ProjectName: proj.Name,
		WorkDir:     proj.Directory,
	})
	return func() tea.Msg { return ProjectCreatedMsg{Project: proj} }
}

func (m *Model) createSessionWithWorktree(projectID, agentTypeStr string, agentCmd []string, branch string) tea.Cmd {
	// Find project directory.
	var proj *state.Project
	for _, p := range m.appState.Projects {
		if p.ID == projectID {
			proj = p
			break
		}
	}
	if proj == nil {
		return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("project not found")} }
	}
	projDir := proj.Directory
	if projDir == "" {
		projDir, _ = os.Getwd()
	}

	// Resolve git root.
	gitRoot, err := git.Root(projDir)
	if err != nil {
		return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("not a git repository: %w", err)} }
	}

	worktreePath := git.WorktreePath(gitRoot, branch)

	// If .worktrees is not yet in .gitignore, ask the user first.
	if !git.IsInGitignore(gitRoot, ".worktrees") {
		// Stash branch/agent info so the confirm handler can retrieve them.
		m.pendingWorktreeAgentType = agentTypeStr
		m.pendingWorktreeAgentCmd = agentCmd
		return func() tea.Msg {
			return ConfirmActionMsg{
				Message: "Add \".worktrees\" to .gitignore?\n\n(Recommended to keep worktrees out of git history)",
				Action:  "gitignore-worktrees:" + projectID + ":" + branch,
			}
		}
	}

	return m.spawnWorktreeSession(proj, agentTypeStr, agentCmd, branch, gitRoot, worktreePath)
}

func (m *Model) spawnWorktreeSession(proj *state.Project, agentTypeStr string, agentCmd []string, branch, gitRoot, worktreePath string) tea.Cmd {
	agentType := state.AgentType(agentTypeStr)
	if len(agentCmd) == 0 {
		if profile, ok := m.cfg.Agents[agentTypeStr]; ok {
			agentCmd = profile.Cmd
		} else {
			agentCmd = []string{agentTypeStr}
		}
	}

	// Create the worktree first so the agent starts in an existing directory.
	if err := git.CreateWorktree(gitRoot, branch, worktreePath); err != nil {
		return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("worktree creation failed: %w", err)} }
	}

	muxSess := mux.SessionName(proj.ID)
	sessionTitle := branch

	var windowIdx int
	var err error
	if !mux.SessionExists(muxSess) {
		winName := mux.WindowName(sessionTitle)
		if err = mux.CreateSession(muxSess, winName, worktreePath, agentCmd); err != nil {
			_ = git.RemoveWorktree(gitRoot, worktreePath)
			return func() tea.Msg { return ErrorMsg{Err: err} }
		}
		windowIdx = 0
	} else {
		winName := mux.WindowName(sessionTitle)
		windowIdx, err = mux.CreateWindow(muxSess, winName, worktreePath, agentCmd)
		if err != nil {
			_ = git.RemoveWorktree(gitRoot, worktreePath)
			return func() tea.Msg { return ErrorMsg{Err: err} }
		}
	}

	_, sess := state.CreateSession(&m.appState, proj.ID, sessionTitle, agentType, agentCmd, worktreePath, muxSess, windowIdx)
	sess.WorktreePath = worktreePath
	sess.WorktreeBranch = branch
	state.RecordAgentUsage(&m.appState, agentTypeStr)
	m.sidebar.Rebuild(&m.appState)
	m.persist()

	m.fireHook(state.HookEvent{
		Name:         state.EventSessionCreate,
		ProjectID:    proj.ID,
		ProjectName:  proj.Name,
		SessionID:    sess.ID,
		SessionTitle: sess.Title,
		AgentType:    agentType,
		AgentCmd:     agentCmd,
		TmuxSession:  muxSess,
		TmuxWindow:   windowIdx,
		WorkDir:      worktreePath,
	})

	return func() tea.Msg { return SessionCreatedMsg{Session: sess} }
}

func (m *Model) createSession(projectID, agentTypeStr string, agentCmd []string) tea.Cmd {
	agentType := state.AgentType(agentTypeStr)
	if len(agentCmd) == 0 {
		if profile, ok := m.cfg.Agents[agentTypeStr]; ok {
			agentCmd = profile.Cmd
		} else {
			agentCmd = []string{agentTypeStr}
		}
	}

	// Find project to get tmux session name.
	var proj *state.Project
	for _, p := range m.appState.Projects {
		if p.ID == projectID {
			proj = p
			break
		}
	}
	if proj == nil {
		return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("project not found")} }
	}

	workDir := proj.Directory
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	muxSess := mux.SessionName(projectID)
	sessionTitle := git.RandomBranchName()
	for _, s := range proj.Sessions {
		if s.Title == sessionTitle {
			sessionTitle = git.RandomBranchName()
		}
	}

	var windowIdx int
	var err error
	if !mux.SessionExists(muxSess) {
		winName := mux.WindowName(sessionTitle)
		if err = mux.CreateSession(muxSess, winName, workDir, agentCmd); err != nil {
			return func() tea.Msg { return ErrorMsg{Err: err} }
		}
		windowIdx = 0
	} else {
		winName := mux.WindowName(sessionTitle)
		windowIdx, err = mux.CreateWindow(muxSess, winName, workDir, agentCmd)
		if err != nil {
			return func() tea.Msg { return ErrorMsg{Err: err} }
		}
	}

	_, sess := state.CreateSession(&m.appState, projectID, sessionTitle, agentType, agentCmd, workDir, muxSess, windowIdx)
	state.RecordAgentUsage(&m.appState, agentTypeStr)
	m.sidebar.Rebuild(&m.appState)
	m.persist()

	m.fireHook(state.HookEvent{
		Name:         state.EventSessionCreate,
		ProjectID:    projectID,
		ProjectName:  proj.Name,
		SessionID:    sess.ID,
		SessionTitle: sess.Title,
		AgentType:    agentType,
		AgentCmd:     agentCmd,
		TmuxSession:  muxSess,
		TmuxWindow:   windowIdx,
		WorkDir:      workDir,
	})
	return func() tea.Msg { return SessionCreatedMsg{Session: sess} }
}

func (m *Model) createTeam(spec components.TeamSpec) tea.Cmd {
	projectID := m.pendingProjectID
	var proj *state.Project
	for _, p := range m.appState.Projects {
		if p.ID == projectID {
			proj = p
			break
		}
	}
	if proj == nil {
		return func() tea.Msg { return ErrorMsg{Err: fmt.Errorf("project not found")} }
	}

	_, team := state.CreateTeam(&m.appState, projectID, spec.Name, spec.Goal, spec.SharedWorkDir)
	m.fireHook(state.HookEvent{
		Name:        state.EventTeamCreate,
		ProjectID:   projectID,
		ProjectName: proj.Name,
		TeamID:      team.ID,
		TeamName:    team.Name,
		WorkDir:     spec.SharedWorkDir,
	})

	muxSess := mux.SessionName(projectID)

	// Create orchestrator session.
	var cmds []tea.Cmd
	orchCmd := m.agentCmd(string(spec.OrchestratorAgent))
	cmds = append(cmds, m.addTeamSession(proj, team, state.RoleOrchestrator, "orchestrator", spec.OrchestratorAgent, orchCmd, spec.SharedWorkDir, muxSess))

	// Create worker sessions.
	for i, agentType := range spec.Workers {
		workerCmd := m.agentCmd(string(agentType))
		title := fmt.Sprintf("worker-%d", i+1)
		cmds = append(cmds, m.addTeamSession(proj, team, state.RoleWorker, title, agentType, workerCmd, spec.SharedWorkDir, muxSess))
	}

	m.sidebar.Rebuild(&m.appState)
	m.persist()
	return tea.Batch(append(cmds, func() tea.Msg { return TeamCreatedMsg{Team: team} })...)
}

func (m *Model) addTeamSession(proj *state.Project, team *state.Team, role state.TeamRole, title string, agentType state.AgentType, agentCmd []string, workDir, muxSess string) tea.Cmd {
	var windowIdx int
	var err error
	if !mux.SessionExists(muxSess) {
		winName := mux.WindowName(title)
		if err = mux.CreateSession(muxSess, winName, workDir, agentCmd); err != nil {
			return func() tea.Msg { return ErrorMsg{Err: err} }
		}
		windowIdx = 0
	} else {
		winName := mux.WindowName(title)
		windowIdx, err = mux.CreateWindow(muxSess, winName, workDir, agentCmd)
		if err != nil {
			return func() tea.Msg { return ErrorMsg{Err: err} }
		}
	}

	_, sess := state.AddTeamSession(&m.appState, proj.ID, team.ID, role, title, agentType, agentCmd, workDir, muxSess, windowIdx)
	state.RecordAgentUsage(&m.appState, string(agentType))
	m.fireHook(state.HookEvent{
		Name:         state.EventTeamMemberAdd,
		ProjectID:    proj.ID,
		ProjectName:  proj.Name,
		SessionID:    sess.ID,
		SessionTitle: title,
		TeamID:       team.ID,
		TeamName:     team.Name,
		TeamRole:     role,
		AgentType:    agentType,
		AgentCmd:     agentCmd,
		TmuxSession:  muxSess,
		TmuxWindow:   windowIdx,
		WorkDir:      workDir,
	})
	return func() tea.Msg { return SessionCreatedMsg{Session: sess} }
}

func (m *Model) attachActiveSession() tea.Cmd {
	sel := m.sidebar.Selected()
	if sel == nil || sel.SessionID == "" {
		return nil
	}
	sess := m.activeSessionByID(sel.SessionID)
	if sess == nil {
		return nil
	}
	m.fireHook(state.HookEvent{
		Name:         state.EventSessionAttach,
		SessionID:    sess.ID,
		SessionTitle: sess.Title,
		AgentType:    sess.AgentType,
		TmuxSession:  sess.TmuxSession,
		TmuxWindow:   sess.TmuxWindow,
		WorkDir:      sess.WorkDir,
	})
	return func() tea.Msg {
		return SessionAttachMsg{
			TmuxSession:     sess.TmuxSession,
			TmuxWindow:      sess.TmuxWindow,
			RestoreGridMode: state.GridRestoreNone,
			SessionTitle:    sess.Title,
			AgentType:       sess.AgentType,
			ProjectName:     m.projectNameByID(sess.ProjectID),
		}
	}
}

func (m *Model) killSession(sessionID string) tea.Cmd {
	sess := m.activeSessionByID(sessionID)
	var tmuxSess string
	if sess != nil {
		tmuxSess = sess.TmuxSession
		target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
		_ = mux.KillWindow(target)
		m.fireHook(state.HookEvent{
			Name:         state.EventSessionKill,
			SessionID:    sess.ID,
			SessionTitle: sess.Title,
			AgentType:    sess.AgentType,
			TmuxSession:  sess.TmuxSession,
			TmuxWindow:   sess.TmuxWindow,
		})
		// Clean up git worktree if this session owns one.
		if sess.WorktreePath != "" {
			repoDir := sess.WorkDir
			if gitRoot, err := git.Root(repoDir); err == nil {
				_ = git.RemoveWorktree(gitRoot, sess.WorktreePath)
			}
		}
	}
	m.appState = *state.RemoveSession(&m.appState, sessionID)
	// If this was the last window in the tmux session, clean up the container.
	if tmuxSess != "" {
		killTmuxSessionIfEmpty(&m.appState, tmuxSess)
	}
	m.sidebar.Rebuild(&m.appState)
	m.persist()
	return func() tea.Msg { return SessionKilledMsg{SessionID: sessionID} }
}

func (m *Model) killTeam(teamID string) tea.Cmd {
	// Kill all sessions in the team.
	var teamTmuxSessions []string
	for _, p := range m.appState.Projects {
		for _, t := range p.Teams {
			if t.ID == teamID {
				for _, sess := range t.Sessions {
					teamTmuxSessions = append(teamTmuxSessions, sess.TmuxSession)
					target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
					_ = mux.KillWindow(target)
				}
				m.fireHook(state.HookEvent{
					Name:   state.EventTeamKill,
					TeamID: t.ID, TeamName: t.Name,
				})
			}
		}
	}
	m.appState = *state.RemoveTeam(&m.appState, teamID)
	// Clean up any now-empty tmux session containers.
	for _, s := range teamTmuxSessions {
		killTmuxSessionIfEmpty(&m.appState, s)
	}
	m.sidebar.Rebuild(&m.appState)
	m.persist()
	return func() tea.Msg { return TeamKilledMsg{TeamID: teamID} }
}

func (m *Model) killProject(projectID string) tea.Cmd {
	for _, p := range m.appState.Projects {
		if p.ID == projectID {
			for _, sess := range p.Sessions {
				target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
				_ = mux.KillWindow(target)
			}
			for _, t := range p.Teams {
				for _, sess := range t.Sessions {
					target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
					_ = mux.KillWindow(target)
				}
			}
			// Kill session if still around.
			_ = mux.KillSession(mux.SessionName(projectID))
			m.fireHook(state.HookEvent{
				Name:      state.EventProjectKill,
				ProjectID: p.ID, ProjectName: p.Name,
			})
		}
	}
	m.appState = *state.RemoveProject(&m.appState, projectID)
	m.sidebar.Rebuild(&m.appState)
	m.persist()
	return func() tea.Msg { return ProjectKilledMsg{ProjectID: projectID} }
}

func (m *Model) killAllSessions() {
	// Collect unique tmux session containers before we start killing windows.
	tmuxSessions := uniqueTmuxSessionNames(&m.appState)
	for _, sess := range state.AllSessions(&m.appState) {
		target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
		_ = mux.KillWindow(target)
	}
	// All windows are gone — kill the now-empty session containers.
	for _, s := range tmuxSessions {
		_ = mux.KillSession(s)
	}
}

// --- Helpers ---

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

func (m *Model) syncActiveFromSidebar() {
	sel := m.sidebar.Selected()
	if sel == nil {
		debugLog.Printf("syncActiveFromSidebar: no selection (cursor=%d items=%d)", m.sidebar.Cursor, len(m.sidebar.Items))
		return
	}
	prevSession := m.appState.ActiveSessionID
	prevProject := m.appState.ActiveProjectID
	if sel.SessionID != "" && sel.SessionID != m.appState.ActiveSessionID {
		// Switch to the new session. Restore any cached preview content so the
		// pane shows something immediately; the fresh PollPreview tick will
		// replace it with up-to-date output shortly after.
		m.appState.ActiveSessionID = sel.SessionID
		cached := m.contentSnapshots[sel.SessionID]
		m.appState.PreviewContent = cached
		m.preview.SetContent(cached)
	}
	if sel.ProjectID != "" {
		m.appState.ActiveProjectID = sel.ProjectID
	}
	if sel.TeamID != "" {
		m.appState.ActiveTeamID = sel.TeamID
	}
	debugLog.Printf("syncActiveFromSidebar: cursor=%d kind=%d sess=%s->%s proj=%s->%s",
		m.sidebar.Cursor, sel.Kind,
		prevSession, m.appState.ActiveSessionID,
		prevProject, m.appState.ActiveProjectID)
}

func (m *Model) activeSessionByID(id string) *state.Session {
	for _, s := range state.AllSessions(&m.appState) {
		if s.ID == id {
			return s
		}
	}
	return nil
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
		// Extract agent type strings from the items via FilterValue.
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
	}
}

// handleAttachHint handles key input while the attach hint overlay is shown.
func (m Model) handleAttachHint(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y", " ":
		m.showAttachHint = false
		attach := m.pendingAttach
		m.pendingAttach = nil
		if attach == nil {
			return m, nil
		}
		cmd := m.doAttach(*attach)
		return m, cmd
	case "d":
		// Don't show again: save to config.
		m.showAttachHint = false
		m.cfg.HideAttachHint = true
		_ = config.Save(m.cfg)
		attach := m.pendingAttach
		m.pendingAttach = nil
		if attach == nil {
			return m, nil
		}
		cmd := m.doAttach(*attach)
		return m, cmd
	case "esc", "q":
		m.showAttachHint = false
		m.pendingAttach = nil
	}
	return m, nil
}

// attachHintView renders the attach hint dialog content.
func (m Model) attachHintView() string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 3).
		Render(
			styles.TitleStyle.Render("Attaching to session") + "\n\n" +
				"You are about to attach to a running agent session.\n" +
				"The Hive TUI will be suspended while you work.\n\n" +
				lipgloss.NewStyle().Bold(true).Render("To return to Hive:") + "  press  " +
				lipgloss.NewStyle().
					Foreground(styles.ColorAccent).
					Bold(true).
					Render(mux.DetachKey()) +
				"\n\n" +
				styles.MutedStyle.Render("enter: proceed  d: don't show again  esc: cancel"),
		)
}

func (m *Model) recomputeLayout() {
	sw, pw, ch := computeLayout(m.appState.TermWidth, m.appState.TermHeight)
	m.sidebar.Width = sw
	m.sidebar.Height = ch
	m.preview.Resize(pw, ch)
	m.statusBar.Width = m.appState.TermWidth
}

func (m *Model) persist() {
	if err := saveState(&m.appState); err != nil {
		log.Printf("hive: failed to save state: %v", err)
	}
	if err := saveUsage(m.appState.AgentUsage); err != nil {
		log.Printf("hive: failed to save usage: %v", err)
	}
}

// recoverSessions creates a "Recovered Sessions" project (if it doesn't already
// exist) and adds each selected RecoverableSession as a state Session pointing
// at the existing tmux window.
func (m *Model) recoverSessions(sessions []state.RecoverableSession) {
	workDir := m.appState.RecoveryWorkDir

	// Find or create the recovery project.
	var proj *state.Project
	for _, p := range m.appState.Projects {
		if p.Name == "Recovered Sessions" {
			proj = p
			break
		}
	}
	if proj == nil {
		var newProj *state.Project
		_, newProj = state.CreateProject(&m.appState, "Recovered Sessions", "", "#6B7280", workDir)
		proj = newProj
	}

	for _, rs := range sessions {
		agentType := rs.DetectedAgentType
		if agentType == "" {
			agentType = state.AgentCustom
		}
		title := rs.WindowName
		if title == "" {
			title = fmt.Sprintf("%s:%d", rs.TmuxSession, rs.WindowIndex)
		}
		state.CreateSession(
			&m.appState,
			proj.ID,
			title,
			agentType,
			nil,
			workDir,
			rs.TmuxSession,
			rs.WindowIndex,
		)
	}

	m.sidebar.Rebuild(&m.appState)
	m.persist()
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

// projectNameByID returns the display name for a project ID, or "" if not found.
func (m *Model) projectNameByID(id string) string {
	for _, p := range m.appState.Projects {
		if p.ID == id {
			return p.Name
		}
	}
	return ""
}

func (m *Model) teamNameByID(id string) string {
	for _, p := range m.appState.Projects {
		for _, t := range p.Teams {
			if t.ID == id {
				return t.Name
			}
		}
	}
	return id
}

// sessionByTmux returns the session matching the given tmux session + window, or nil.
func (m *Model) sessionByTmux(tmuxSession string, tmuxWindow int) *state.Session {
	for _, p := range m.appState.Projects {
		for _, s := range p.Sessions {
			if s.TmuxSession == tmuxSession && s.TmuxWindow == tmuxWindow {
				return s
			}
		}
	}
	return nil
}

func (m *Model) scheduleGridPoll() tea.Cmd {
	sessions := m.gridSessions(m.gridView.Mode)
	if len(sessions) == 0 {
		return nil
	}
	interval := time.Duration(m.cfg.PreviewRefreshMs) * time.Millisecond
	return components.PollGridPreviews(sessions, interval)
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
	for _, sess := range state.AllSessions(&m.appState) {
		if sess.Status != state.StatusDead {
			targets[sess.ID] = mux.Target(sess.TmuxSession, sess.TmuxWindow)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	interval := time.Duration(m.cfg.PreviewRefreshMs*2) * time.Millisecond
	return escape.WatchStatuses(targets, m.contentSnapshots, interval)
}

// --- View helpers ---

func (m Model) overlayView(overlay string) string {
	w := m.appState.TermWidth
	h := m.appState.TermHeight
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	// Place overlay centered over a dark background filling the terminal.
	return lipgloss.Place(w, h,
		lipgloss.Center, lipgloss.Center,
		overlay,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#111827")),
	)
}

func (m Model) renameDialogView() string {
	title := "Rename Session"
	if m.titleEditor.TeamID != "" {
		title = "Rename Team"
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(50).
		Render(
			styles.TitleStyle.Render(title) + "\n\n" +
				m.titleEditor.View() + "\n\n" +
				styles.MutedStyle.Render("enter: save  esc: cancel  ctrl+u: clear"),
		)
}

func (m Model) nameInputView(title, prompt, hint string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(56).
		Render(
			styles.TitleStyle.Render(title) + "\n\n" +
				prompt + "\n" +
				m.nameInput.View() + "\n\n" +
				styles.MutedStyle.Render(hint),
		)
}

func (m Model) dirConfirmView() string {
	dir := strings.TrimSpace(m.nameInput.Value())
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(56).
		Render(
			styles.TitleStyle.Render("New Project (2/2)") + "\n\n" +
				"Directory does not exist:\n" +
				styles.MutedStyle.Render(dir) + "\n\n" +
				"Create it?" + "\n\n" +
				styles.MutedStyle.Render("y/enter: create  n/esc: back"),
		)
}

func (m Model) helpView() string {
	type binding struct{ key, desc string }
	bindings := []binding{
		{"j/k ↑↓", "navigate sessions"},
		{"J/K", "navigate projects"},
		{"tab", "toggle sidebar/preview focus"},
		{"enter/a", "attach to session"},
		{"space", "toggle collapse project/team"},
		{"n", "new project"},
		{"t", "new session (agent picker)"},
		{"W", "new worktree session"},
		{"T", "new agent team (wizard)"},
		{"r", "rename session or team"},
		{"x/d", "kill session"},
		{"D", "kill entire team"},
		{"/", "filter sessions"},
		{"ctrl+p", "command palette"},
		{"g", "grid overview (all sessions)"},
		{"1-9", "jump to project by number"},
		{"S", "open settings"},
		{"?", "toggle this help"},
		{"H", "tmux shortcuts reference"},
		{"q", "quit (sessions persist in tmux)"},
		{"Q", "quit and kill all sessions"},
	}
	var rows []string
	for _, b := range bindings {
		row := fmt.Sprintf("  %s  %s",
			styles.HelpKeyStyle.Width(14).Render(b.key),
			styles.HelpDescStyle.Render(b.desc),
		)
		rows = append(rows, row)
	}
	content := styles.TitleStyle.Render("Hive — Keyboard Shortcuts") + "\n\n" +
		strings.Join(rows, "\n") + "\n\n" +
		styles.MutedStyle.Render("Press ? or esc to close")

	return lipgloss.Place(m.appState.TermWidth, m.appState.TermHeight,
		lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(styles.ColorAccent).
			Padding(1, 3).
			Render(content),
	)
}

func (m Model) tmuxHelpView() string {
	type binding struct{ key, desc string }
	bindings := []binding{
		{mux.DetachKey(), "detach from session (return to hive)"},
		{"ctrl+b c", "create a new window"},
		{"ctrl+b n / p", "next / previous window"},
		{"ctrl+b 0-9", "switch to window by number"},
		{"ctrl+b ,", "rename current window"},
		{"ctrl+b %", "split pane vertically"},
		{"ctrl+b \"", "split pane horizontally"},
		{"ctrl+b arrow", "navigate between panes"},
		{"ctrl+b z", "zoom/unzoom current pane"},
		{"ctrl+b x", "kill current pane"},
		{"ctrl+b [", "enter scroll/copy mode (q to exit)"},
		{"ctrl+b ]", "paste from tmux buffer"},
		{"ctrl+b ?", "show all tmux key bindings"},
		{"ctrl+b t", "show clock"},
		{"ctrl+b $", "rename current session"},
	}
	var rows []string
	for _, b := range bindings {
		row := fmt.Sprintf("  %s  %s",
			styles.HelpKeyStyle.Width(18).Render(b.key),
			styles.HelpDescStyle.Render(b.desc),
		)
		rows = append(rows, row)
	}
	content := styles.TitleStyle.Render("tmux Shortcuts Reference") + "\n\n" +
		strings.Join(rows, "\n") + "\n\n" +
		styles.MutedStyle.Render("Press H or esc to close")

	return lipgloss.Place(m.appState.TermWidth, m.appState.TermHeight,
		lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(styles.ColorAccent).
			Padding(1, 3).
			Render(content),
	)
}

// doAttach returns the tea.Cmd that performs session attachment.
//
// For the tmux backend (UseExecAttach = true) it uses tea.ExecProcess so the
// TUI is merely suspended during the attach — no restart, no state reload.
//   - If the backend supports display-popup (tmux ≥ 3.2, running inside tmux)
//     the session opens as a floating overlay over the TUI.
//   - Otherwise a plain full-screen tmux attach-session is used.
//
// For the native backend (UseExecAttach = false) it falls back to setting
// attachPending and calling tea.Quit; cmd/start.go handles the restart.
func (m *Model) doAttach(sess SessionAttachMsg) tea.Cmd {
	target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
	restoreMode := sess.RestoreGridMode

	if !mux.UseExecAttach() {
		// Native backend: use the classic quit+restart path.
		m.attachPending = &sess
		return tea.Quit
	}

	var cmd *exec.Cmd
	if mux.SupportsPopup() {
		// Floating popup overlay — TUI stays alive underneath.
		cmd = exec.Command("tmux", "display-popup",
			"-E",        // close popup when command exits
			"-w", "95%",
			"-h", "90%",
			"--",
			"tmux", "attach-session", "-t", target,
		)
	} else {
		// Full-screen attach. Run tmux directly without a shell wrapper.
		// The terminal title is set by the native RunAttach path only (that
		// path has an opportunity to write escapes between TUI exit and
		// attach); for the ExecProcess path the tmux status bar provides
		// sufficient session context.
		cmd = exec.Command("tmux", "attach-session", "-t", target)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return AttachDoneMsg{Err: err, RestoreGridMode: restoreMode}
	})
}

// RunAttach handles the attach flow for the native backend: called by cmd/start.go
// after tea.Program.Run returns with a non-nil LastAttach(). Not used when the
// tmux backend is active (which uses tea.ExecProcess internally).
func RunAttach(sess SessionAttachMsg) error {
	// Set the terminal window/tab title so the user always knows which session
	// they are in, even when the attached app redraws the entire screen.
	// ESC[22;0t pushes the current title onto xterm's internal stack.
	// ESC]0;...\a sets a new title. ESC[23;0t (deferred) pops and restores it.
	fmt.Print("\033[22;0t")
	fmt.Printf("\033]0;%s\007", buildAttachTitle(sess))
	defer fmt.Print("\033[23;0t")

	target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
	return mux.Attach(target)
}

// buildAttachTitle returns a terminal window title string for the attached session.
func buildAttachTitle(sess SessionAttachMsg) string {
	agent := string(sess.AgentType)
	if sess.ProjectName != "" && sess.SessionTitle != "" {
		return fmt.Sprintf("Hive | %s / %s (%s)", sess.ProjectName, sess.SessionTitle, agent)
	}
	if sess.SessionTitle != "" {
		return fmt.Sprintf("Hive | %s (%s)", sess.SessionTitle, agent)
	}
	if sess.ProjectName != "" {
		return fmt.Sprintf("Hive | %s (%s)", sess.ProjectName, agent)
	}
	return "Hive"
}
