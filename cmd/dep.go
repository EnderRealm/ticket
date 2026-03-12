package cmd

import (
	"fmt"
	"strings"

	"github.com/EnderRealm/ticket/pkg/ticket"
	"github.com/spf13/cobra"
)

var depCmd = &cobra.Command{
	Use:   "dep <id> <dep-id>",
	Short: "Add a dependency",
	Long:  "Add a dependency, or use 'dep tree <id>' or 'dep cycle'.",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runDep,
}

var depTreeCmd = &cobra.Command{
	Use:   "tree <id>",
	Short: "Show dependency tree",
	Args:  cobra.ExactArgs(1),
	RunE:  runDepTree,
}

var depCycleCmd = &cobra.Command{
	Use:   "cycle",
	Short: "Find dependency cycles",
	RunE:  runDepCycle,
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
	depCmd.AddCommand(depCycleCmd)

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
		stage := string(n.Stage)
		if stage == "" {
			stage = "?"
		}
		fmt.Printf("%s%s [%s] %s\n", indent, n.ID, stage, n.Title)
	}
	return nil
}

func runDepCycle(cmd *cobra.Command, args []string) error {
	store := ticket.NewFileStore(TicketsDir())
	cycles, err := ticket.FindCycles(store)
	if err != nil {
		return err
	}

	if len(cycles) == 0 {
		fmt.Println("No dependency cycles found")
		return nil
	}

	allTickets, _ := store.List()
	byID := map[string]*ticket.Ticket{}
	for _, t := range allTickets {
		byID[t.ID] = t
	}

	for i, c := range cycles {
		if i > 0 {
			fmt.Println()
		}
		fmt.Printf("Cycle %d: %s\n", i+1, c)
		for _, id := range c.IDs {
			if t, ok := byID[id]; ok {
				fmt.Printf("  %-8s [%s] %s\n", id, t.Stage, t.Title)
			}
		}
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
