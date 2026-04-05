package cli

import (
	"fmt"
	"strconv"

	"github.com/constructspace/loom/internal/core"
	"github.com/constructspace/loom/internal/diff"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	var (
		space   string
		entity  string
		format  string
		summary bool
		context int
	)

	cmd := &cobra.Command{
		Use:   "diff [from] [to]",
		Short: "Show changes between two points",
		Long: `Show changes between two points in the oplog.

With no arguments, diffs from the last checkpoint to HEAD.
With one argument, diffs from that ref to HEAD.
With two arguments, diffs between the two refs.`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			vault, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer vault.Close()

			stream, err := vault.ActiveStream()
			if err != nil {
				return err
			}

			// Determine from/to refs.
			var fromRef, toRef diff.DiffRef

			toRef = diff.ParseRef("HEAD")

			switch len(args) {
			case 0:
				// Default from: last checkpoint seq, or seq 0 if none.
				latestSeq := vault.Checkpoints.LatestSeq(stream.ID)
				if latestSeq <= 0 {
					fromRef = diff.DiffRef{Type: "seq", Value: "0"}
				} else {
					fromRef = diff.DiffRef{Type: "seq", Value: strconv.FormatInt(latestSeq, 10)}
				}
			case 1:
				fromRef = diff.ParseRef(args[0])
			case 2:
				fromRef = diff.ParseRef(args[0])
				toRef = diff.ParseRef(args[1])
			}

			engine := diff.NewDiffEngine(vault.DB, vault.OpReader, vault.Store)
			opts := diff.DiffOptions{
				SpaceID:  space,
				EntityID: entity,
				Format:   format,
				Context:  context,
				Summary:  summary,
			}

			result, err := engine.Diff(fromRef, toRef, opts)
			if err != nil {
				return fmt.Errorf("diff: %w", err)
			}

			var out string
			switch format {
			case "json":
				out = diff.FormatJSON(result)
			default:
				out = diff.FormatTerminal(result, true)
			}

			fmt.Fprint(cmd.OutOrStdout(), out)
			return nil
		},
	}

	cmd.Flags().StringVar(&space, "space", "", "Filter to one space")
	cmd.Flags().StringVar(&entity, "entity", "", "Filter to one entity")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	cmd.Flags().BoolVar(&summary, "summary", false, "Show summary only")
	cmd.Flags().IntVarP(&context, "context", "C", 3, "Lines of context")

	return cmd
}
