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
	Long: "Start MCP server on stdio. Serves all projects from the central ticket store.\n\n" +
		"Set TK_STORE_ROOT to an absolute path to serve a throwaway store instead: tk\n" +
		"resolves every store and config path under that root, needs no ~/.ticket/config.yaml,\n" +
		"and runs no sync or journal watch (tk sync, tk watch and tk recompute refuse to run at\n" +
		"all while it is set). It does not move verify_allow or spawn_command, both always read\n" +
		"from ~/.ticket/config.yaml, and it does not cover a ticket_create `repo` argument (a\n" +
		"caller-supplied path resolves outside the root by construction) or code a verify command\n" +
		"runs.",
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

		if err := startBackgroundLoops(ctx); err != nil {
			return err
		}

		store, centralRoot, err := serveStore()
		if err != nil {
			return err
		}

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

// serveStore returns the store serve exposes over MCP and the root it lives
// under — the central store, or the TK_STORE_ROOT override.
func serveStore() (*ticket.MultiStore, string, error) {
	// One resolution, above the store: consulting the override again on the
	// error path would read the same source twice with two dispositions for its
	// error. Only whether it is set is needed — a set-but-unusable one is
	// already the error CentralStoreRoot returns below.
	_, isolated, _ := project.StoreRootOverride()

	root, err := project.CentralStoreRoot()
	if err != nil {
		// An override that failed is not a missing configuration: wrapping it
		// would point the reader at `tk init` when the fix is the variable.
		if isolated {
			return nil, "", err
		}
		return nil, "", fmt.Errorf("serve requires a configured central store: %w", err)
	}
	return ticket.NewMultiStore(filepath.Join(root, "tickets")), root, nil
}

// startBackgroundLoops starts the store sync and commit-journal watch
// goroutines. Both write to the store — sync commits and pushes the tree, watch
// auto-closes tickets — so neither runs under TK_STORE_ROOT, where the store is
// a sandbox nobody wants published. syncLoop happens not to start against a
// throwaway root today only because findGitRoot fails outside a git tree; a temp
// dir created inside one would find a root and sync it.
func startBackgroundLoops(ctx context.Context) error {
	_, isolated, err := project.StoreRootOverride()
	if err != nil {
		return err
	}
	if isolated {
		// Otherwise an isolated serve is indistinguishable on stderr from one
		// that is syncing and journalling the real store.
		log.Printf("serve: %s set — sync and journal watch disabled", project.StoreRootEnv)
		return nil
	}

	if storeRoot, err := project.CentralStoreRoot(); err == nil {
		// findGitRoot is only the gate — whether the store sits in a git repo at
		// all. syncLoop operates on the store root itself; see syncCentralStore.
		if _, err := findGitRoot(storeRoot); err == nil {
			go syncLoop(ctx, storeRoot, syncInterval())
		}
	}
	go watchLoop(ctx, syncInterval())
	return nil
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
				store, err := watchStoreFor(cfg, name)
				if err != nil {
					log.Printf("watch: %s: resolve store: %v", name, err)
					continue
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
