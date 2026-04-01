package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/lucascaro/hive/internal/config"
	muxnative "github.com/lucascaro/hive/internal/mux/native"
)

var (
	daemonSockFlag string
	daemonLogFlag  string
)

// muxDaemonCmd is a hidden subcommand that runs the native mux daemon.
// It is launched automatically by "hive start" when the native backend is
// selected and the daemon is not already running.
var muxDaemonCmd = &cobra.Command{
	Use:    "mux-daemon",
	Short:  "Run the native multiplexer daemon (internal use)",
	Hidden: true,
	RunE:   runMuxDaemon,
}

func init() {
	muxDaemonCmd.Flags().StringVar(&daemonSockFlag, "sock", "", "Unix socket path (default: <config-dir>/mux.sock)")
	muxDaemonCmd.Flags().StringVar(&daemonLogFlag, "log", "", "Log file path (default: <config-dir>/mux-daemon.log)")
}

func runMuxDaemon(_ *cobra.Command, _ []string) error {
	sockPath := daemonSockFlag
	if sockPath == "" {
		sockPath = muxnative.SockPath()
	}
	logPath := daemonLogFlag
	if logPath == "" {
		logPath = filepath.Join(config.Dir(), "mux-daemon.log")
	}
	if err := muxnative.RunDaemon(sockPath, logPath); err != nil {
		return fmt.Errorf("mux daemon: %w", err)
	}
	return nil
}
