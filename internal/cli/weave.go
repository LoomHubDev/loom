package cli

import (
	"fmt"

	"github.com/constructspace/loom/internal/core"
	"github.com/spf13/cobra"
)

func newWeaveCmd() *cobra.Command {
	var dryRun bool
	var strategy string

	cmd := &cobra.Command{
		Use:   "weave <stream-name>",
		Short: "Weave a stream into the current stream",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer v.Close()

			target, err := v.ActiveStream()
			if err != nil {
				return fmt.Errorf("get active stream: %w", err)
			}

			parsedStrategy, err := parseWeaveStrategy(strategy)
			if err != nil {
				return err
			}

			engine := core.NewWeaveEngine(v.Streams, v.OpReader, v.OpWriter, v.Checkpoints, v.Store)
			fmt.Fprintf(cmd.OutOrStdout(), "Weaving %s into %s...\n", args[0], target.Name)

			result, err := engine.Weave(core.WeaveOptions{
				SourceName: args[0],
				TargetName: target.Name,
				Strategy:   parsedStrategy,
				DryRun:     dryRun,
				Author:     v.Config.Author.Name,
			})
			if err != nil {
				return err
			}

			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Dry run: would apply %d operations from %d source entities.\n", result.Applied, result.SourceEntities)
				if result.ConflictCount > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "Conflicts: %d\n", result.ConflictCount)
				}
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Weave complete. %d operations applied.\n", result.Applied)
			if result.Checkpoint != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Checkpoint: %q\n", result.Checkpoint.Title)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be woven without writing changes")
	cmd.Flags().StringVar(&strategy, "strategy", string(core.WeaveStrategyAuto), "Weave strategy: auto, ours, theirs")

	return cmd
}

func parseWeaveStrategy(raw string) (core.WeaveStrategy, error) {
	switch raw {
	case "", string(core.WeaveStrategyAuto), "auto+llm":
		return core.WeaveStrategyAuto, nil
	case string(core.WeaveStrategyOurs):
		return core.WeaveStrategyOurs, nil
	case string(core.WeaveStrategyTheirs):
		return core.WeaveStrategyTheirs, nil
	default:
		return "", fmt.Errorf("invalid weave strategy %q", raw)
	}
}
