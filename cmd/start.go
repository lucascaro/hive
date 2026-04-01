package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/lucascaro/hive/internal/config"
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

	// Run the TUI loop (re-enter after attach/detach).
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

		// Check if we need to attach to a session.
		if fm, ok := finalModel.(interface{ LastAttach() *tui.SessionAttachMsg }); ok {
			if attach := fm.LastAttach(); attach != nil {
				if err := tui.RunAttach(*attach); err != nil {
					fmt.Fprintf(os.Stderr, "attach failed: %v\n", err)
				}
				// Reload config so preferences saved during TUI run take effect.
				if reloadedCfg, loadErr := config.Load(); loadErr == nil {
					cfg = config.Migrate(reloadedCfg)
				}
				// Reload state after returning from the attached session.
				if reloaded, err := tui.LoadState(); err == nil && reloaded != nil {
					appState.Projects = reloaded
				}
				// Reconcile again: the agent may have exited while attached.
				reconcileState(&appState)
				// Restore cursor to the session we just detached from.
				appState.ActiveSessionID = findSessionID(&appState, attach.TmuxSession, attach.TmuxWindow)
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
func reconcileState(appState *state.AppState) {
	var deadIDs []string
	for _, sess := range state.AllSessions(appState) {
		windows, err := mux.ListWindows(sess.TmuxSession)
		if err != nil {
			// Session gone entirely — all windows in it are dead.
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
			deadIDs = append(deadIDs, sess.ID)
		}
	}
	for _, id := range deadIDs {
		appState = state.RemoveSession(appState, id)
	}
}
