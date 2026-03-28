package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/EnderRealm/ticket/internal/mcp"
	"github.com/EnderRealm/ticket/internal/project"
	"github.com/EnderRealm/ticket/pkg/ticket"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var centralFlag bool

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start MCP server on stdio",
	Long:  "Start MCP server on stdio. Use --central to serve all projects from the central ticket store.",
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

		var store ticket.Store
		var defaultProject string
		var centralRoot string
		if centralFlag {
			root, err := project.CentralStoreRoot()
			if err != nil {
				return fmt.Errorf("--central requires a configured central store: %w", err)
			}
			centralRoot = root
			store = ticket.NewMultiStore(filepath.Join(root, "tickets"))

			// Resolve default project from CWD — scopes tools when
			// no explicit project param is provided. Empty string
			// (not in a known repo) means all projects.
			cfg, _ := project.Load()
			cwd, _ := os.Getwd()
			defaultProject, _ = project.ResolveName(cfg, cwd, "")
		} else {
			store = ticket.NewFileStore(TicketsDir())
		}

		server := mcp.NewServer(store, defaultProject, centralRoot)
		return server.Run(ctx, &gomcp.StdioTransport{})
	},
}

func init() {
	serveCmd.Flags().BoolVar(&centralFlag, "central", false, "serve all projects from the central ticket store")
	rootCmd.AddCommand(serveCmd)
}
