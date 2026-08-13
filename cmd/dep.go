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
	depCmd.Flags().String("cargo", "", `what concretely flows across the edge (branch, schema, doc); "" clears`)
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

	store := TicketStore()
	id, depID := args[0], args[1]

	// Verify dep ticket exists.
	dep, err := store.Get(depID)
	if err != nil {
		return err
	}

	// AddDep is a no-op when the dep is already present, so --cargo also
	// annotates an existing edge. Keyed on Changed, not emptiness: --cargo ""
	// clears the annotation, matching the `edit --set key=` convention.
	cargoFlag, _ := cmd.Flags().GetString("cargo")
	cargo := strings.TrimSpace(cargoFlag)
	cargoSet := cmd.Flags().Changed("cargo")

	// Through Mutate: the edge is added to the deps the ticket already holds, so
	// a plain read-modify-write would drop an edge a concurrent writer added.
	t, err := ticket.Mutate(store, id, func(t *ticket.Ticket) error {
		if err := ticket.AddDep(t, dep.ID); err != nil {
			return err
		}
		if cargoSet {
			return ticket.SetDepCargo(t, dep.ID, cargo)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if cargoSet && cargo == "" {
		fmt.Printf("Added dependency: %s -> %s (cargo cleared)\n", t.ID, dep.ID)
		return nil
	}
	if cargo != "" {
		fmt.Printf("Added dependency: %s -> %s (carries: %s)\n", t.ID, dep.ID, cargo)
		return nil
	}
	fmt.Printf("Added dependency: %s -> %s\n", t.ID, dep.ID)
	return nil
}

func runDepTree(cmd *cobra.Command, args []string) error {
	store := TicketStore()
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
		line := fmt.Sprintf("%s%s [%s] %s", indent, n.ID, status, n.Title)
		// The root has no incoming edge; every other node has one, and an
		// unannotated edge is called out as a grooming candidate.
		if n.Depth > 0 {
			if n.Cargo != "" {
				line += "  ← carries: " + n.Cargo
			} else {
				line += "  ← no cargo"
			}
		}
		fmt.Println(line)
	}
	return nil
}

func runUndep(cmd *cobra.Command, args []string) error {
	store := TicketStore()
	id, depID := args[0], args[1]

	t, err := ticket.Mutate(store, id, func(t *ticket.Ticket) error {
		ticket.RemoveDep(t, depID)
		return nil
	})
	if err != nil {
		return err
	}

	fmt.Printf("Removed dependency: %s -> %s\n", t.ID, depID)
	return nil
}
