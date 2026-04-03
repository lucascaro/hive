package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

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
	cfg = config.Migrate(cfg)

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

	// Load state.
	projects, err := tui.LoadState()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load state: %v (starting fresh)\n", err)
		projects = nil
	}

	appState := state.AppState{
		Projects:   projects,
		AgentUsage: tui.LoadUsage(),
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
		model := tui.New(cfg, appState)
		p := tea.NewProgram(model,
			tea.WithAltScreen(),
			tea.WithMouseCellMotion(),
		)
		finalModel, err := p.Run()
		if err != nil {
			return fmt.Errorf("TUI error: %w", err)
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

// initMuxBackend selects and sets the active mux backend based on config.
// The tmux backend is used by default; set "multiplexer": "native" in config
// (or pass --native on the command line) to use the built-in PTY daemon instead.
//
// For the native backend, this also ensures the daemon is running before
// creating the backend client.
func initMuxBackend(cfg config.Config) error {
	switch cfg.Multiplexer {
	case "native":
		sockPath := muxnative.SockPath()
		logPath := filepath.Join(config.Dir(), "mux-daemon.log")
		if err := muxnative.EnsureRunning(sockPath, logPath); err != nil {
			return fmt.Errorf("start native mux daemon: %w", err)
		}
		mux.SetBackend(muxnative.NewBackend(sockPath))
	default:
		mux.SetBackend(muxtmux.NewBackend())
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

	// Detect orphaned hive-* tmux session containers: present in tmux but have
	// no corresponding project in state AND contain no windows (empty shells).
	appState.OrphanSessions = detectOrphanContainers(appState)
}

// detectOrphanContainers returns hive-* tmux session names that have no
// matching project in state and have no windows (truly empty orphans).
func detectOrphanContainers(appState *state.AppState) []string {
	allNames, err := mux.ListSessionNames()
	if err != nil || len(allNames) == 0 {
		return nil
	}

	// Build set of tmux session names currently referenced by state.
	knownSessions := make(map[string]struct{})
	for _, sess := range state.AllSessions(appState) {
		knownSessions[sess.TmuxSession] = struct{}{}
	}

	var orphans []string
	for _, name := range allNames {
		if !strings.HasPrefix(name, "hive-") {
			continue
		}
		if _, known := knownSessions[name]; known {
			continue
		}
		// Only flag containers that are actually empty (no windows).
		windows, err := mux.ListWindows(name)
		if err != nil || len(windows) == 0 {
			orphans = append(orphans, name)
		}
	}
	return orphans
}
