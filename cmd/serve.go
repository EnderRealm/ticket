package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/EnderRealm/ticket/internal/mcp"
	"github.com/EnderRealm/ticket/internal/project"
	"github.com/EnderRealm/ticket/pkg/ticket"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start MCP server on stdio",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Handle shutdown signals
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			cancel()
		}()

		// Start sync goroutine if central store is configured
		if storeRoot, err := project.CentralStoreRoot(); err == nil {
			if gitRoot, err := findGitRoot(storeRoot); err == nil {
				go syncLoop(ctx, gitRoot, syncInterval())
			}
		}

		store := ticket.NewFileStore(TicketsDir())
		server := mcp.NewServer(store)
		return server.Run(ctx, &gomcp.StdioTransport{})
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
