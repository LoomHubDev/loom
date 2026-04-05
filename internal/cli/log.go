package cli

import (
	"fmt"
	"strings"

	"github.com/constructspace/loom/internal/core"
	"github.com/spf13/cobra"
)

func newLogCmd() *cobra.Command {
	var (
		limit  int
		source string
		space  string
		search string
	)

	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show checkpoint history",
		RunE: func(cmd *cobra.Command, args []string) error {
			vault, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer vault.Close()

			var checkpoints []core.Checkpoint

			if search != "" {
				searchLimit := 0
				if source == "" && space == "" {
					searchLimit = limit
				}
				checkpoints, err = vault.Checkpoints.Search(search, searchLimit)
			} else {
				stream, serr := vault.ActiveStream()
				if serr != nil {
					return serr
				}
				checkpoints, err = vault.Checkpoints.List(stream.ID, limit)
			}
			if err != nil {
				return err
			}

			if len(checkpoints) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No checkpoints yet. Create one with: loom checkpoint <title>")
				return nil
			}

			rendered := 0
			for _, cp := range checkpoints {
				// Filter by source
				if source != "" && string(cp.Source) != source {
					continue
				}

				// Filter by space
				if space != "" {
					hasSpace := false
					for _, s := range cp.Spaces {
						if s.SpaceID == space {
							hasSpace = true
							break
						}
					}
					if !hasSpace {
						continue
					}
				}

				timeAgo := formatTimeAgo(cp.Timestamp)

				fmt.Fprintf(cmd.OutOrStdout(), "%s  %-35s %-8s %s\n",
					cp.ID[:10], cp.Title, string(cp.Source), timeAgo)

				// Show space summaries
				if len(cp.Spaces) > 0 {
					var parts []string
					for _, s := range cp.Spaces {
						total := s.Summary.EntitiesCreated + s.Summary.EntitiesModified + s.Summary.EntitiesDeleted
						if total > 0 {
							detail := ""
							if s.Summary.EntitiesCreated > 0 {
								detail += fmt.Sprintf("%d created", s.Summary.EntitiesCreated)
							}
							if s.Summary.EntitiesModified > 0 {
								if detail != "" {
									detail += ", "
								}
								detail += fmt.Sprintf("%d modified", s.Summary.EntitiesModified)
							}
							if s.Summary.EntitiesDeleted > 0 {
								if detail != "" {
									detail += ", "
								}
								detail += fmt.Sprintf("%d deleted", s.Summary.EntitiesDeleted)
							}
							parts = append(parts, fmt.Sprintf("%s: %s", s.SpaceID, detail))
						}
					}
					if len(parts) > 0 {
						fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", strings.Join(parts, " | "))
					}
				}

				fmt.Fprintln(cmd.OutOrStdout())
				rendered++
				if rendered >= limit {
					break
				}
			}

			return nil
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Max checkpoints")
	cmd.Flags().StringVar(&source, "source", "", "Filter by source (manual, auto, agent)")
	cmd.Flags().StringVar(&space, "space", "", "Filter by space")
	cmd.Flags().StringVar(&search, "search", "", "Full-text search")

	return cmd
}
