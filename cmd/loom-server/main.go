package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/constructspace/loom/internal/server"
	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "loom-server",
		Short: "LoomHub server",
	}
	root.AddCommand(serveCmd())
	root.AddCommand(userCmd())
	return root
}

func serveCmd() *cobra.Command {
	var port int
	var dataDir string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the LoomHub HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			hub, err := server.NewHubServer(dataDir)
			if err != nil {
				return fmt.Errorf("create hub server: %w", err)
			}
			defer hub.Close()

			addr := fmt.Sprintf(":%d", port)
			fmt.Printf("LoomHub server listening on %s\n", addr)

			// Handle SIGINT / SIGTERM for graceful shutdown.
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

			errCh := make(chan error, 1)
			go func() {
				if err := http.ListenAndServe(addr, hub.Handler()); err != nil {
					errCh <- err
				}
			}()

			select {
			case sig := <-quit:
				fmt.Printf("\nReceived %s, shutting down\n", sig)
				return nil
			case err := <-errCh:
				return err
			}
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 3000, "Port to listen on")
	cmd.Flags().StringVar(&dataDir, "data", "/var/loom", "Data directory")
	return cmd
}

func userCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage users",
	}
	cmd.AddCommand(userAddCmd())
	return cmd
}

func userAddCmd() *cobra.Command {
	var password string
	var dataDir string

	cmd := &cobra.Command{
		Use:   "add <username>",
		Short: "Create a new user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]

			hub, err := server.NewHubServer(dataDir)
			if err != nil {
				return fmt.Errorf("create hub server: %w", err)
			}
			defer hub.Close()

			if err := hub.Users().CreateUser(username, password); err != nil {
				return fmt.Errorf("create user: %w", err)
			}

			fmt.Printf("User %s created\n", username)
			return nil
		},
	}

	cmd.Flags().StringVar(&password, "password", "", "Password for the new user (required)")
	cmd.Flags().StringVar(&dataDir, "data", "/var/loom", "Data directory")
	_ = cmd.MarkFlagRequired("password")
	return cmd
}
