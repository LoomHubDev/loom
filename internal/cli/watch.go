package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/constructspace/loom/internal/core"
	"github.com/constructspace/loom/internal/watch"
	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	var debounceMs int
	var noAutoCheckpoint bool
	var daemon bool

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch for file changes and auto-version",
		Long:  "Start a file watcher that automatically records operations for every file change.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Daemon mode: re-launch as a background process.
			if daemon && os.Getenv("LOOM_WATCH_DAEMON") != "1" {
				filteredArgs := filterArg(os.Args[1:], "--daemon")
				child := exec.Command(os.Args[0], filteredArgs...)
				child.Env = append(os.Environ(), "LOOM_WATCH_DAEMON=1")
				child.Stdout = nil
				child.Stderr = nil
				if err := child.Start(); err != nil {
					return fmt.Errorf("start daemon: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Watcher daemon started (PID %d)\n", child.Process.Pid)
				return nil
			}

			vault, err := core.OpenVault(projectDir)
			if err != nil {
				return err
			}
			defer vault.Close()

			stream, err := vault.ActiveStream()
			if err != nil {
				return err
			}

			config := watch.WatcherConfig{
				DebounceMs:     debounceMs,
				AutoCheckpoint: !noAutoCheckpoint,
			}

			w, err := watch.NewWatcher(vault, config)
			if err != nil {
				return fmt.Errorf("create watcher: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Watching %s (stream: %s)\n", vault.Config.Project.Name, stream.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Spaces: ")
			for id := range vault.Config.Spaces {
				fmt.Fprintf(cmd.OutOrStdout(), "%s ", id)
			}
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "Press Ctrl+C to stop.")

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				fmt.Fprintln(cmd.OutOrStdout(), "\nStopping watcher...")
				cancel()
			}()

			return w.Start(ctx)
		},
	}

	cmd.Flags().IntVar(&debounceMs, "debounce", 0, "Debounce window in milliseconds (default: from config)")
	cmd.Flags().BoolVar(&noAutoCheckpoint, "no-auto-checkpoint", false, "Disable auto-checkpointing")
	cmd.Flags().BoolVar(&daemon, "daemon", false, "Run in background")

	return cmd
}

// filterArg returns args with the given flag removed.
func filterArg(args []string, flag string) []string {
	var filtered []string
	for _, a := range args {
		if a != flag {
			filtered = append(filtered, a)
		}
	}
	return filtered
}
