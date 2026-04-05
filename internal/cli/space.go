package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
	"github.com/constructspace/loom/internal/core"
	"github.com/spf13/cobra"
)

func newSpaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "space",
		Short: "Manage project spaces",
	}

	cmd.AddCommand(
		newSpaceListCmd(),
		newSpaceAddCmd(),
		newSpaceRemoveCmd(),
	)

	return cmd
}

func newSpaceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all spaces",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			vault, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer vault.Close()

			counts, _ := vault.EntityCount()

			fmt.Fprintln(cmd.OutOrStdout(), "Spaces:")

			// Sort for deterministic output
			ids := make([]string, 0, len(vault.Config.Spaces))
			for id := range vault.Config.Spaces {
				ids = append(ids, id)
			}
			sort.Strings(ids)

			for _, id := range ids {
				sc := vault.Config.Spaces[id]
				count := counts[id]
				fmt.Fprintf(cmd.OutOrStdout(), "  %-12s %-12s %-12s %d entities\n",
					id, sc.Adapter, sc.Path, count)
			}

			return nil
		},
	}
}

func newSpaceAddCmd() *cobra.Command {
	var adapter string

	cmd := &cobra.Command{
		Use:   "add <id> <path>",
		Short: "Add a new space",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			spacePath := args[1]

			vault, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer vault.Close()

			if _, exists := vault.Config.Spaces[id]; exists {
				return fmt.Errorf("space %q already exists", id)
			}

			// Check path exists on disk
			absSpacePath := filepath.Join(vault.ProjectPath, spacePath)
			if _, err := os.Stat(absSpacePath); os.IsNotExist(err) {
				return fmt.Errorf("path %q does not exist", spacePath)
			}

			if vault.Config.Spaces == nil {
				vault.Config.Spaces = make(map[string]core.SpaceConfig)
			}
			vault.Config.Spaces[id] = core.SpaceConfig{
				Adapter: adapter,
				Path:    spacePath,
			}

			configPath := filepath.Join(vault.LoomPath, "config.toml")
			f, err := os.Create(configPath)
			if err != nil {
				return err
			}
			defer f.Close()
			if err := toml.NewEncoder(f).Encode(vault.Config); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Added space %s at %s\n", id, spacePath)
			return nil
		},
	}

	cmd.Flags().StringVar(&adapter, "adapter", "filesystem", "Adapter type (filesystem, git, design)")

	return cmd
}

func newSpaceRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a space",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			vault, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer vault.Close()

			if _, exists := vault.Config.Spaces[id]; !exists {
				return fmt.Errorf("space %q does not exist", id)
			}

			if len(vault.Config.Spaces) <= 1 {
				return fmt.Errorf("cannot remove the last space")
			}

			delete(vault.Config.Spaces, id)

			configPath := filepath.Join(vault.LoomPath, "config.toml")
			f, err := os.Create(configPath)
			if err != nil {
				return err
			}
			defer f.Close()
			if err := toml.NewEncoder(f).Encode(vault.Config); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed space %s\n", id)
			return nil
		},
	}
}
