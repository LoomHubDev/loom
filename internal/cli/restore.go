package cli

import (
	"fmt"

	"github.com/constructspace/loom/internal/core"
	"github.com/spf13/cobra"
)

func newRestoreCmd() *cobra.Command {
	var (
		space  string
		entity string
	)

	cmd := &cobra.Command{
		Use:   "restore <checkpoint-id>",
		Short: "Restore project state to a previous checkpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vault, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer vault.Close()

			engine := core.NewRestoreEngine(vault)

			scope := core.RestoreScope{
				SpaceID:  space,
				EntityID: entity,
			}
			if space == "" && entity == "" {
				scope.Full = true
			}

			result, err := engine.Restore(args[0], scope)
			if err != nil {
				return fmt.Errorf("restore: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "restored to checkpoint  %s\n", args[0])
			fmt.Fprintf(out, "restore checkpoint      %s\n", result.ID)
			fmt.Fprintf(out, "title                   %s\n", result.Title)

			return nil
		},
	}

	cmd.Flags().StringVar(&space, "space", "", "Restore only this space")
	cmd.Flags().StringVar(&entity, "entity", "", "Restore only this entity")

	return cmd
}
