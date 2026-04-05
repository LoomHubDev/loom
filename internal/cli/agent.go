package cli

import (
	"fmt"

	"github.com/constructspace/loom/internal/agent"
	loom "github.com/constructspace/loom/pkg/loom"
	"github.com/spf13/cobra"
)

func newAgentServerCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "agent-server",
		Short: "Start the agent HTTP API server",
		Long:  "Start an HTTP API server that AI agents can use to interact with Loom.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := loom.Open(projectDir)
			if err != nil {
				return fmt.Errorf("open project: %w", err)
			}
			defer client.Close()

			srv := agent.NewAgentServer(client, port)
			return srv.StartWithSignals()
		},
	}

	cmd.Flags().IntVar(&port, "port", 7890, "Port to listen on")

	return cmd
}
