package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

const Version = "0.2.1"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Println("hive", Version)
	},
}
