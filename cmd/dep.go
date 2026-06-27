package cmd

import (
	"fmt"
	"strings"

	"github.com/EnderRealm/ticket/v7/pkg/ticket"
	"github.com/spf13/cobra"
)

var depCmd = &cobra.Command{
	Use:   "dep <id> <dep-id>",
	Short: "Add a dependency",
	Long:  "Add a dependency, or use 'dep tree <id>'.",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runDep,
}

var depTreeCmd = &cobra.Command{
	Use:   "tree <id>",
	Short: "Show dependency tree",
	Args:  cobra.ExactArgs(1),
	RunE:  runDepTree,
}

var undepCmd = &cobra.Command{
	Use:   "undep <id> <dep-id>",
	Short: "Remove a dependency",
	Args:  cobra.ExactArgs(2),
	RunE:  runUndep,
}

func init() {
	depTreeCmd.Flags().Bool("full", false, "show full tree without dedup")
	depCmd.AddCommand(depTreeCmd)

	rootCmd.AddCommand(depCmd)
	rootCmd.AddCommand(undepCmd)
}

func runDep(cmd *cobra.Command, args []string) error {
	// If not a subcommand, treat as "dep <id> <dep-id>".
	if len(args) < 2 {
		return fmt.Errorf("usage: tk dep <id> <dep-id>")
	}

	store := ticket.NewFileStore(TicketsDir())
	id, depID := args[0], args[1]

	t, err := store.Get(id)
	if err != nil {
		return err
	}

	// Verify dep ticket exists.
	dep, err := store.Get(depID)
	if err != nil {
		return err
	}

	if err := ticket.AddDep(t, dep.ID); err != nil {
		return err
	}

	if err := store.Update(t); err != nil {
		return err
	}

	fmt.Printf("Added dependency: %s -> %s\n", t.ID, dep.ID)
	return nil
}

func runDepTree(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	full, _ := cmd.Flags().GetBool("full")

	nodes, err := ticket.DepTree(store, args[0], full)
	if err != nil {
		return err
	}

	for _, n := range nodes {
		indent := strings.Repeat("  ", n.Depth)
		status := string(n.Status)
		if status == "" {
			status = "?"
		}
		fmt.Printf("%s%s [%s] %s\n", indent, n.ID, status, n.Title)
	}
	return nil
}

func runUndep(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	id, depID := args[0], args[1]

	t, err := store.Get(id)
	if err != nil {
		return err
	}

	ticket.RemoveDep(t, depID)

	if err := store.Update(t); err != nil {
		return err
	}

	fmt.Printf("Removed dependency: %s -> %s\n", t.ID, depID)
	return nil
}
