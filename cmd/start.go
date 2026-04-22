package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/lucascaro/hive/internal/changelog"
	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/git"
	"github.com/lucascaro/hive/internal/mux"
	muxnative "github.com/lucascaro/hive/internal/mux/native"
	muxtmux "github.com/lucascaro/hive/internal/mux/tmux"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui"
)

// findSessionID returns the session ID matching the given mux session/window, or "".
func findSessionID(appState *state.AppState, muxSession string, muxWindow int) string {
	for _, sess := range state.AllSessions(appState) {
		if sess.TmuxSession == muxSession && sess.TmuxWindow == muxWindow {
			return sess.ID
		}
	}
	return ""
}

var startNative bool

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Hive TUI",
	RunE:  runStart,
}

func init() {
	startCmd.Flags().BoolVar(&startNative, "native", false, "Use the native PTY backend instead of tmux")
}

func runStart(_ *cobra.Command, _ []string) error {
	// Ensure config directory exists.
	if err := config.Ensure(); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Load config.
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	// MigrateAndPersist (vs. Migrate) so one-shot upgrade side effects —
	// e.g. resetting hide_attach_hint after the detach key changed in v2 —
	// are written back to disk and only fire once per user.
	cfg, err = config.MigrateAndPersist(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to persist migrated config: %v\n", err)
	}

	// Compute "What's New" content if version changed.
	var whatsNewContent string
	if cfg.LastSeenVersion != Version && !cfg.HideWhatsNew {
		raw := changelog.ParseSince(EmbeddedChangelog, cfg.LastSeenVersion)
		if raw != "" {
			whatsNewContent = changelog.Render(raw, 60)
		}
	}
	// Always update last seen version.
	if cfg.LastSeenVersion != Version {
		cfg.LastSeenVersion = Version
		if err := config.Save(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to persist last seen version: %v\n", err)
		}
	}

	// --native flag overrides config.
	if startNative {
		cfg.Multiplexer = "native"
	}

	// Select and initialise the multiplexer backend.
	if err := initMuxBackend(cfg); err != nil {
		return err
	}

	// Validate backend availability (only relevant for the tmux backend).
	if !mux.IsAvailable() {
		return fmt.Errorf("selected backend is not available.\nFor tmux backend: install tmux (https://github.com/tmux/tmux)\nOr use the native backend: hive start --native")
	}

	// Multi-instance grouping: reclaim grouped sessions left behind by crashed
	// hive processes, then create this process's grouped session so attach
	// commands target a per-instance view of the shared window list. No-op on
	// the native backend (does not implement GroupedBackend).
	if err := mux.SweepOrphanInstances(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: orphan sweep failed: %v\n", err)
	}
	if err := mux.InitInstance(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to create per-instance tmux session: %v (falling back to shared canonical)\n", err)
	}
	defer func() {
		if err := mux.ShutdownInstance(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to tear down per-instance tmux session: %v\n", err)
		}
	}()

	// Load state.
	projects, err := tui.LoadState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load state: %v (starting fresh)\n", err)
		projects = nil
	}

	cwd, _ := os.Getwd()

	appState := state.AppState{
		Projects:        projects,
		AgentUsage:      tui.LoadUsage(),
		RecoveryWorkDir: cwd,
	}
	if appState.Projects == nil {
		appState.Projects = []*state.Project{}
	}
	if appState.AgentUsage == nil {
		appState.AgentUsage = make(map[string]state.AgentUsageRecord)
	}

	// Reconcile: mark sessions dead if their window is gone.
	reconcileState(&appState)

	// Run the TUI loop. For the native backend the loop re-enters after each
	// attach/detach cycle (tea.Quit is used because native attach cannot use
	// tea.ExecProcess). For the tmux backend the TUI handles attach internally
	// via tea.ExecProcess and never sets LastAttach(); the loop runs only once.
	for {
		// Pre-clear the primary screen buffer so that BubbleTea's internal
		// alt-screen exit/enter transitions during attach/detach are invisible
		// (blank primary instead of terminal history).
		writePrimaryBufferClear(os.Stdout)
		model := tui.New(cfg, appState, whatsNewContent)
		whatsNewContent = "" // only show on first loop iteration
		p := tea.NewProgram(model,
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
		)
		finalModel, err := p.Run()
		if err != nil {
			return fmt.Errorf("TUI error: %w", err)
		}

		// Alt-screen teardown wipes the TUI's final frame, so any status-bar
		// error message set immediately before tea.Quit (e.g. canonical tmux
		// session gone) is invisible. Mirror it to stderr so the user sees why
		// hive exited.
		if errModel, ok := finalModel.(interface{ LastError() string }); ok {
			if msg := errModel.LastError(); msg != "" {
				fmt.Fprintf(os.Stderr, "hive: %s\n", msg)
			}
		}

		// Native backend attach: re-enter TUI after detaching from the session.
		if fm, ok := finalModel.(interface{ LastAttach() *tui.SessionAttachMsg }); ok {
			if a := fm.LastAttach(); a != nil {
				if err := tui.RunAttach(*a); err != nil {
					fmt.Fprintf(os.Stderr, "attach failed: %v\n", err)
				}
				// Reload config and state so any changes made during the session
				// (or by the agent) are reflected when the TUI restarts.
				if reloadedCfg, loadErr := config.Load(); loadErr == nil {
					cfg = config.Migrate(reloadedCfg)
				}
				if reloaded, err := tui.LoadState(); err == nil && reloaded != nil {
					appState.Projects = reloaded
				}
				reconcileState(&appState)
				appState.ActiveSessionID = findSessionID(&appState, a.TmuxSession, a.TmuxWindow)
				appState.RestoreGridMode = a.RestoreGridMode
				continue
			}
		}
		break
	}
	return nil
}

// writePrimaryBufferClear writes the ANSI sequence that clears the primary
// screen buffer (the non-alt-screen buffer that holds shell history). This
// must be called before tea.NewProgram so that BubbleTea saves a blank
// primary buffer when it enters alt-screen. Any subsequent exit of alt-screen
// during attach/detach then reveals a blank screen instead of the user's
// terminal history.
//
// The sequence must be \033[2J\033[H (erase entire display + cursor home).
// It must NOT contain \033[?1049l (exit alt-screen), which would expose the
// primary buffer before it has been cleared.
func writePrimaryBufferClear(w io.Writer) {
	fmt.Fprint(w, "\033[2J\033[H")
}

// initMuxBackend selects and sets the active mux backend based on config.
// The tmux backend is used by default; set "multiplexer": "native" in config
// (or pass --native on the command line) to use the built-in PTY daemon instead.
//
// For the native backend, this also ensures the daemon is running before
// creating the backend client.
func initMuxBackend(cfg config.Config) error {
	spec, err := mux.ParseDetachKey(cfg.DetachKey)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"warning: invalid detach_key %q (%v); falling back to default %q\n",
			cfg.DetachKey, err, mux.DefaultDetachKey)
		spec, _ = mux.ParseDetachKey(mux.DefaultDetachKey)
	}

	switch cfg.Multiplexer {
	case "native":
		sockPath := muxnative.SockPath()
		logPath := filepath.Join(config.Dir(), "mux-daemon.log")
		if err := muxnative.EnsureRunning(sockPath, logPath); err != nil {
			return fmt.Errorf("start native mux daemon: %w", err)
		}
		mux.SetBackend(muxnative.NewBackend(sockPath, spec))
	default:
		mux.SetBackend(muxtmux.NewBackend(spec))
	}
	return nil
}

// reconcileState removes sessions whose window no longer exists in the backend.
// For worktree sessions, it also removes the associated git worktree.
// It also detects orphaned hive-* tmux session containers (no project in state,
// no windows) and stores their names in appState.OrphanSessions for the TUI to
// present to the user.
func reconcileState(appState *state.AppState) {
	var dead []*state.Session
	var deadIDs []string
	for _, sess := range state.AllSessions(appState) {
		windows, err := mux.ListWindows(sess.TmuxSession)
		if err != nil {
			// Session gone entirely — all windows in it are dead.
			dead = append(dead, sess)
			deadIDs = append(deadIDs, sess.ID)
			continue
		}
		found := false
		for _, w := range windows {
			if w.Index == sess.TmuxWindow {
				found = true
				break
			}
		}
		if !found {
			dead = append(dead, sess)
			deadIDs = append(deadIDs, sess.ID)
		}
	}
	for _, sess := range dead {
		// Clean up any git worktree owned by this session.
		if sess.WorktreePath != "" {
			if gitRoot, err := git.Root(sess.WorkDir); err == nil {
				if rmErr := git.RemoveWorktree(gitRoot, sess.WorktreePath); rmErr != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to remove worktree %s: %v\n", sess.WorktreePath, rmErr)
				}
			} else {
				// WorkDir is gone or not a git repo; try a direct remove as a fallback.
				fmt.Fprintf(os.Stderr, "warning: cannot locate git root for session %s (%v); attempting direct worktree removal\n", sess.ID, err)
				if _, statErr := os.Stat(sess.WorktreePath); statErr == nil {
					if rmErr := os.RemoveAll(sess.WorktreePath); rmErr != nil {
						fmt.Fprintf(os.Stderr, "warning: failed to remove worktree directory %s: %v\n", sess.WorktreePath, rmErr)
					}
				}
			}
		}
	}
	for _, id := range deadIDs {
		appState = state.RemoveSession(appState, id)
	}

	// Detect orphaned hive-* tmux session containers:
	//   - empty (no windows): offer to kill
	//   - with windows but no state entry: offer to recover
	// Skip when using a custom config dir (e.g. demos) — tmux sessions from
	// the real instance would appear as false-positive orphans.
	if os.Getenv("HIVE_CONFIG_DIR") == "" {
		orphans, recoverable := detectOrphanContainers(appState)
		appState.OrphanSessions = orphans
		appState.RecoverableSessions = recoverable
	}
}

// detectOrphanContainers returns hive-* tmux session names/windows that have no
// matching project in state.
//   - orphans: sessions with no windows (empty shells, offer to kill)
//   - recoverable: individual windows in sessions not tracked by state
func detectOrphanContainers(appState *state.AppState) (orphans []string, recoverable []state.RecoverableSession) {
	allNames, err := mux.ListSessionNames()
	if err != nil || len(allNames) == 0 {
		return nil, nil
	}

	// Build set of tmux session names currently referenced by state.
	knownSessions := make(map[string]struct{})
	for _, sess := range state.AllSessions(appState) {
		knownSessions[sess.TmuxSession] = struct{}{}
	}

	for _, name := range allNames {
		if !strings.HasPrefix(name, "hive-") {
			continue
		}
		// Skip per-instance grouped sessions — they are transient mirrors
		// of the canonical session and not true orphans.
		if muxtmux.IsGroupedSession(name) {
			continue
		}
		if _, known := knownSessions[name]; known {
			continue
		}
		windows, err := mux.ListWindows(name)
		if err != nil || len(windows) == 0 {
			orphans = append(orphans, name)
			continue
		}
		for _, w := range windows {
			target := fmt.Sprintf("%s:%d", name, w.Index)
			agentType := detectAgentType(target)
			preview, _ := mux.CapturePane(target, 10)
			recoverable = append(recoverable, state.RecoverableSession{
				TmuxSession:       name,
				WindowIndex:       w.Index,
				WindowName:        w.Name,
				DetectedAgentType: agentType,
				PanePreview:       preview,
			})
		}
	}
	return orphans, recoverable
}

// knownAgents maps process/command names to their AgentType.
var knownAgents = map[string]state.AgentType{
	"claude":    state.AgentClaude,
	"codex":     state.AgentCodex,
	"gemini":    state.AgentGemini,
	"copilot":   state.AgentCopilot,
	"aider":     state.AgentAider,
	"opencode":  state.AgentOpenCode,
}

// detectAgentType tries to determine the agent type from the running process
// name, falling back to scanning pane content for known keywords.
func detectAgentType(target string) state.AgentType {
	// Primary: check the foreground process name.
	if cmd, err := mux.GetCurrentCommand(target); err == nil {
		cmd = strings.TrimSpace(strings.ToLower(cmd))
		if at, ok := knownAgents[cmd]; ok {
			return at
		}
	}
	// Fallback: scan visible pane content for agent-specific keywords.
	content, err := mux.CapturePane(target, 30)
	if err != nil {
		return ""
	}
	lower := strings.ToLower(content)
	for keyword, at := range knownAgents {
		if strings.Contains(lower, keyword) {
			return at
		}
	}
	return ""
}
