package cmd

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/EnderRealm/ticket/v8/internal/project"
	"github.com/EnderRealm/ticket/v8/pkg/ticket"
	"github.com/spf13/cobra"
)

var frontierCmd = &cobra.Command{
	Use:   "frontier",
	Short: "List ready tickets whose dependencies are all done or closed",
	Long:  "List ready tickets whose dependencies are all done or closed — the parallel-safe set to start next.",
	Args:  cobra.NoArgs,
	RunE:  runFrontier,
}

func init() {
	frontierCmd.Flags().String("parent", "", "only the children of this epic (qualified project/id, or bare with --project)")
	rootCmd.AddCommand(frontierCmd)
}

// runFrontier computes the frontier over the whole store and narrows it
// afterwards: --parent keeps the members the graph places under the epic, in
// every namespace, and --project (the persistent selector) keeps one
// namespace's rows. Neither changes what is ready — a project filter narrows
// the output, never the lookup.
func runFrontier(cmd *cobra.Command, args []string) error {
	root, err := project.CentralStoreRoot()
	if err != nil {
		return fmt.Errorf("frontier requires a configured central store: %w", err)
	}
	store := ticket.NewMultiStore(filepath.Join(root, "tickets"))
	if projectFlag != "" {
		if _, err := ticket.ResolveStoreForNamespace(projectFlag); err != nil {
			return err
		}
	}

	// One reading: the frontier and the membership under --parent come off
	// the same snapshot.
	snap, err := store.Snapshot()
	if err != nil {
		return err
	}
	tickets := ticket.FrontierOf(store, store.ListFromSnapshot(snap))

	if v, _ := cmd.Flags().GetString("parent"); v != "" {
		if p, _ := ticket.ParseNamespacedID(v); p == "" && projectFlag == "" {
			return fmt.Errorf("--parent %q must be qualified (project/id), or pass --project to say which namespace it names", v)
		}
		parentID, err := snap.Resolve(projectFlag, v)
		if err != nil {
			return err
		}
		if p, ok := snap.Get(parentID); ok && p.Type != ticket.TypeEpic {
			return fmt.Errorf("--parent %s is type %s, not an epic", parentID, p.Type)
		}
		member := map[string]bool{}
		for _, c := range snap.Children(parentID) {
			member[c.ID] = true
		}
		var kept []*ticket.Ticket
		for _, t := range tickets {
			if member[t.ID] {
				kept = append(kept, t)
			}
		}
		tickets = kept
	}

	if projectFlag != "" {
		var filtered []*ticket.Ticket
		for _, t := range tickets {
			if p, _ := ticket.ParseNamespacedID(t.ID); p == projectFlag {
				filtered = append(filtered, t)
			}
		}
		tickets = filtered
	}

	if len(tickets) == 0 {
		printEmptyMessage()
		return nil
	}

	ticket.SortByPriorityID(tickets)

	if jsonOutput {
		items := make([]ticketJSON, 0, len(tickets))
		for _, t := range tickets {
			items = append(items, toTicketJSON(t))
		}
		data, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	// STATUS is ready for every row by construction — drop the column.
	newTableWriter("STATUS").Print(tickets)
	return nil
}
