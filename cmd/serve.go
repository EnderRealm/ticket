package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/EnderRealm/ticket/v7/internal/mcp"
	"github.com/EnderRealm/ticket/v7/internal/project"
	"github.com/EnderRealm/ticket/v7/pkg/journal"
	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start MCP server on stdio",
	Long:  "Start MCP server on stdio. Serves all projects from the central ticket store.",
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

		// Start watch goroutine for commit journal
		go watchLoop(ctx, syncInterval())

		root, err := project.CentralStoreRoot()
		if err != nil {
			return fmt.Errorf("serve requires a configured central store: %w", err)
		}
		centralRoot := root
		store := ticket.NewMultiStore(filepath.Join(root, "tickets"))

		// Resolve default project from CWD — scopes tools when
		// no explicit project param is provided. Empty string
		// (not in a known repo) means all projects.
		cfg, _ := project.Load()
		cwd, _ := os.Getwd()
		defaultProject, _ := project.ResolveName(cfg, cwd, "")

		server := mcp.NewServer(store, defaultProject, centralRoot)
		return server.Run(ctx, &gomcp.StdioTransport{})
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

// watchLoop runs journal watch cycles on a ticker until ctx is cancelled.
func watchLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cfg, err := project.Load()
			if err != nil {
				log.Printf("watch: load config: %v", err)
				continue
			}
			for name, entry := range cfg.Projects {
				if !entry.AutoLink && !entry.AutoClose {
					continue
				}
				var store ticket.Store
				if entry.Store == "central" {
					dir, err := project.CentralProjectDir(name)
					if err != nil {
						log.Printf("watch: %s: resolve store: %v", name, err)
						continue
					}
					// Project-scoped: auto-close writes go through the same
					// propagation hooks as MCP writes and must resolve the
					// namespaced parent/dep IDs the central store records.
					store = ticket.NewProjectFileStore(dir, name)
				} else if entry.Path != "" {
					store = ticket.NewFileStore(filepath.Join(entry.Path, ".tickets"))
				}
				result, err := journal.RunWatchCycle(name, entry, store)
				if err != nil {
					log.Printf("watch: %s: %v", name, err)
					continue
				}
				if result.Appended > 0 || result.Closed > 0 {
					log.Printf("watch: %s: appended %d, closed %d", name, result.Appended, result.Closed)
				}
				for _, w := range result.Warnings {
					log.Printf("watch: %s: %s", name, w)
				}
			}
		}
	}
}
