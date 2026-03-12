package cli

import (
	"github.com/spf13/cobra"
)

var projectDir string

// NewRootCmd creates the root loom command.
func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "loom",
		Short: "Versioning for 2026+",
		Long:  "Loom is a continuous, intelligent, multi-space versioning system.",
		SilenceUsage: true,
	}

	rootCmd.PersistentFlags().StringVarP(&projectDir, "project", "p", ".", "Project directory")

	rootCmd.AddCommand(
		newInitCmd(),
		newStatusCmd(),
		newCheckpointCmd(),
		newLogCmd(),
		newStreamCmd(),
		newHubCmd(),
		newSendCmd(),
		newReceiveCmd(),
	)

	return rootCmd
}
