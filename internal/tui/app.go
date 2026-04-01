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
	f, err := os.OpenFile(config.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		debugLog = log.New(os.Stderr, "[hive] ", log.Ltime)
		return
	}
	debugLog = log.New(f, "[hive] ", log.Ltime|log.Lmicroseconds)
}

// Model is the root Bubble Tea model.
type Model struct {
	cfg         config.Config
	appState    state.AppState
	keys        KeyMap
	sidebar     components.Sidebar
	preview     components.Preview
	statusBar   components.StatusBar
	titleEditor components.TitleEditor
	agentPicker components.AgentPicker
	teamBuilder components.TeamBuilder
	confirm     components.Confirm
	gridView    components.GridView
	nameInput   textinput.Model // for project name / directory input
	// UI sub-states
	inputMode           string // "project-name", "project-dir", "new-session", "worktree-branch", ""
	pendingProjectName  string // name entered in step 1 of project creation
	pendingProjectID    string
	pendingAgentType    string // agent type awaiting install confirmation
	// Worktree session creation
	pendingWorktree          bool   // true when the next session should use a worktree
	pendingWorktreeAgentType string // agent type selected for worktree session
	pendingWorktreeAgentCmd  []string
	// Attach hint overlay
	showAttachHint  bool
	pendingAttach   *SessionAttachMsg
	// Attach flow: set before tea.Quit so cmd/start.go can act on it.
	attachPending *SessionAttachMsg
	// previewPollGen is incremented each time we intentionally start a fresh
	// polling cycle (session switch, new session, detach).  PreviewUpdatedMsg
	// events whose Generation doesn't match are discarded without rescheduling,
	// which lets stale concurrent poll goroutines die off naturally instead of
	// accumulating and causing rapid-fire re-renders.
	previewPollGen uint64
	// pendingPreviewClear is set when we switch sessions (or create/detach).
	// The PreviewUpdatedMsg handler issues tea.ClearScreen on the first fresh
	// update after a switch to eliminate any rendering artifacts.
	pendingPreviewClear bool
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
		nameInput:        ni,
		contentSnapshots: make(map[string]string),
	}
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
	if m.appState.RestoreGridView {
		m.appState.RestoreGridView = false
		m.gridView.Show(m.liveSessions())
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
	return tea.Batch(
		tea.SetWindowTitle("hive"),
		m.schedulePollPreview(),
		m.scheduleWatchTitles(),
		m.scheduleWatchStatuses(),
	)
}

// Update handles all messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	// --- Window resize ---
	case tea.WindowSizeMsg:
		m.appState.TermWidth = msg.Width
		m.appState.TermHeight = msg.Height
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
		// On the first fresh update after a session switch, clear the screen so
		// any rendering artifacts from the previous session are fully erased.
		if m.pendingPreviewClear {
			m.pendingPreviewClear = false
			return m, tea.Batch(m.schedulePollPreview(), tea.ClearScreen)
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
		m.pendingPreviewClear = true
		return m, tea.Batch(m.schedulePollPreview(), tea.ClearScreen)

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
		needsClear := false
		if content, ok := msg.Contents[m.appState.ActiveSessionID]; ok {
			m.appState.PreviewContent = content
			m.preview.SetContent(content)
			// If we're in the post-switch grace period, pair the content update with a
			// full screen clear so that the placeholder → content transition is rendered
			// cleanly rather than as an incremental diff (which can leave visual artifacts).
			if m.pendingPreviewClear {
				m.pendingPreviewClear = false
				needsClear = true
			}
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
		if needsClear {
			return m, tea.Batch(m.scheduleWatchStatuses(), tea.ClearScreen)
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
		m.pendingPreviewClear = true
		return m, tea.Batch(m.schedulePollPreview(), tea.ClearScreen)

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
		m.attachPending = &msg
		return m, tea.Quit

	case SessionDetachedMsg:
		m.previewPollGen++ // returning from tmux, start fresh poll chain
		m.pendingPreviewClear = true
		return m, tea.Batch(m.schedulePollPreview(), tea.ClearScreen)

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
		attach := &SessionAttachMsg{TmuxSession: msg.TmuxSession, TmuxWindow: msg.TmuxWindow, FromGridView: true}
		if !m.cfg.HideAttachHint {
			m.pendingAttach = attach
			m.showAttachHint = true
			return m, nil
		}
		m.attachPending = attach
		return m, tea.Quit

	// --- Agent picker result ---
	case components.AgentPickedMsg:
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

	// --- Key events ---
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// View renders the full UI.
func (m Model) View() string {
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
	if m.agentPicker.Active {
		return m.overlayView(m.agentPicker.View())
	}
	if m.teamBuilder.Active {
		return m.overlayView(m.teamBuilder.View())
	}
	if m.inputMode == "project-name" {
		return m.overlayView(m.nameInputView("New Project (1/2)", "Project name:", "enter: next  esc: cancel"))
	}
	if m.inputMode == "project-dir" {
		return m.overlayView(m.nameInputView("New Project (2/2)", "Working directory:", "enter: create  esc: back"))
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

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Grid overview consumes all keys while active.
	if m.gridView.Active {
		m.gridView.Width = m.appState.TermWidth
		m.gridView.Height = m.appState.TermHeight
		switch msg.String() {
		case "G":
			// Switch to all-projects view without closing.
			m.gridView.Show(m.liveSessions())
			return m, nil
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
	if m.agentPicker.Active {
		cmd, _ := m.agentPicker.Update(msg)
		return m, cmd
	}
	if m.teamBuilder.Active {
		cmd := m.teamBuilder.Update(msg)
		return m, cmd
	}
	if m.inputMode == "project-name" || m.inputMode == "project-dir" {
		return m.handleNameInput(msg)
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
		return m, func() tea.Msg { return QuitAndKillMsg{} }

	case key.Matches(msg, m.keys.Help):
		m.appState.ShowHelp = !m.appState.ShowHelp
		m.appState.ShowTmuxHelp = false
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
		var sessions []*state.Session
		for _, sess := range m.liveSessions() {
			if m.appState.ActiveProjectID == "" || sess.ProjectID == m.appState.ActiveProjectID {
				sessions = append(sessions, sess)
			}
		}
		if len(sessions) == 0 {
			sessions = m.liveSessions()
		}
		m.gridView.Show(sessions)
		return m, m.scheduleGridPoll()

	case msg.String() == "G":
		m.gridView.Show(m.liveSessions())
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
			return m, func() tea.Msg {
				return ConfirmActionMsg{
					Message: fmt.Sprintf("Kill team %q and all its sessions?", sel.Label),
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
				m.pendingPreviewClear = true
				return m, tea.Batch(m.schedulePollPreview(), tea.ClearScreen)
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
				m.pendingPreviewClear = true
				return m, tea.Batch(m.schedulePollPreview(), tea.ClearScreen)
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
				m.pendingPreviewClear = true
				return m, tea.Batch(m.schedulePollPreview(), tea.ClearScreen)
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
				m.pendingPreviewClear = true
				return m, tea.Batch(m.schedulePollPreview(), tea.ClearScreen)
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
			// Step 1 done: move to directory selection
			m.pendingProjectName = val
			m.inputMode = "project-dir"
			cwd, _ := os.Getwd()
			m.nameInput.Placeholder = "/path/to/project"
			m.nameInput.Reset()
			m.nameInput.SetValue(cwd)
			return m, nil
		}
		// Step 2: directory confirmed → create project
		dir := val
		m.nameInput.Blur()
		m.inputMode = ""
		cmd := m.createProject(m.pendingProjectName, dir)
		m.pendingProjectName = ""
		return m, cmd
	case "esc":
		if m.inputMode == "project-dir" {
			// Go back to name step
			m.inputMode = "project-name"
			m.nameInput.Reset()
			m.nameInput.SetValue(m.pendingProjectName)
			return m, nil
		}
		m.nameInput.Blur()
		m.inputMode = ""
		m.pendingProjectName = ""
		return m, nil
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
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
	proj.SessionCounter++
	sessionTitle := fmt.Sprintf("session-%d", proj.SessionCounter)

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
	// Use a monotonically incrementing counter so names stay unique even
	// after sessions are deleted (avoids "session-1" reappearing).
	proj.SessionCounter++
	sessionTitle := fmt.Sprintf("session-%d", proj.SessionCounter)

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
		Name:        state.EventSessionCreate,
		ProjectID:   projectID,
		ProjectName: proj.Name,
		SessionID:   sess.ID,
		SessionTitle: sess.Title,
		AgentType:   agentType,
		AgentCmd:    agentCmd,
		TmuxSession: muxSess,
		TmuxWindow:  windowIdx,
		WorkDir:     workDir,
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
		Name:        state.EventTeamMemberAdd,
		ProjectID:   proj.ID,
		ProjectName: proj.Name,
		SessionID:   sess.ID,
		SessionTitle: title,
		TeamID:      team.ID,
		TeamName:    team.Name,
		TeamRole:    role,
		AgentType:   agentType,
		AgentCmd:    agentCmd,
		TmuxSession: muxSess,
		TmuxWindow:  windowIdx,
		WorkDir:     workDir,
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
		Name:        state.EventSessionAttach,
		SessionID:   sess.ID,
		SessionTitle: sess.Title,
		AgentType:   sess.AgentType,
		TmuxSession: sess.TmuxSession,
		TmuxWindow:  sess.TmuxWindow,
		WorkDir:     sess.WorkDir,
	})
	return func() tea.Msg {
		return SessionAttachMsg{TmuxSession: sess.TmuxSession, TmuxWindow: sess.TmuxWindow}
	}
}

func (m *Model) killSession(sessionID string) tea.Cmd {
	sess := m.activeSessionByID(sessionID)
	if sess != nil {
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
	m.sidebar.Rebuild(&m.appState)
	m.persist()
	return func() tea.Msg { return SessionKilledMsg{SessionID: sessionID} }
}

func (m *Model) killTeam(teamID string) tea.Cmd {
	// Kill all sessions in the team.
	for _, p := range m.appState.Projects {
		for _, t := range p.Teams {
			if t.ID == teamID {
				for _, sess := range t.Sessions {
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
	for _, sess := range state.AllSessions(&m.appState) {
		target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
		_ = mux.KillWindow(target)
	}
}

// --- Helpers ---

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
	return &SessionAttachMsg{TmuxSession: sess.TmuxSession, TmuxWindow: sess.TmuxWindow}
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
		m.attachPending = attach
		return m, tea.Quit
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
		m.attachPending = attach
		return m, tea.Quit
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
	_ = saveState(&m.appState)
	_ = saveUsage(m.appState.AgentUsage)
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

func (m *Model) scheduleGridPoll() tea.Cmd {
	sessions := m.liveSessions()
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

// RunAttach handles the attach flow: exits TUI, attaches to the session, relaunches TUI.
// This is called by cmd/start.go after tea.Program.Run returns with a SessionAttachMsg.
func RunAttach(sess SessionAttachMsg) error {
	target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
	return mux.Attach(target)
}
