package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/constructspace/loom/internal/core"
	lsync "github.com/constructspace/loom/internal/sync"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newHubCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hub",
		Short: "Manage hub remotes",
		Long:  "Add, remove, and list LoomHub remotes for syncing.",
	}

	cmd.AddCommand(
		newHubAddCmd(),
		newHubRemoveCmd(),
		newHubListCmd(),
		newHubAuthCmd(),
		newHubStatusCmd(),
	)

	return cmd
}

func newHubAddCmd() *cobra.Command {
	var setDefault bool

	cmd := &cobra.Command{
		Use:   "add <name> <url>",
		Short: "Add a hub remote",
		Long:  "Add a LoomHub remote. URL format: https://hub.example.com/owner/loom",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			url := args[1]

			v, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer v.Close()

			remotes := core.NewRemoteStore(v.DB)

			// If this is the first remote, make it default
			existing, _ := remotes.List()
			if len(existing) == 0 {
				setDefault = true
			}

			if err := remotes.Add(name, url, setDefault); err != nil {
				return err
			}

			defStr := ""
			if setDefault {
				defStr = " (default)"
			}
			fmt.Printf("Added remote %q → %s%s\n", name, url, defStr)
			return nil
		},
	}

	cmd.Flags().BoolVar(&setDefault, "default", false, "Set as the default remote")

	return cmd
}

func newHubRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a hub remote",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer v.Close()

			remotes := core.NewRemoteStore(v.DB)
			if err := remotes.Remove(args[0]); err != nil {
				return err
			}

			fmt.Printf("Removed remote %q\n", args[0])
			return nil
		},
	}
}

func newHubListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List hub remotes",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer v.Close()

			remotes := core.NewRemoteStore(v.DB)
			list, err := remotes.List()
			if err != nil {
				return err
			}

			if len(list) == 0 {
				fmt.Println("No remotes configured. Use 'loom hub add <name> <url>' to add one.")
				return nil
			}

			for _, r := range list {
				def := " "
				if r.IsDefault {
					def = "*"
				}
				fmt.Printf("%s %-12s %s\n", def, r.Name, r.URL)
				if r.PushSeq > 0 || r.PullSeq > 0 {
					fmt.Printf("  push_seq: %d  pull_seq: %d\n", r.PushSeq, r.PullSeq)
				}
			}
			return nil
		},
	}
}

func newHubAuthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "auth [remote]",
		Short: "Authenticate with a hub remote",
		Long:  "Log in to a LoomHub instance and store the token locally.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer v.Close()

			remotes := core.NewRemoteStore(v.DB)

			var remote *core.Remote
			if len(args) > 0 {
				remote, err = remotes.Get(args[0])
			} else {
				remote, err = remotes.Default()
			}
			if err != nil {
				return err
			}

			reader := bufio.NewReader(os.Stdin)
			fmt.Printf("Authenticating with %s (%s)\n", remote.Name, remote.URL)

			fmt.Print("Username: ")
			username, _ := reader.ReadString('\n')
			username = strings.TrimSpace(username)

			fmt.Print("Password: ")
			passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println()
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			password := string(passwordBytes)

			client := lsync.NewClient(remote.URL, "")
			token, err := client.Login(username, password)
			if err != nil {
				return fmt.Errorf("login failed: %w", err)
			}

			if err := remotes.SetAuthToken(remote.Name, token); err != nil {
				return fmt.Errorf("store token: %w", err)
			}

			fmt.Printf("Authenticated as %s\n", username)
			return nil
		},
	}
}

func newHubStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [remote]",
		Short: "Show sync status for hub remotes",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer v.Close()

			remotes := core.NewRemoteStore(v.DB)

			var list []core.Remote
			if len(args) > 0 {
				r, err := remotes.Get(args[0])
				if err != nil {
					return err
				}
				list = []core.Remote{*r}
			} else {
				list, err = remotes.List()
				if err != nil {
					return err
				}
			}

			out := cmd.OutOrStdout()

			if len(list) == 0 {
				fmt.Fprintln(out, "No remotes configured.")
				return nil
			}

			streams, err := v.Streams.List()
			if err != nil {
				return fmt.Errorf("list streams: %w", err)
			}

			for _, r := range list {
				fmt.Fprintf(out, "%s: %s\n", r.Name, r.URL)
				for _, s := range streams {
					pushSeq, _ := remotes.GetStreamPushSeq(r.Name, s.ID)
					pullSeq, _ := remotes.GetStreamPullSeq(r.Name, s.ID)
					ahead := s.HeadSeq - pushSeq
					behind := pullSeq

					lastPushStr := "never"
					if r.LastPush != "" {
						if t, err := time.Parse("2006-01-02 15:04:05", r.LastPush); err == nil {
							lastPushStr = formatAgo(time.Since(t))
						} else if t, err := time.Parse(time.RFC3339, r.LastPush); err == nil {
							lastPushStr = formatAgo(time.Since(t))
						}
					}

					fmt.Fprintf(out, "  %-10s %d ahead, %d behind (last push: %s)\n",
						s.Name+":", ahead, behind, lastPushStr)
				}
			}
			return nil
		},
	}
}

func formatAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case d < 24*time.Hour:
		hrs := int(d.Hours())
		if hrs == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hrs)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}
