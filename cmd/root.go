package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "hive",
	Short: "Multi-agent terminal session manager",
	Long:  `Hive manages Claude, Codex, Gemini, Copilot, and other AI coding agents across multiple projects and sessions.`,
	RunE:  runStart,
}

// EmbeddedChangelog holds the contents of CHANGELOG.md, set by main.go via go:embed.
var EmbeddedChangelog string

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(attachCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(muxDaemonCmd)
}
