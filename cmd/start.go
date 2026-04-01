package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tmux"
	"github.com/lucascaro/hive/internal/tui"
)

// findSessionID returns the session ID matching the given tmux session/window, or "".
func findSessionID(appState *state.AppState, tmuxSession string, tmuxWindow int) string {
	for _, sess := range state.AllSessions(appState) {
		if sess.TmuxSession == tmuxSession && sess.TmuxWindow == tmuxWindow {
			return sess.ID
		}
	}
	return ""
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Hive TUI",
	RunE:  runStart,
}

func runStart(_ *cobra.Command, _ []string) error {
	// Check tmux availability.
	if !tmux.IsAvailable() {
		return fmt.Errorf("tmux is required but not found in PATH.\nInstall tmux: https://github.com/tmux/tmux")
	}

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

	// Reconcile: mark sessions dead if their tmux window is gone.
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

		// Check if we need to attach to a tmux session.
		if fm, ok := finalModel.(interface{ LastAttach() *tui.SessionAttachMsg }); ok {
			if attach := fm.LastAttach(); attach != nil {
				if err := tui.RunAttach(*attach); err != nil {
					fmt.Fprintf(os.Stderr, "tmux attach failed: %v\n", err)
				}
				// Reload config so preferences saved during TUI run (e.g. "don't show again") take effect.
				if reloadedCfg, loadErr := config.Load(); loadErr == nil {
					cfg = config.Migrate(reloadedCfg)
				}
				// Reload state after returning from tmux.
				if reloaded, err := tui.LoadState(); err == nil && reloaded != nil {
					appState.Projects = reloaded
				}
				// Reconcile again: the agent may have exited while attached,
				// closing its window; mark those sessions dead before re-entering TUI.
				reconcileState(&appState)
				// Restore the cursor to the session we just detached from so the
				// TUI re-opens with that session selected rather than the first one.
				appState.ActiveSessionID = findSessionID(&appState, attach.TmuxSession, attach.TmuxWindow)
				continue
			}
		}
		break
	}
	return nil
}

// reconcileState removes sessions whose tmux window no longer exists.
func reconcileState(appState *state.AppState) {
	var deadIDs []string
	for _, sess := range state.AllSessions(appState) {
		windows, err := tmux.ListWindows(sess.TmuxSession)
		if err != nil {
			// Tmux session gone entirely — all windows in it are dead.
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
