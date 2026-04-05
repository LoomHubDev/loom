package cli

import (
	"fmt"
	"strings"

	"github.com/constructspace/loom/internal/core"
	"github.com/spf13/cobra"
)

func newCheckpointCmd() *cobra.Command {
	var (
		summary string
		tags    []string
	)

	cmd := &cobra.Command{
		Use:   "checkpoint <title>",
		Short: "Create a named checkpoint",
		Args:  cobra.MinimumNArgs(1),
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

			title := strings.Join(args, " ")

			// Pre-checkpoint hook
			if err := vault.Hooks.Run(core.HookPreCheckpoint, map[string]string{
				"LOOM_TITLE":  title,
				"LOOM_STREAM": stream.Name,
			}); err != nil {
				return fmt.Errorf("pre-checkpoint hook failed: %w", err)
			}

			// Parse tags
			tagMap := make(map[string]string)
			for _, t := range tags {
				parts := strings.SplitN(t, "=", 2)
				if len(parts) == 2 {
					tagMap[parts[0]] = parts[1]
				}
			}

			cp, err := vault.Checkpoints.Create(core.CheckpointInput{
				StreamID: stream.ID,
				Title:    title,
				Summary:  summary,
				Author:   vault.Config.Author.Name,
				Source:   core.SourceManual,
				Tags:     tagMap,
			})
			if err != nil {
				return err
			}

			// Post-checkpoint hook (non-blocking)
			_ = vault.Hooks.Run(core.HookPostCheckpoint, map[string]string{
				"LOOM_CHECKPOINT_ID": cp.ID,
			})

			fmt.Fprintf(cmd.OutOrStdout(), "Checkpoint created: %s\n", cp.ID[:10])
			fmt.Fprintf(cmd.OutOrStdout(), "  Title: %s\n", cp.Title)
			fmt.Fprintf(cmd.OutOrStdout(), "  Seq:   %d\n", cp.Seq)

			if len(cp.Spaces) > 0 {
				var parts []string
				for _, s := range cp.Spaces {
					total := s.Summary.EntitiesCreated + s.Summary.EntitiesModified + s.Summary.EntitiesDeleted
					parts = append(parts, fmt.Sprintf("%s (%d ops)", s.SpaceID, total))
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  Spaces: %s\n", strings.Join(parts, ", "))
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&summary, "summary", "m", "", "Checkpoint summary")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "Key=value tags")

	return cmd
}
