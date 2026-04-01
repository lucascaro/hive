package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui"
)

var attachCmd = &cobra.Command{
	Use:   "attach [session-id-or-name]",
	Short: "Attach directly to a session by ID or name (headless)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runAttach,
}

func runAttach(_ *cobra.Command, args []string) error {
	if err := config.Ensure(); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := initMuxBackend(config.Migrate(cfg)); err != nil {
		return err
	}

	projects, err := tui.LoadState()
	if err != nil || len(projects) == 0 {
		return fmt.Errorf("no saved sessions found")
	}

	appState := state.AppState{Projects: projects}
	sessions := state.AllSessions(&appState)

	if len(sessions) == 0 {
		return fmt.Errorf("no sessions found")
	}

	var target *state.Session
	if len(args) == 0 {
		// Default to first non-dead session.
		for _, s := range sessions {
			if s.Status != state.StatusDead {
				target = s
				break
			}
		}
	} else {
		query := args[0]
		for _, s := range sessions {
			if s.ID == query || s.Title == query {
				target = s
				break
			}
		}
	}

	if target == nil {
		return fmt.Errorf("session not found")
	}

	muxTarget := mux.Target(target.TmuxSession, target.TmuxWindow)
	fmt.Fprintf(os.Stderr, "Attaching to %s (%s)…\n", target.Title, muxTarget)
	return mux.Attach(muxTarget)
}
