package cli

import (
	"fmt"
	"strings"

	"github.com/constructspace/loom/internal/core"
	"github.com/constructspace/loom/internal/diff"
	"github.com/spf13/cobra"
)

func newShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <checkpoint-id>",
		Short: "Show checkpoint details and diff",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vault, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer vault.Close()

			cp, err := vault.Checkpoints.Get(args[0])
			if err != nil {
				return fmt.Errorf("get checkpoint: %w", err)
			}

			out := cmd.OutOrStdout()

			fmt.Fprintf(out, "checkpoint  %s\n", cp.ID)
			fmt.Fprintf(out, "title       %s\n", cp.Title)
			fmt.Fprintf(out, "author      %s\n", cp.Author)
			fmt.Fprintf(out, "timestamp   %s\n", cp.Timestamp)
			fmt.Fprintf(out, "source      %s\n", string(cp.Source))
			if cp.Summary != "" {
				fmt.Fprintf(out, "summary     %s\n", cp.Summary)
			}
			if len(cp.Tags) > 0 {
				var tagParts []string
				for k, v := range cp.Tags {
					tagParts = append(tagParts, fmt.Sprintf("%s=%s", k, v))
				}
				fmt.Fprintf(out, "tags        %s\n", strings.Join(tagParts, ", "))
			}
			if len(cp.Spaces) > 0 {
				fmt.Fprintf(out, "spaces\n")
				for _, s := range cp.Spaces {
					fmt.Fprintf(out, "  %s  +%d ~%d -%d\n",
						s.SpaceID,
						s.Summary.EntitiesCreated,
						s.Summary.EntitiesModified,
						s.Summary.EntitiesDeleted,
					)
				}
			}

			// Show diff from parent if available.
			if cp.ParentID != "" {
				fmt.Fprintln(out)
				engine := diff.NewDiffEngine(vault.DB, vault.OpReader, vault.Store)
				fromRef := diff.ParseRef(cp.ParentID)
				toRef := diff.DiffRef{Type: "seq", Value: fmt.Sprintf("%d", cp.Seq)}
				result, err := engine.Diff(fromRef, toRef, diff.DiffOptions{Context: 3})
				if err != nil {
					return fmt.Errorf("diff: %w", err)
				}
				fmt.Fprint(out, diff.FormatTerminal(result, true))
			}

			return nil
		},
	}

	return cmd
}
